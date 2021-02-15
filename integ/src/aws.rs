use async_trait::async_trait;
use rusoto_cloudformation::{
    CloudFormation, CloudFormationClient, CreateStackInput, DescribeStacksInput, Parameter,
};
use rusoto_core::{DispatchSignedRequest, Region};
use rusoto_credential::{DefaultCredentialsProvider, ProvideAwsCredentials};
use snafu::{OptionExt, ResultExt};
use std::collections::HashMap;
use std::str::FromStr;

// 10 minutes timeout for stack creation
const CREATE_STACK_TIMEOUT: i64 = 10;

pub(crate) trait NewWith {
    fn new_with<P, D>(request_dispatcher: D, credentials_provider: P, region: Region) -> Self
    where
        P: ProvideAwsCredentials + Send + Sync + 'static,
        D: DispatchSignedRequest + Send + Sync + 'static;
}

impl NewWith for CloudFormationClient {
    fn new_with<P, D>(request_dispatcher: D, credentials_provider: P, region: Region) -> Self
    where
        P: ProvideAwsCredentials + Send + Sync + 'static,
        D: DispatchSignedRequest + Send + Sync + 'static,
    {
        Self::new_with(request_dispatcher, credentials_provider, region)
    }
}

/// Create a rusoto client of the given type using the given region
fn build_client<T: NewWith>(region: &Region) -> Result<T> {
    let provider = DefaultCredentialsProvider::new().context(error::DefaultProvider)?;
    Ok(T::new_with(
        rusoto_core::HttpClient::new().context(error::HttpClient)?,
        provider,
        region.clone(),
    ))
}

pub(crate) struct AwsCfnMediator {
    cfn_client: CloudFormationClient,
}

impl AwsCfnMediator {
    pub(crate) fn new(region_name: &str) -> Result<Self> {
        let region =
            Region::from_str(region_name).context(error::ParseRegion { name: region_name })?;
        let cfn_client = build_client::<CloudFormationClient>(&region)?;
        Ok(AwsCfnMediator { cfn_client })
    }
}

/// CfnMediator trait is used to provide abstraction over cloudformation api and maps
/// response to internal types for easy consumption
#[async_trait]
pub(crate) trait CfnMediator {
    /// Creates a cloudformation stack from template file
    async fn create_stack(
        &self,
        template_body: String,
        stack_name: String,
        parameters: Option<Vec<Parameter>>,
    ) -> Result<()>;

    /// Describes cloudformation stacks
    async fn describe_stacks(&self, stack_name: String) -> Result<Vec<StackInfo>>;
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) struct StackResource {
    pub id: String,
    pub locgical_name: String,
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) struct StackInfo {
    pub stack_name: String,
    pub stack_status: String,
    pub outputs: HashMap<String, String>,
}

#[async_trait]
impl CfnMediator for AwsCfnMediator {
    async fn create_stack(
        &self,
        template_body: String,
        stack_name: String,
        parameters: Option<Vec<Parameter>>,
    ) -> Result<()> {
        self.cfn_client
            .create_stack(CreateStackInput {
                capabilities: Some(vec![String::from("CAPABILITY_NAMED_IAM")]),
                stack_name,
                template_body: Some(template_body),
                parameters,
                // Delete stack instead of rollback on failure
                on_failure: Some("DELETE".to_string()),
                timeout_in_minutes: Some(CREATE_STACK_TIMEOUT),
                ..CreateStackInput::default()
            })
            .await
            .context(error::CreateStack)?;
        Ok(())
    }

    async fn describe_stacks(&self, stack_name: String) -> Result<Vec<StackInfo>> {
        let resp = self
            .cfn_client
            .describe_stacks(DescribeStacksInput {
                stack_name: Some(stack_name),
                ..DescribeStacksInput::default()
            })
            .await
            .context(error::DescribeStacks)?;
        let mut stacks = Vec::new();
        for stack in resp.stacks.context(error::CfnMissingField {
            field: "stacks",
            api: "describe_stacks",
        })? {
            let mut outputs = HashMap::new();
            if let Some(stack_outputs) = stack.outputs {
                for out in stack_outputs {
                    outputs.insert(
                        out.output_key.context(error::CfnMissingField {
                            field: "stacks.outputs.output_key",
                            api: "describe_stacks",
                        })?,
                        out.output_value.context(error::CfnMissingField {
                            field: "stacks.outputs.output_value",
                            api: "describe_stacks",
                        })?,
                    );
                }
            }
            stacks.push(StackInfo {
                stack_name: stack.stack_name,
                stack_status: stack.stack_status,
                outputs,
            });
        }
        Ok(stacks)
    }
}

type Result<T> = std::result::Result<T, error::Error>;
pub(crate) mod error {
    use snafu::Snafu;

    /// The error type for this module.
    #[derive(Debug, Snafu)]
    #[snafu(visibility = "pub(super)")]
    pub(crate) enum Error {
        /// The application failed because there was a missing field in Cloudformation api
        /// call response
        #[snafu(display("Missing field in `{}` response: {}", api, field))]
        CfnMissingField {
            api: &'static str,
            field: &'static str,
        },

        /// The application failed to create cloudformation stack
        #[snafu(display("Failed to create stack: {}", source))]
        CreateStack {
            source: rusoto_core::RusotoError<rusoto_cloudformation::CreateStackError>,
        },

        /// The application failed to create default aws credential provider.
        #[snafu(display("Failed to create the default AWS credentials provider: {}", source))]
        DefaultProvider {
            source: rusoto_credential::CredentialsError,
        },

        /// The application failed to describe cloudformation stacks
        #[snafu(display("Failed to describe stacks: {}", source))]
        DescribeStacks {
            source: rusoto_core::RusotoError<rusoto_cloudformation::DescribeStacksError>,
        },

        /// The application failed to create http client required by `rusoto`
        #[snafu(display("Failed to create HTTP client: {}", source))]
        HttpClient {
            source: rusoto_core::request::TlsError,
        },

        /// The application failed to convert to AWS region enum from string
        #[snafu(display("Failed to parse region `{}` : {}", name, source))]
        ParseRegion {
            name: String,
            source: rusoto_signature::region::ParseRegionError,
        },
    }
}

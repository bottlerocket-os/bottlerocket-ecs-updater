use crate::{
    EcsMediator, Instance, Instances, SsmCommandDetails, SsmInvocationResult, SsmMediator,
};
use async_trait::async_trait;
use rusoto_core::{DispatchSignedRequest, Region};
use rusoto_credential::{DefaultCredentialsProvider, ProvideAwsCredentials};
use rusoto_ecs::{
    Attribute, DescribeContainerInstancesRequest, Ecs, EcsClient, ListContainerInstancesRequest,
};
use rusoto_ssm::{ListCommandInvocationsRequest, SendCommandRequest, Ssm, SsmClient};
use snafu::{ensure, OptionExt, ResultExt, Snafu};
use std::collections::HashMap;
use std::str::FromStr;
use tokio::time::{sleep, Duration};

// TODO: might need tuning for better default value
const SSM_COMMAND_DEFAULT_TIMEOUT_SECS: i64 = 60;

type Result<T> = std::result::Result<T, Error>;

/// The error type for this module.
#[derive(Debug, Snafu)]
enum Error {
    #[snafu(display("Failed to create the default AWS credentials provider: {}", source))]
    DefaultProvider {
        source: rusoto_credential::CredentialsError,
    },

    #[snafu(display("Failed to describe container instances: {}", source))]
    DescribeContainerInstances {
        source: rusoto_core::RusotoError<rusoto_ecs::DescribeContainerInstancesError>,
    },

    #[snafu(display("Missing field in `{}` response: {}", api, field))]
    EcsMissingField {
        api: &'static str,
        field: &'static str,
    },

    #[snafu(display("Failed to create HTTP client: {}", source))]
    HttpClient {
        source: rusoto_core::request::TlsError,
    },

    #[snafu(display("Failed to list command invocations: {}", source))]
    ListCommandInvocations {
        source: rusoto_core::RusotoError<rusoto_ssm::ListCommandInvocationsError>,
    },

    #[snafu(display("Failed to list container instances: {}", source))]
    ListContainerInstances {
        source: rusoto_core::RusotoError<rusoto_ecs::ListContainerInstancesError>,
    },

    #[snafu(display(
        "Missing command_plugin in `list_command_invocations` responses for instance '{}'",
        instance_id
    ))]
    MissingPlugin { instance_id: String },

    #[snafu(display("Failed to parse region `{}` : {}", name, source))]
    ParseRegion {
        name: String,
        source: rusoto_signature::region::ParseRegionError,
    },

    #[snafu(display("Missing field in `{}` response: {}", api, field))]
    SsmMissingField {
        api: &'static str,
        field: &'static str,
    },

    #[snafu(display("Failed to send ssm command: {}", source))]
    SsmSendCommand {
        source: rusoto_core::RusotoError<rusoto_ssm::SendCommandError>,
    },
}

impl From<Error> for crate::Error {
    fn from(e: Error) -> Self {
        crate::Error::new(e)
    }
}

pub(crate) trait NewWith {
    fn new_with<P, D>(request_dispatcher: D, credentials_provider: P, region: Region) -> Self
    where
        P: ProvideAwsCredentials + Send + Sync + 'static,
        D: DispatchSignedRequest + Send + Sync + 'static;
}

impl NewWith for EcsClient {
    fn new_with<P, D>(request_dispatcher: D, credentials_provider: P, region: Region) -> Self
    where
        P: ProvideAwsCredentials + Send + Sync + 'static,
        D: DispatchSignedRequest + Send + Sync + 'static,
    {
        Self::new_with(request_dispatcher, credentials_provider, region)
    }
}

impl NewWith for SsmClient {
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
    let provider = DefaultCredentialsProvider::new().context(self::DefaultProvider)?;
    Ok(T::new_with(
        rusoto_core::HttpClient::new().context(self::HttpClient)?,
        provider,
        region.clone(),
    ))
}

pub struct AwsEcsMediator {
    ecs_client: EcsClient,
}

impl AwsEcsMediator {
    pub fn new(region_name: &str) -> crate::Result<Self> {
        let region =
            Region::from_str(region_name).context(self::ParseRegion { name: region_name })?;
        let ecs_client = build_client::<EcsClient>(&region)?;
        Ok(AwsEcsMediator { ecs_client })
    }
}

#[async_trait]
impl EcsMediator for AwsEcsMediator {
    async fn list_bottlerocket_instances(
        &self,
        cluster: &str,
        max_results: Option<i64>,
        next_token: Option<String>,
    ) -> crate::Result<Instances> {
        // get all container instances
        let list_instances = self
            .ecs_client
            .list_container_instances(ListContainerInstancesRequest {
                cluster: Some(cluster.to_string()),
                max_results,
                next_token,
                ..ListContainerInstancesRequest::default()
            })
            .await
            .context(ListContainerInstances)?;
        let container_instance_arns =
            list_instances
                .container_instance_arns
                .context(self::EcsMissingField {
                    field: "container_instance_arns",
                    api: "list_container_instances",
                })?;
        let resp = self
            .ecs_client
            .describe_container_instances(DescribeContainerInstancesRequest {
                cluster: Some(cluster.to_string()),
                container_instances: container_instance_arns,
                include: None,
            })
            .await
            .context(DescribeContainerInstances)?;
        let mut instances = Vec::new();
        for inst in resp.container_instances.context(EcsMissingField {
            field: "container_instances",
            api: "describe_container_instances",
        })? {
            // Only add instances running Bottlerocket
            if is_bottlerocket(&inst.attributes) {
                instances.push(Instance {
                    instance_id: inst.ec_2_instance_id.context(EcsMissingField {
                        api: "describe_container_instances",
                        field: "container_instances[].ec_2_instance_id",
                    })?,
                    status: inst.status.context(EcsMissingField {
                        api: "describe_container_instances",
                        field: "container_instances[].status",
                    })?,
                });
            }
        }
        Ok(Instances {
            bottlerocket_instances: instances,
            next_token: list_instances.next_token,
        })
    }
}

// iterates instance attributes and checks "bottlerocket.variant" attribute
// to identify Bottlerocket instance
fn is_bottlerocket(attributes: &Option<Vec<Attribute>>) -> bool {
    match attributes {
        None => false,
        Some(attributes) => attributes.iter().any(|a| {
            a.name == "bottlerocket.variant" && a.value.clone().unwrap_or_default() == "aws-ecs-1"
        }),
    }
}

pub struct AwsSsmMediator {
    ssm_client: SsmClient,
}

impl AwsSsmMediator {
    pub fn new(region_name: &str) -> crate::Result<Self> {
        let region =
            Region::from_str(region_name).context(self::ParseRegion { name: region_name })?;
        let ssm_client = build_client::<SsmClient>(&region)?;
        Ok(AwsSsmMediator { ssm_client })
    }
}

#[async_trait]
impl SsmMediator for AwsSsmMediator {
    async fn send_command(
        &self,
        instance_ids: Vec<String>,
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> crate::Result<SsmCommandDetails> {
        let command = self
            .ssm_client
            .send_command(SendCommandRequest {
                comment: Some("Makes Bottlerocket API call via SSM".into()),
                instance_ids: Some(instance_ids),
                document_name: String::from("AWS-RunShellScript"),
                document_version: Some("1".into()),
                parameters: Some(params.clone()),
                timeout_seconds: match timeout {
                    None => Some(SSM_COMMAND_DEFAULT_TIMEOUT_SECS),
                    Some(_) => timeout,
                },
                ..SendCommandRequest::default()
            })
            .await
            .context(SsmSendCommand)?
            .command
            .context(SsmMissingField {
                field: "command",
                api: "send_command",
            })?;
        Ok(SsmCommandDetails {
            command_id: command.command_id.context(SsmMissingField {
                field: "command.command_id",
                api: "send_command",
            })?,
            status: command.status.context(SsmMissingField {
                field: "command.status",
                api: "send_command",
            })?,
        })
    }

    async fn list_command_invocations(
        &self,
        command_id: &str,
        details: bool,
    ) -> crate::Result<Vec<SsmInvocationResult>> {
        let resp = self
            .ssm_client
            .list_command_invocations(ListCommandInvocationsRequest {
                command_id: Some(command_id.to_string()),
                details: Some(details),
                ..ListCommandInvocationsRequest::default()
            })
            .await
            .context(ListCommandInvocations)?;
        let mut invocation_list = Vec::new();
        for invocation in resp.command_invocations.context(SsmMissingField {
            field: "command_invocations",
            api: "list_command_invocations",
        })? {
            let instance_id = invocation.instance_id.context(SsmMissingField {
                field: "instance_id",
                api: "list_command_invocations",
            })?;
            let mut result = SsmInvocationResult {
                instance_id: instance_id.clone(),
                invocation_status: invocation.status.context(SsmMissingField {
                    field: "command_invocations[].status",
                    api: "list_command_invocations",
                })?,
                script_output: None,
                script_response_code: None,
            };
            // command_plugins is available only when we fetch invocations with details
            if details {
                let plugins = invocation.command_plugins.context(SsmMissingField {
                    field: "command_invocations[].command_plugins",
                    api: "list_command_invocations",
                })?;
                //  Expect only single plugin to exist per instance for our command shell script
                ensure!(plugins.len() == 1, MissingPlugin { instance_id });
                result.script_response_code = Some(plugins[0].response_code.to_owned().context(
                    SsmMissingField {
                        field: "command_invocations[].command_plugins[0].response_code",
                        api: "list_command_invocations",
                    },
                )?);
                result.script_output =
                    Some(plugins[0].output.to_owned().context(SsmMissingField {
                        field: "command_invocations[].command_plugins[0].output",
                        api: "list_command_invocations",
                    })?);
            }
            invocation_list.push(result);
        }
        Ok(invocation_list)
    }

    async fn wait_command_complete(&self, command_id: &str) -> crate::Result<()> {
        loop {
            println!("waiting for command to complete");
            // we need to wait before calling invocation because it takes some time
            // for command to be registered before we can list invocations.
            sleep(Duration::from_millis(1000)).await;
            let results = self.list_command_invocations(command_id, false).await?;
            let is_any_pending = results
                .iter()
                .any(|result| result.invocation_status == "InProgress");
            if !is_any_pending {
                // exit, all command have completed
                break;
            }
        }
        Ok(())
    }
}

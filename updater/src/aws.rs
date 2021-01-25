use crate::EcsMediator;
use async_trait::async_trait;
use rusoto_core::{DispatchSignedRequest, Region};
use rusoto_credential::{DefaultCredentialsProvider, ProvideAwsCredentials};
use rusoto_ecs::{Ecs, EcsClient, ListContainerInstancesRequest};
use snafu::{OptionExt, ResultExt, Snafu};
use std::str::FromStr;

type Result<T> = std::result::Result<T, Error>;

/// The error type for this module.
#[derive(Debug, Snafu)]
enum Error {
    #[snafu(display("Failed to create the default AWS credentials provider: {}", source))]
    DefaultProvider {
        source: rusoto_credential::CredentialsError,
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

    #[snafu(display("Failed to list container instances: {}", source))]
    ListContainerInstances {
        source: rusoto_core::RusotoError<rusoto_ecs::ListContainerInstancesError>,
    },

    #[snafu(display("Failed to parse region `{}` : {}", name, source))]
    ParseRegion {
        name: String,
        source: rusoto_signature::region::ParseRegionError,
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
    async fn list_container_instances(&self, cluster: &str) -> crate::Result<Vec<String>> {
        let resp = self
            .ecs_client
            .list_container_instances(ListContainerInstancesRequest {
                cluster: Some(cluster.to_string()),
                ..ListContainerInstancesRequest::default()
            })
            .await
            .context(ListContainerInstances)?;
        let container_instance_arns =
            resp.container_instance_arns
                .context(self::EcsMissingField {
                    field: "container_instance_arns",
                    api: "list_container_instances",
                })?;
        Ok(container_instance_arns)
    }
}

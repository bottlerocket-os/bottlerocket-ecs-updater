use crate::aws::error::{self, Result};
use rusoto_core::{DispatchSignedRequest, Region};
use rusoto_credential::{DefaultCredentialsProvider, ProvideAwsCredentials};
use rusoto_ecs::EcsClient;
use rusoto_ssm::SsmClient;
use snafu::ResultExt;

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

/// Create a rusoto client of the given type using the given region and configuration.
pub(crate) fn build_client<T: NewWith>(region: &Region) -> Result<T> {
    let provider = DefaultCredentialsProvider::new().context(error::Provider)?;
    Ok(T::new_with(
        rusoto_core::HttpClient::new().context(error::HttpClient)?,
        provider,
        region.clone(),
    ))
}

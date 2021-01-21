//! Contains the error type for this library.

#![allow(clippy::default_trait_access)]

use snafu::{Backtrace, Snafu};
/// Alias for `Result<T, Error>`.
pub type Result<T> = std::result::Result<T, Error>;

/// The error type for this library.
#[derive(Debug, Snafu)]
#[snafu(visibility = "pub(crate)")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum Error {
    // The application failed to describe container instance in cluster
    #[snafu(display("Failed to describe instance for cluster {}: {}", cluster_arn, source))]
    DescribeInstances {
        cluster_arn: String,
        source: rusoto_core::RusotoError<rusoto_ecs::DescribeContainerInstancesError>,
        backtrace: Backtrace,
    },

    // The application failed because of missing field in response
    #[snafu(display("Missing field in `{}` response: {}", api, field))]
    ECSMissingField {
        api: &'static str,
        field: &'static str,
    },

    // The application failed to create HttpClient
    #[snafu(display("Failed to create HTTP client: {}", source))]
    HttpClient {
        source: rusoto_core::request::TlsError,
        backtrace: Backtrace,
    },

    // The application failed to get ssm command invocation result
    #[snafu(display(
        "Failed to get ssm command `{}` invocation result: {}",
        command_id,
        source
    ))]
    ListCommandInvocations {
        command_id: String,
        source: rusoto_core::RusotoError<rusoto_ssm::ListCommandInvocationsError>,
        backtrace: Backtrace,
    },

    // The application failed to list container instances in cluster
    #[snafu(display(
        "Failed to list container instances for cluster {}: {}",
        cluster_arn,
        source
    ))]
    ListContainerInstances {
        cluster_arn: String,
        source: rusoto_core::RusotoError<rusoto_ecs::ListContainerInstancesError>,
        backtrace: Backtrace,
    },

    // The application failed to parse region
    #[snafu(display("Failed to parse region {} : {}", name, source))]
    ParseRegion {
        name: String,
        source: rusoto_signature::region::ParseRegionError,
        backtrace: Backtrace,
    },

    // The application failed to create AWS credential provider
    #[snafu(display("Failed to create AWS credentials provider: {}", source))]
    Provider {
        source: rusoto_credential::CredentialsError,
    },

    // The application failed to send ssm command to instances
    #[snafu(display("Failed to send SSM command: {}", source))]
    SendSSMCommand {
        source: rusoto_core::RusotoError<rusoto_ssm::SendCommandError>,
        backtrace: Backtrace,
    },

    // The application failed because of missing field in response
    #[snafu(display(
        "Missing field in `{}` response for instance {}: {}",
        api,
        instance_id,
        field
    ))]
    SSMInvocationMissingField {
        instance_id: String,
        api: &'static str,
        field: &'static str,
    },

    // The application failed because of missing field in response
    #[snafu(display("Missing field in `{}` response: {}", api, field))]
    SSMMissingField {
        api: &'static str,
        field: &'static str,
    },
}

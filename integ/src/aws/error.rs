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
    // The application failed because of missing field in response
    #[snafu(display("Missing field `{}` in api `{}` response", field, api))]
    CfnMissingField {
        api: &'static str,
        field: &'static str,
    },

    // The application failed to create cluster
    #[snafu(display("Failed to create cluster: {}", source))]
    CreateCluster {
        source: rusoto_core::RusotoError<rusoto_ecs::CreateClusterError>,
        backtrace: Backtrace,
    },

    // The application failed to describe container instance in cluster
    #[snafu(display("Failed to create stack: {}", source))]
    CreateStack {
        source: rusoto_core::RusotoError<rusoto_cloudformation::CreateStackError>,
        backtrace: Backtrace,
    },

    // The application failed to delete stack
    #[snafu(display("Failed to delete stack: {}", source))]
    DeleteStack {
        source: rusoto_core::RusotoError<rusoto_cloudformation::DeleteStackError>,
        backtrace: Backtrace,
    },

    // The application failed to describe stacks
    #[snafu(display("Failed to describe stacks: {}", source))]
    DescribeStacks {
        source: rusoto_core::RusotoError<rusoto_cloudformation::DescribeStacksError>,
        backtrace: Backtrace,
    },

    // The application failed to delete stack
    #[snafu(display("Failed to describe stack resources: {}", source))]
    DescribeStackResources {
        source: rusoto_core::RusotoError<rusoto_cloudformation::DescribeStackResourcesError>,
        backtrace: Backtrace,
    },

    // The application failed because of missing field in response
    #[snafu(display("Missing field `{}` in api `{}` response", field, api))]
    ECSMissingField {
        api: &'static str,
        field: &'static str,
    },

    // The application failed because of missing field in response
    #[snafu(display("Missing field `{}` in api `{}` response", field, api))]
    Ec2MissingField {
        api: &'static str,
        field: &'static str,
    },

    // The application failed to create HttpClient
    #[snafu(display("Failed to create HTTP client: {}", source))]
    HttpClient {
        source: rusoto_core::request::TlsError,
        backtrace: Backtrace,
    },

    // The application failed because of missing `physical_resource_id` field in stack resource
    #[snafu(display(
        "Missing field `physical_resource_id` for stack resource {}",
        resource_name
    ))]
    MissingPhysicalResourceID { resource_name: String },

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
        backtrace: Backtrace,
    },

    // The application failed to start instances
    #[snafu(display("Failed to run instances: {}", source))]
    RunInstance {
        source: rusoto_core::RusotoError<rusoto_ec2::RunInstancesError>,
        backtrace: Backtrace,
    },
}

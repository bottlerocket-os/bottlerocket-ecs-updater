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
    // The application failed to send ssm command to check updates
    #[snafu(display("Failed to send ssm command to check updates: {}", source))]
    CheckUpdates {
        source: Box<dyn std::error::Error + Send + Sync + 'static>,
        backtrace: Backtrace,
    },

    // The application failed to describe container instances
    #[snafu(display("Failed to describe container instances: {}", source))]
    DescribeContainerInstances {
        source: Box<dyn std::error::Error + Send + Sync + 'static>,
        backtrace: Backtrace,
    },

    // The application failed to create ECS client
    #[snafu(display("Failed to create ECS client: {}", source))]
    EcsClient { source: crate::aws::error::Error },

    // The application failed because empty region was passed
    #[snafu(display("--region should not be empty"))]
    EmptyRegion,

    // The application failed to get ssm command output
    #[snafu(display("Failed to get ssm command output: {}", source))]
    GetCommandOutput {
        source: Box<dyn std::error::Error + Send + Sync + 'static>,
        backtrace: Backtrace,
    },

    // The application failed because region is invalid
    #[snafu(display("Invalid region: {}", source))]
    InvalidRegion { source: crate::aws::error::Error },

    // The application failed to list container instances from cluster
    #[snafu(display("Failed to list container instances: {}", source))]
    ListContainerInstances {
        source: Box<dyn std::error::Error + Send + Sync + 'static>,
        backtrace: Backtrace,
    },

    // The application failed to create SSM client
    #[snafu(display("Failed to create SSM client: {}", source))]
    SsmClient { source: crate::aws::error::Error },
}

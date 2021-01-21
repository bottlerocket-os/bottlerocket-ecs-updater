//! Contains the error type for this library.

#![allow(clippy::default_trait_access)]

use crate::aws;
use snafu::{Backtrace, Snafu};

/// Alias for `Result<T, Error>`.
pub type Result<T> = std::result::Result<T, Error>;

/// The error type for this library.
#[derive(Debug, Snafu)]
#[snafu(visibility = "pub(crate)")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum Error {
    // The application failed to create cluster
    #[snafu(display("Failed to add instances to cluster `{}`: {}", cluster_name, source))]
    AddClusterInstances {
        cluster_name: String,
        source: aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed to create cluster
    #[snafu(display("Failed to create cluster `{}`: {}", cluster_name, source))]
    CreateTestCluster {
        cluster_name: String,
        source: aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed to create Cloudformation client
    #[snafu(display("Failed to create Cloudformation client: {}", source))]
    CfnClient {
        source: crate::aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed to create ECS client
    #[snafu(display("Failed to create ECS client: {}", source))]
    EcsClient {
        source: crate::aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed to create EC2 client
    #[snafu(display("Failed to create Ec2 client: {}", source))]
    Ec2Client {
        source: crate::aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed because empty region was passed
    #[snafu(display("--region should not be empty"))]
    EmptyRegion,

    // The application failed because region is invalid
    #[snafu(display("Invalid region: {}", source))]
    InvalidRegion {
        source: aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed to setup Logger
    #[snafu(display("Logger setup error: {}", source))]
    Logger { source: log::SetLoggerError },

    // The application failed to read stack
    #[snafu(display("Failed to read stack file `{}`: {}", stack_file_name, source))]
    ReadStack {
        stack_file_name: String,
        source: std::io::Error,
        backtrace: Backtrace,
    },

    // The application failed to get stacks base path
    #[snafu(display("Failed to create stacks base path"))]
    StacksPath,

    // The application failed to start integ stak
    #[snafu(display("Failed to start integ stack `` {}:", source))]
    StartIntegStack {
        stack_name: String,
        source: aws::error::Error,
        backtrace: Backtrace,
    },

    // The application failed to start integ stak
    #[snafu(display("Failed to start updater stack `{}`: {}", stack_name, source))]
    UpdaterStack {
        stack_name: String,
        source: aws::error::Error,
        backtrace: Backtrace,
    },
}

/*!
We created a `lib.rs` file to facilitate testing in the `tests` folder, and any useful code reuse in
the `integ` project. This project is not meant to be used as a library in other projects.
!*/

#![deny(rust_2018_idioms)]

mod aws;
mod updater;

use crate::aws::{AwsEcsMediator, AwsSsmMediator};
pub use crate::updater::Updater;
use async_trait::async_trait;
use std::collections::HashMap;
use std::fmt::{Display, Formatter};
use structopt::StructOpt;

/// An opaque error type to wrap more detailed error types. The inner type provides the message.
#[derive(Debug)]
pub struct Error(Box<dyn std::error::Error + Send + Sync + 'static>);
pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    /// Create a new opaque error.
    pub fn new<E>(source: E) -> Self
    where
        E: Into<Box<dyn std::error::Error + Send + Sync>>,
    {
        Self(source.into())
    }
}

impl Display for Error {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        std::fmt::Display::fmt(&self.0, f)
    }
}

// implement std::error::Error to support Error type as source for snafu
impl std::error::Error for Error {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(self)
    }
}

// the Args struct is defined in `lib.rs` so that we can use it in integ tests
/// Bottlerocket ECS Updater
///
/// Watches Bottlerocket instances in your ECS cluster, and updates them when they have updates
/// available.
///
/// Arguments can be specified by environment variable. Command-line arguments will override a value
/// that is given by environment variable.
///
#[derive(StructOpt, Debug)]
pub struct Args {
    /// The short name or full Amazon Resource Name (ARN) of the cluster in which we will manage
    /// Bottlerocket instances.
    #[structopt(long, env = "BOTTLEROCKET_ECS_CLUSTER")]
    pub cluster: String,
    /// The AWS Region in which cluster is running
    #[structopt(long, env = "AWS_REGION")]
    pub region: String,
}

/// Creates a new concrete implementation of [`EcsMediator`] using `rusoto`.
pub fn new_ecs(region: &str) -> Result<impl EcsMediator> {
    Ok(AwsEcsMediator::new(region)?)
}

/// Creates a new concrete implementation of [`SsmMediator`] using `rusoto`.
pub fn new_ssm(region: &str) -> Result<impl SsmMediator> {
    Ok(AwsSsmMediator::new(region)?)
}

// instances in a batch running Bottlerocket OS will be mapped to this
#[derive(Debug, Clone, PartialEq)]
pub struct Instances {
    // list of Bottlerocket instances
    pub bottlerocket_instances: Vec<Instance>,
    // next_token to fetch next batch of instances
    pub next_token: Option<String>,
}

// details of instance running Bottlerocket OS will be mapped to this
#[derive(Debug, Clone, PartialEq)]
pub struct Instance {
    // ec2 instance id
    pub instance_id: String,
    // tells the status of the container instance.
    // The valid values are REGISTERING , REGISTRATION_FAILED , ACTIVE , INACTIVE , DEREGISTERING , or DRAINING .
    pub status: String,
}

/// Introducing a trait abstraction over the the ECS API allows us to mock the API and write tests
/// without going to the extremely low level of `rusoto_mock`. That is, we can mock the higher level
/// use-cases of what we might send and receive to/from the API instead of mocking the API itself.
#[async_trait]
pub trait EcsMediator {
    /// Describes each container instances and gets only Bottlerocket instances
    async fn list_bottlerocket_instances(
        &self,
        cluster: &str,
        max_results: Option<i64>,
        next_token: Option<String>,
    ) -> Result<Instances>;
}

// Command details from ssm `SendCommandResponse` will be mapped to this
#[derive(Debug, Clone, PartialEq)]
pub struct SsmCommandDetails {
    // id for ssm command
    pub command_id: String,
    // status of the ssm command
    pub status: String,
}

// invocation results from ssm `ListCommandInvocationsResult` will be mapped to this
#[derive(Debug, Clone, PartialEq)]
pub struct SsmInvocationStatus {
    // ec2 instance id on which command ran
    pub instance_id: String,
    // command invocation status, valid values are Success, Failed, or Pending
    pub invocation_status: String,
}

// invocation results from ssm `GetCommandInvocationResult` will be mapped to this
#[derive(Debug, Clone, PartialEq)]
pub struct SsmInvocationOutput {
    // ec2 instance id on which command ran
    pub instance_id: String,
    // ssm command script output on stdout
    pub standard_output: String,
    // command invocation status, valid values are Success, Failed, or Pending
    pub status: String,
    // command invocation script response code
    pub response_code: i64,
}

/// Introducing a trait abstraction over the the SSM API allows us to mock the API and write tests
/// without going to the extremely low level of `rusoto_mock`. That is, we can mock the higher level
/// use-cases of what we might send and receive to/from the API instead of mocking the API itself.
#[async_trait]
pub trait SsmMediator {
    /// Runs ssm document on the list of instances provided.
    async fn send_command(
        &self,
        instance_ids: Vec<String>,
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> Result<SsmCommandDetails>;

    /// Gets the all ssm command status
    async fn list_command_invocations(&self, command_id: &str) -> Result<Vec<SsmInvocationStatus>>;

    /// Gets the ssm command result
    async fn get_command_invocations(
        &self,
        command_id: &str,
        instance_id: &str,
    ) -> Result<SsmInvocationOutput>;

    async fn wait_command_complete(&self, command_id: &str) -> Result<()>;
}

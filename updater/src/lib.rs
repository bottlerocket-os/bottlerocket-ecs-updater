/*!
We created a `lib.rs` file to facilitate testing in the `tests` folder, and any useful code reuse in
the `integ` project. This project is not meant to be used as a library in other projects.
!*/

#![deny(rust_2018_idioms)]

mod aws;

use crate::aws::{AwsEcsMediator, AwsSsmMediator};
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

// TODO - this abstraction may inhibit testing, it may need to change as functionality expands.
/// The long-lived object that will watch an ECS cluster and update Bottlerocket hosts.
pub struct Updater<T: EcsMediator, S: SsmMediator> {
    cluster: String,
    ecs: T,
    // TODO: remove when we use api calls to check updates.
    #[allow(dead_code)]
    ssm: S,
}

impl<T: EcsMediator, S: SsmMediator> Updater<T, S> {
    /// Create a new `Updater`.
    pub fn new(args: Args, ecs_mediator: T, ssm_mediator: S) -> Self {
        Self {
            cluster: args.cluster,
            ecs: ecs_mediator,
            ssm: ssm_mediator,
        }
    }

    /// Run the `Updater`
    // TODO - once we start looping we may need a cancellation mechanism, watch for SIGINT etc.
    pub async fn run(&self) -> Result<()> {
        // TODO: use max_results and next_token to query instances in batch
        let list = self
            .ecs
            .list_bottlerocket_instances(&self.cluster, None, None)
            .await?;
        println!("{:?}", list);
        Ok(())
    }
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
pub struct SsmInvocationResult {
    // ec2 instance id on which command ran
    pub instance_id: String,
    // ssm command script output
    pub script_output: Option<String>,
    // command invocation status, valid values are Success, Failed, or Pending
    pub invocation_status: String,
    // command invocation script response code
    pub script_response_code: Option<i64>,
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

    /// Gets the ssm command result
    async fn list_command_invocations(
        &self,
        command_id: &str,
        details: bool,
    ) -> Result<Vec<SsmInvocationResult>>;
}

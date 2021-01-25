/*!
We created a `lib.rs` file to facilitate testing in the `tests` folder, and any useful code reuse in
the `integ` project. This project is not meant to be used as a library in other projects.
!*/

#![deny(rust_2018_idioms)]

mod aws;

use crate::aws::AwsEcsMediator;
use async_trait::async_trait;
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

// TODO - this abstraction may inhibit testing, it may need to change as functionality expands.
/// The long-lived object that will watch an ECS cluster and update Bottlerocket hosts.
pub struct Updater<T: EcsMediator> {
    cluster: String,
    ecs: T,
}

impl<T: EcsMediator> Updater<T> {
    /// Create a new `Updater`.
    pub fn new(args: Args, ecs_mediator: T) -> Self {
        Self {
            cluster: args.cluster,
            ecs: ecs_mediator,
        }
    }

    /// Run the `Updater`
    // TODO - once we start looping we may need a cancellation mechanism, watch for SIGINT etc.
    pub async fn run(&self) -> Result<()> {
        let list = self.ecs.list_container_instances(&self.cluster).await?;
        println!("{:?}", list);
        Ok(())
    }
}

/// Introducing a trait abstraction over the the ECS API allows us to mock the API and write tests
/// without going to the extremely low level of `rusoto_mock`. That is, we can mock the higher level
/// use-cases of what we might send and receive to/from the API instead of mocking the API itself.
#[async_trait]
pub trait EcsMediator {
    /// Provides a list of container instance ARNs in the given cluster.
    async fn list_container_instances(&self, cluster: &str) -> Result<Vec<String>>;
}

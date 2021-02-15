mod aws;

use crate::aws::{AwsCfnMediator, CfnMediator};
use snafu::{OptionExt, ResultExt};
use std::path::PathBuf;
use std::{fs, process, thread, time};
use structopt::StructOpt;

/// Bottlerocket ECS Updater Integ
///
/// A test system that deploys Bottlerocket instances into an ECS cluster, deploys the local version
/// of `bottlerocket-ecs-updater` and asserts that it works.
///
#[derive(StructOpt, Debug)]
pub struct Args {
    /// The AWS Region in which cluster is running
    #[structopt(long, env = "AWS_REGION")]
    pub region: String,
}

#[tokio::main]
async fn main() {
    let args = Args::from_args();
    // we want to print the error message using the display trait
    if let Err(e) = main_inner(args).await {
        eprintln!("{}", e);
        process::exit(1);
    }
}

async fn main_inner(args: Args) -> Result<()> {
    // TODO: we would have to restructure complete setup so that
    // integ stack is deployed only once per account and we are able to run
    // multiple integ test in parallel. As of now setup looks more like an application
    // and below calls are added to demonstrate api calls.
    let integ_template_name = "integ-shared.yaml";
    let integ_stack_name = "bottlerocket-ecs-updater-integ";
    let template = get_stack_template(integ_template_name)?;
    let cfn_mediator = new_cfn(&args.region)?;
    cfn_mediator
        .create_stack(template, integ_stack_name.to_string(), None)
        .await
        .context(error::CreateIntegStack {
            integ_template_name,
        })?;
    thread::sleep(time::Duration::from_secs(200));
    let stacks_details = cfn_mediator
        .describe_stacks(integ_stack_name.to_string())
        .await
        .context(error::DescribeIntegStack { integ_stack_name })?;
    dbg!(stacks_details);
    Ok(())
}

/// Creates a new concrete implementation of [`CfnMediator`] using `rusoto`.
fn new_cfn(region: &str) -> Result<impl CfnMediator> {
    Ok(AwsCfnMediator::new(region).context(error::AwsCfnMediator { region })?)
}

fn get_stack_template(stack_file_name: &str) -> Result<String> {
    let template = fs::read_to_string(
        stacks_dir()
            .join(stack_file_name.to_string())
            .to_str()
            .context(error::StacksPath)?,
    )
    .context(error::ReadStack {
        stack_file_name: stack_file_name.to_string(),
    })?;
    Ok(template)
}

fn stacks_dir() -> PathBuf {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.pop();
    p.join("stacks")
}

type Result<T> = std::result::Result<T, error::Error>;
mod error {
    use snafu::Snafu;

    /// The error type for this module.
    #[derive(Debug, Snafu)]
    #[snafu(visibility = "pub(super)")]
    pub(crate) enum Error {
        /// The application failed to create instance of AwsCfnMediator
        #[snafu(display(
            "Failed to instantiate `AwsCfnMediator` for region '{}': {}",
            region,
            source
        ))]
        AwsCfnMediator {
            region: String,
            source: crate::aws::error::Error,
        },

        /// The application failed create integ cloudformation stack
        #[snafu(display(
            "Failed to create stack from template '{}': {}",
            integ_template_name,
            source
        ))]
        CreateIntegStack {
            integ_template_name: String,
            source: crate::aws::error::Error,
        },

        /// The application failed to describe integ cloudformation stack
        #[snafu(display("Failed to describe stack '{}': {}", integ_stack_name, source))]
        DescribeIntegStack {
            integ_stack_name: String,
            source: crate::aws::error::Error,
        },

        /// The application failed to read stack template
        #[snafu(display("Failed to read stack file '{}': {}", stack_file_name, source))]
        ReadStack {
            stack_file_name: String,
            source: std::io::Error,
        },

        /// The application failed to get stacks base path
        #[snafu(display("Failed to get stacks base path"))]
        StacksPath,
    }
}

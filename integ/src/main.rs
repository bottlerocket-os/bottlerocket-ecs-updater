mod aws;

use crate::aws::{AwsCfnMediator, CfnMediator};
use rusoto_cloudformation::Parameter;
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
    /// The Bottlerocket `aws-ecs-1` variant image id
    #[structopt(long, env = "BOTTLEROCKET_ECS_IMAGE_ID")]
    pub image_id: String,
    /// The Bottlerocket ecs updater ECR Image
    #[structopt(long, env = "BOTTLEROCKET_ECS_UPDATER_IMAGE")]
    pub updater_image: String,
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
    // TODO: we would have to restructure below setup so that
    // integ stack is deployed only once per account and we are able to run
    // multiple integ test in parallel, as of now setup looks more like an application.
    //  Below calls are added to demonstrate api calls and setup integ infrastructure temporarily.
    let integ_template_name = "integ-shared.yaml";
    let integ_stack_name = "bottlerocket-ecs-updater-integ-shared";
    let cluster_template_name = "cluster.yaml";
    // TODO: Add UUID at the end so we can launch multiple cluster.
    let cluster_stack_name = "bottlerocket-ecs-updater-integ-cluster";
    // TODO: Add UUID at the end so we can launch multiple cluster.
    let cluster_name = "updater-test-cluster";
    let updater_template_name = "bottlerocket-ecs-updater.yaml";
    // TODO: Add UUID at the end so we can test multiple cluster.
    let updater_stack_name = "bottlerocket-ecs-updater";

    let integ_template =
        get_stack_template(integ_stacks_dir().join(integ_template_name.to_string()))?;
    let cfn_mediator = new_cfn(&args.region)?;
    // TODO: check if stack already exist.
    cfn_mediator
        .create_stack(integ_template, integ_stack_name.to_string(), None)
        .await
        .context(error::CreateIntegStack {
            integ_template_name,
        })?;
    // TODO: check stack status for completion instead of sleep
    thread::sleep(time::Duration::from_secs(200));

    let stacks_details = cfn_mediator
        .describe_stacks(integ_stack_name.to_string())
        .await
        .context(error::DescribeIntegStack { integ_stack_name })?;
    dbg!(stacks_details.clone());

    let cluster_template =
        get_stack_template(integ_stacks_dir().join(cluster_template_name.to_string()))?;
    let cluster_params = vec![
        Parameter {
            parameter_key: Some(String::from("IntegSharedResourceStack")),
            parameter_value: Some(integ_stack_name.to_string()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("ClusterName")),
            parameter_value: Some(cluster_name.to_string()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("ImageID")),
            parameter_value: Some(args.image_id.to_string()),
            ..Parameter::default()
        },
    ];
    cfn_mediator
        .create_stack(
            cluster_template,
            cluster_stack_name.to_string(),
            Some(cluster_params),
        )
        .await
        .context(error::CreateClusterStack {
            cluster_template_name,
        })?;

    // TODO: check stack status and no of instances in cluster for completion instead of sleep
    thread::sleep(time::Duration::from_secs(200));

    let updater_params = vec![
        Parameter {
            parameter_key: Some(String::from("ClusterName")),
            parameter_value: Some(cluster_name.to_string()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("UpdaterImage")),
            parameter_value: Some(args.updater_image.clone()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("Subnets")),
            parameter_value: Some(
                stacks_details[0]
                    .outputs
                    .get("PublicSubnets")
                    .context(error::MissingPublicSubnets)?
                    .to_string(),
            ),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("LogGroupName")),
            parameter_value: Some(
                stacks_details[0]
                    .outputs
                    .get("LogGroup")
                    .context(error::MissingLogGroup)?
                    .to_string(),
            ),
            ..Parameter::default()
        },
    ];
    let updater_template =
        get_stack_template(updater_stacks_dir().join(updater_template_name.to_string()))?;
    cfn_mediator
        .create_stack(
            updater_template,
            updater_stack_name.to_string(),
            Some(updater_params),
        )
        .await
        .context(error::CreateUpdaterStack {
            updater_template_name,
        })?;
    // TODO: clean up.
    Ok(())
}

// Creates a new concrete implementation of [`CfnMediator`] using `rusoto`.
fn new_cfn(region: &str) -> Result<impl CfnMediator> {
    Ok(AwsCfnMediator::new(region).context(error::AwsCfnMediator { region })?)
}

fn get_stack_template(file_path: PathBuf) -> Result<String> {
    let template = fs::read_to_string(&file_path).context(error::ReadStack {
        stack_template_path: &file_path,
    })?;
    Ok(template)
}

fn integ_stacks_dir() -> PathBuf {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.pop();
    p.join("integ").join("stacks")
}

fn updater_stacks_dir() -> PathBuf {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.pop();
    p.join("stacks")
}

type Result<T> = std::result::Result<T, error::Error>;
mod error {
    use snafu::Snafu;
    use std::path::PathBuf;

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

        /// The application failed create cluster using cloudformation stack
        #[snafu(display(
            "Failed to create cluster from template '{}': {}",
            cluster_template_name,
            source
        ))]
        CreateClusterStack {
            cluster_template_name: String,
            source: crate::aws::error::Error,
        },

        /// The application failed create integ shared cloudformation stack
        #[snafu(display(
            "Failed to create stack from template '{}': {}",
            integ_template_name,
            source
        ))]
        CreateIntegStack {
            integ_template_name: String,
            source: crate::aws::error::Error,
        },

        /// The application failed create updater stack
        #[snafu(display(
            "Failed to create updater stack from template '{}': {}",
            updater_template_name,
            source
        ))]
        CreateUpdaterStack {
            updater_template_name: String,
            source: crate::aws::error::Error,
        },

        /// The application failed to describe integ shared cloudformation stack
        #[snafu(display("Failed to describe stack '{}': {}", integ_stack_name, source))]
        DescribeIntegStack {
            integ_stack_name: String,
            source: crate::aws::error::Error,
        },

        /// The application failed to find LogGroup output in integ shared stack
        #[snafu(display("Missing output LogGroup in integ shared stack"))]
        MissingLogGroup,

        /// The application failed to find PublicSubnets output in integ shared stack
        #[snafu(display("Missing output PublicSubnets in integ shared stack"))]
        MissingPublicSubnets,

        /// The application failed to read stack template
        #[snafu(display("Failed to read stack template '{}': {}", stack_template_path.display(), source))]
        ReadStack {
            stack_template_path: PathBuf,
            source: std::io::Error,
        },
    }
}

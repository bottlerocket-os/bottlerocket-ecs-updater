mod aws;
mod error;
mod integ_args;

use crate::integ_args::IntegArgs;
extern crate base64;
use crate::error;
use log::{debug, info};
use rusoto_cloudformation::{CloudFormationClient, Parameter};
use rusoto_ec2::Ec2Client;
use rusoto_ecs::EcsClient;
use simplelog::{Config as LogConfig, SimpleLogger};
use snafu::{ensure, OptionExt, ResultExt};
use std::path::PathBuf;
use std::thread;
use std::{fs, process};
use structopt::StructOpt;
use tokio::time::{delay_for, Duration};
use uuid::Uuid;

// Returning a Result from main makes it print a Debug representation of the error, but with Snafu
// we have nice Display representations of the error, so we wrap "main" (run) and print any error.
// https://github.com/shepmaster/snafu/issues/110
#[tokio::main]
async fn main() {
    if let Err(e) = run().await {
        eprintln!("{}", e);
        process::exit(1);
    }
}

async fn run() -> error::Result<()> {
    let args = IntegArgs::from_args();
    // Region is required
    ensure!(!args.region.is_empty(), error::EmptyRegion);
    // Log setup
    SimpleLogger::init(args.log_level, LogConfig::default()).context(error::Logger)?;
    let region = aws::region_from_string(&args.region).context(error::InvalidRegion)?;
    let ecs_client = aws::client::build_client::<EcsClient>(&region).context(error::EcsClient)?;
    let ec2_client = aws::client::build_client::<Ec2Client>(&region).context(error::Ec2Client)?;
    let cfn_client =
        aws::client::build_client::<CloudFormationClient>(&region).context(error::CfnClient)?;
    let aws_api = Box::new(aws::api::AwsMediator::new_with(
        ecs_client, ec2_client, cfn_client,
    ));
    _run(&args, aws_api).await
}

async fn _run(args: &IntegArgs, aws_api: Box<dyn aws::api::Mediator>) -> error::Result<()> {
    let random_guid = Uuid::new_v4().to_simple().to_string();
    let cluster_name = format!("bottlerocket-ecs-updater-integ-{}", random_guid);
    let integ_stack_name = "bottlerocket-ecs-updater-integ";
    let updater_stack_name = format!("bottlerocket-ecs-updater-{}", random_guid);
    let integ_stack_file_name = "integ.yaml";
    let updater_stack_file_name = "bottlerocket-ecs-updater.yaml";
    const WAIT_STACK_TIMEOUT_SECS: u64 = 200;

    let describe_stack_result = aws_api.describe_stacks(integ_stack_name.to_string()).await;
    // TODO check for create complete status of stack, it can happen that stack may exist but it is in delete state
    match describe_stack_result {
        Ok(_) => println!("Stack Already exist"),
        Err(_) => {
            println!(
                "creating `{}` cloudformation stack",
                integ_stack_file_name.to_string()
            );

            let template = get_template(integ_stack_file_name)?;
            aws_api
                .create_stack(template, integ_stack_name.to_string(), None)
                .await;

            info!(
                "waiting for {} stack creation to complete",
                integ_stack_name
            );
            let mut timeout = delay_for(Duration::from_secs(WAIT_STACK_TIMEOUT_SECS));
            let stack_completion = wait_stack_completion(aws_api, integ_stack_name.to_string());
            tokio::select! {
                            status = stack_completion => info!("Completed creating {} stack : {}",integ_stack_name, status),
                            _ =  &mut timeout => {

                            eprintln!("Failed to get {} stack completion status : Timed out after {}", integ_stack_name, WAIT_STACK_TIMEOUT_SECS);
                            std::process::exit(1);
                            },
            }
        }
    }
    info!("creating cluster {} to run tests", cluster_name);
    let cluster_details = aws_api
        .create_cluster(cluster_name.to_string())
        .await
        .context(error::CreateTestCluster {
            cluster_name: cluster_name.to_string(),
        })?;
    info!("cluster {} created", cluster_name);

    info!(
        "describing {} stack resources to get subnet and security group id",
        integ_stack_name
    );
    let integ_stack_resources = aws_api
        .describe_stack_resources(integ_stack_name.to_string())
        .await
        .context(error::StartIntegStack {
            stack_name: integ_stack_name.to_string(),
        })?;

    // TODO make sure we have all our stack resource strings.
    info!("adding a instance to the cluster {}", cluster_name);
    let instances = aws_api
        .run_instances(
            args.ami_id.clone(),
            cluster_name.to_string(),
            integ_stack_resources.subnet2_id.clone(),
            integ_stack_resources.security_group_id.clone(),
            integ_stack_resources.ecs_instance_profile_id,
        )
        .await
        .context(error::AddClusterInstances {
            cluster_name: cluster_name.to_string(),
        });

    start_updater_stack(
        aws_api,
        &args,
        updater_stack_file_name,
        &updater_stack_name,
        &cluster_details.cluster_arn,
        &integ_stack_resources.subnet1_id,
        &integ_stack_resources.subnet2_id,
    )
    .await?;
    // TODO wait for instance to join the cluster
    info!(
        "instances {:?} added to the cluster {}",
        instances, cluster_name
    );
    Ok(())
    // TODO : validate and cleanup
    // delete_cfn_stack(cloudformation_client.clone(), integ_stack_name.clone())
    //     .await
    //     .expect("TODO");
}

async fn start_updater_stack(
    &aws_api: Box<dyn aws::api::Mediator>,
    args: &IntegArgs,
    stack_file_name: &str,
    stack_name: &str,
    cluster_arn: &str,
    subnet1_id: &str,
    subnet2_id: &str,
) -> error::Result<()> {
    let params = vec![
        Parameter {
            parameter_key: Some(String::from("EcsClusterArn")),
            parameter_value: Some(cluster_arn.to_string()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("UpdaterImage")),
            parameter_value: Some(args.updater_image.clone()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("EcsClusterVPCSubnet1")),
            parameter_value: Some(subnet1_id.to_string()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("EcsClusterVPCSubnet2")),
            parameter_value: Some(subnet2_id.to_string()),
            ..Parameter::default()
        },
    ];
    let template = get_template(stack_file_name)?;
    aws_api
        .create_stack(template, stack_name.to_string(), Some(params))
        .await
        .context(error::UpdaterStack {
            stack_name: stack_name.to_string(),
        })?;
    Ok(())
}

fn get_template(stack_file_name: &str) -> error::Result<String> {
    let template = fs::read_to_string(
        stacks_location()
            .join(stack_file_name.to_string())
            .to_str()
            .context(error::StacksPath)?,
    )
    .context(error::ReadStack {
        stack_file_name: stack_file_name.to_string(),
    })?;
    Ok(template)
}

fn stacks_location() -> PathBuf {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.pop();
    p.join("stacks")
}

async fn wait_stack_completion(&aws_api: Box<dyn aws::api::Mediator>, stack_name: String) -> bool {
    loop {
        let describe_output = aws_api.describe_stacks(stack_name.clone()).await.unwrap();
        if let Some(stacks) = describe_output.stacks {
            for stack in stacks {
                if stack.stack_status == "CREATE_COMPLETE".to_string() {
                    return true;
                }
                if stack.stack_status == "DELETE_ROLLBACK".to_string() {
                    return false;
                }
            }
        }
        thread::sleep(Duration::from_secs(3));
    }
}

mod integ_args;

use crate::integ_args::IntegArgs;
extern crate base64;
use anyhow::Result;
use rusoto_cloudformation::{
    CloudFormation, CloudFormationClient, CreateStackInput, DeleteStackInput,
    DescribeStackResourcesInput, DescribeStackResourcesOutput, DescribeStacksInput,
    DescribeStacksOutput, Parameter,
};
use rusoto_core::HttpClient;
use rusoto_credential::DefaultCredentialsProvider;
use rusoto_ec2::{
    Ec2, Ec2Client, IamInstanceProfileSpecification, Reservation, RunInstancesRequest,
    TagSpecification,
};
use rusoto_ecs::{
    CapacityProviderStrategyItem, CreateClusterRequest, CreateClusterResponse, Ecs, EcsClient,
};
use std::fs;
use std::path::PathBuf;
use std::str::FromStr;
use std::thread;
use structopt::StructOpt;
use tokio::time::{delay_for, Duration};
use uuid::Uuid;

#[tokio::main]
async fn main() {
    let args = IntegArgs::from_args();
    println!("{:?}", args);
    let random_guid = Uuid::new_v4().to_simple().to_string();
    let cluster_name = format!("bottlerocket-ecs-updater-integ-{}", random_guid);
    let integ_stack_name = "bottlerocket-ecs-updater-integ";
    let updater_stack_name = format!("bottlerocket-ecs-updater-{}", random_guid);
    let integ_stack_file_name = "integ.yaml";
    let updater_stack_file_name = "bottlerocket-ecs-updater.yaml";
    const WAIT_STACK_TIMEOUT_SECS: u64 = 200;

    let cw_dispatcher = HttpClient::new().expect("TODO");
    let cred_provider = DefaultCredentialsProvider::new().unwrap();
    let ec2_client = Ec2Client::new_with(
        cw_dispatcher,
        cred_provider.clone(),
        rusoto_core::region::Region::from_str(&args.region).expect("TODO"),
    );

    let cfn_dispatcher = HttpClient::new().expect("TODO");
    let cloudformation_client = CloudFormationClient::new_with(
        cfn_dispatcher,
        cred_provider.clone(),
        rusoto_core::region::Region::from_str(&args.region).expect("TODO"),
    );

    let ecs_dispatcher = HttpClient::new().expect("TODO");
    let ecs_client = EcsClient::new_with(
        ecs_dispatcher,
        cred_provider.clone(),
        rusoto_core::region::Region::from_str(&args.region).expect("TODO"),
    );
    let describe_stack_result =
        describe_cfn_stack(cloudformation_client.clone(), integ_stack_name.to_string()).await;
    match describe_stack_result {
        // TODO check for create complete status of stack, it can happen that stack may exist but it is delete state
        Ok(_) => {
            println!("`{}` stack already exists", integ_stack_name)
        }
        Err(_) => {
            create_cfn_stack(
                cloudformation_client.clone(),
                integ_stack_file_name,
                integ_stack_name.to_string(),
                None,
            )
            .await;
            println!("creating `{}` cloudformation stack", integ_stack_file_name);

            println!(
                "waiting for {} stack creation to complete",
                integ_stack_name
            );
            let mut timeout = delay_for(Duration::from_secs(WAIT_STACK_TIMEOUT_SECS));
            let stack_completion =
                wait_stack_completion(cloudformation_client.clone(), integ_stack_name.to_string());
            tokio::select! {
                status = stack_completion => println!("Completed creating {} stack : {}",integ_stack_name, status),
                _ =  &mut timeout => println!("Failed to get {} stack completion status : Timed out after {}", integ_stack_name, WAIT_STACK_TIMEOUT_SECS),
            }
        }
    }

    println!("creating cluster {} to run tests", cluster_name);
    let create_cluster_result = create_cluster(ecs_client, cluster_name.to_string()).await;
    let cluster_arn = create_cluster_result
        .cluster
        .and_then(|cluster_details| cluster_details.cluster_arn);
    println!("cluster {} created", cluster_name);

    println!(
        "describing {} stack resources to get subnet and security group id",
        integ_stack_name
    );
    let integ_stack_resources =
        describe_cfn_stack_resources(cloudformation_client.clone(), integ_stack_name.to_string())
            .await;
    let mut subnet1_id = String::new();
    let mut subnet2_id = String::new();
    let mut security_group_id = String::new();
    let mut instance_role_id = String::new();
    if let Some(stack_resources) = integ_stack_resources.stack_resources {
        for resource in stack_resources {
            match resource.logical_resource_id.as_str() {
                "Subnet1" => subnet1_id = resource.physical_resource_id.unwrap(),
                "Subnet2" => subnet2_id = resource.physical_resource_id.unwrap(),
                "SecurityGroup" => security_group_id = resource.physical_resource_id.unwrap(),
                "EcsInstanceProfile" => instance_role_id = resource.physical_resource_id.unwrap(),
                _ => println!(
                    "Resource {} information not required",
                    resource.logical_resource_id
                ),
            }
        }
    }

    println!("adding a instance to the cluster {}", cluster_name);
    let add_instance_result = add_instance(
        ec2_client.clone(),
        args.ami_id.clone(),
        cluster_name.to_string(),
        subnet2_id.clone(),
        security_group_id.clone(),
        instance_role_id.clone(),
    )
    .await;
    // TODO wait for instance to join the cluster

    if let Some(instances) = add_instance_result.instances {
        for instance in instances {
            println!(
                "instance {} added to the cluster {}",
                instance.instance_id.unwrap().as_str(),
                cluster_name
            );
        }
    } else {
        println!("Instance not started");
    }

    let params = vec![
        Parameter {
            parameter_key: Some(String::from("EcsClusterArn")),
            parameter_value: cluster_arn,
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("UpdaterImage")),
            parameter_value: Some(args.updater_image),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("EcsClusterVPCSubnet1")),
            parameter_value: Some(subnet1_id.clone()),
            ..Parameter::default()
        },
        Parameter {
            parameter_key: Some(String::from("EcsClusterVPCSubnet2")),
            parameter_value: Some(subnet2_id.clone()),
            ..Parameter::default()
        },
    ];

    println!("creating stack {} to start updates", updater_stack_name);
    create_cfn_stack(
        cloudformation_client.clone(),
        updater_stack_file_name,
        updater_stack_name.to_string(),
        Some(params),
    )
    .await;

    // TODO : validate and cleanup
    // delete_cfn_stack(cloudformation_client.clone(), integ_stack_name.clone())
    //     .await
    //     .expect("TODO");
}

fn stacks_location() -> PathBuf {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.pop();
    p.join("stacks")
}

async fn wait_stack_completion(
    cloudformation_client: CloudFormationClient,
    stack_name: String,
) -> bool {
    loop {
        let describe_output = describe_cfn_stack(cloudformation_client.clone(), stack_name.clone())
            .await
            .unwrap();
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

async fn create_cfn_stack(
    cloudformation_client: CloudFormationClient,
    stack_file_name: &str,
    stack_name: String,
    parameters: Option<Vec<Parameter>>,
) {
    let stack_path = stacks_location().join(stack_file_name);
    cloudformation_client
        .create_stack(CreateStackInput {
            capabilities: Some(vec![String::from("CAPABILITY_NAMED_IAM")]),
            stack_name,
            template_body: Some(fs::read_to_string(stack_path.to_str().unwrap()).unwrap()),
            parameters,
            ..CreateStackInput::default()
        })
        .await
        .expect("TODO");
}

async fn describe_cfn_stack(
    cloudformation_client: CloudFormationClient,
    stack_name: String,
) -> Result<DescribeStacksOutput, Box<dyn std::error::Error>> {
    let result = cloudformation_client
        .describe_stacks(DescribeStacksInput {
            stack_name: Some(stack_name),
            ..DescribeStacksInput::default()
        })
        .await?;
    Ok(result)
}

async fn describe_cfn_stack_resources(
    cloudformation_client: CloudFormationClient,
    stack_name: String,
) -> DescribeStackResourcesOutput {
    cloudformation_client
        .describe_stack_resources(DescribeStackResourcesInput {
            stack_name: Some(stack_name),
            ..DescribeStackResourcesInput::default()
        })
        .await
        .expect("TODO")
}

async fn delete_cfn_stack(cloudformation_client: CloudFormationClient, stack_name: String) {
    cloudformation_client
        .delete_stack(DeleteStackInput {
            stack_name,
            ..DeleteStackInput::default()
        })
        .await
        .expect("TODO");
}

async fn create_cluster(ecs_client: EcsClient, cluster_name: String) -> CreateClusterResponse {
    return ecs_client
        .create_cluster(CreateClusterRequest {
            capacity_providers: Some(vec!["FARGATE".to_string()]),
            cluster_name: Some(cluster_name),
            default_capacity_provider_strategy: Some(vec![CapacityProviderStrategyItem {
                capacity_provider: String::from("FARGATE"),
                weight: Some(1),
                base: None,
            }]),
            settings: None,
            tags: Some(vec![rusoto_ecs::Tag {
                key: Some(String::from("category")),
                value: Some("ecs-updater-integ".to_string()),
            }]),
        })
        .await
        .expect("TODO");
}

async fn add_instance(
    ec2_client: Ec2Client,
    ami_id: String,
    cluster_name: String,
    subnet_id: String,
    security_group_id: String,
    instance_role_id: String,
) -> Reservation {
    let userdata_template = r#"[settings.ecs]
cluster = "CLUSTER_NAME"
"#;
    let userdata = str::replace(userdata_template, "CLUSTER_NAME", &cluster_name);
    return ec2_client
        .run_instances(RunInstancesRequest {
            subnet_id: Some(subnet_id.to_owned()),
            image_id: Some(ami_id),
            max_count: 1,
            min_count: 1,
            instance_type: Some("c3.large".into()),
            security_group_ids: Some(vec![security_group_id.to_owned()]),
            tag_specifications: Some(vec![TagSpecification {
                resource_type: Some(String::from("instance")),
                tags: Some(vec![rusoto_ec2::Tag {
                    key: Some(String::from("cluster")),
                    value: Some(cluster_name.to_owned()),
                }]),
            }]),
            iam_instance_profile: Some(IamInstanceProfileSpecification {
                name: Some(instance_role_id),
                arn: None,
            }),
            user_data: Some(base64::encode(userdata)),
            ..RunInstancesRequest::default()
        })
        .await
        .expect("TODO");
}

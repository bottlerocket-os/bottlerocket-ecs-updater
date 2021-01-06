mod args;

use crate::args::Args;
use rusoto_core::HttpClient;
use rusoto_credential::DefaultCredentialsProvider;
use rusoto_ecs::{
    DescribeContainerInstancesRequest, Ecs, EcsClient, ListContainerInstancesRequest,
};
use rusoto_ssm::{GetCommandInvocationRequest, SendCommandRequest, Ssm, SsmClient};
use std::collections::HashMap;
use std::str::FromStr;
use std::{thread, time};
use structopt::StructOpt;

#[tokio::main]
async fn main() {
    let args = Args::from_args();

    let cw_dispatcher = HttpClient::new().expect("TODO 1");
    let cred_provider = DefaultCredentialsProvider::new().unwrap();
    let ecs_client = EcsClient::new_with(
        cw_dispatcher,
        cred_provider.clone(),
        rusoto_core::region::Region::from_str(&args.region).expect("TODO 3"),
    );
    let request = ListContainerInstancesRequest {
        cluster: Some(args.cluster_arn.clone()),
        filter: None,
        max_results: None,
        next_token: None,
        status: None,
    };

    let list = ecs_client
        .list_container_instances(request)
        .await
        .expect("TODO 4");
    if let Some(arns) = list.container_instance_arns.clone() {
        println!("response received");
        for arn in arns {
            println!("{}", arn.clone());
        }
    } else {
        eprintln!("failed to list containers");
    }

    // we need ec2 instance id to send ssm command.
    let describe_instance_result = ecs_client
        .describe_container_instances(DescribeContainerInstancesRequest {
            cluster: Some(args.cluster_arn.clone()),
            container_instances: list.container_instance_arns.unwrap(),
            include: None,
        })
        .await
        .expect("TODO");

    let mut params = HashMap::new();
    params.insert("commands".into(), vec!["apiclient -u /settings".into()]);

    let http_dispatcher = HttpClient::new().expect("TODO 1");
    let ssm_client = SsmClient::new_with(
        http_dispatcher,
        cred_provider,
        rusoto_core::region::Region::from_str(&args.region).expect("TODO 5"),
    );

    if let Some(instance_details) = describe_instance_result.container_instances {
        println!("Get  Bottlerocket Instance Settings");
        for instance in instance_details {
            let instance_id = instance.ec_2_instance_id.unwrap();
            println!("Instance id : {}", instance_id);
            let send_command_result = ssm_client
                .send_command(SendCommandRequest {
                    comment: Some("Get all settings".into()),
                    instance_ids: Some(vec![instance_id.clone()]),
                    document_name: String::from("AWS-RunShellScript"),
                    document_version: Some("1".into()),
                    parameters: Some(params.clone()),
                    timeout_seconds: Some(60),
                    ..SendCommandRequest::default()
                })
                .await
                .expect("TODO 8");
            // give some time for command to complete
            thread::sleep(time::Duration::from_secs(3));

            // fetch command result
            if let Some(command) = send_command_result.command {
                let result = ssm_client
                    .get_command_invocation(GetCommandInvocationRequest {
                        command_id: command.command_id.unwrap(),
                        instance_id: instance_id.clone(),
                        plugin_name: None,
                    })
                    .await
                    .expect("TODO");
                println!("{:?}", result.standard_output_content);
            } else {
                println!("{}", "send command is empty")
            }
        }
    } else {
        eprintln!("failed to list containers");
    }
}

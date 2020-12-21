mod args;
use crate::args::Args;
use rusoto_core::HttpClient;
use rusoto_ecs::{Ecs, EcsClient, ListContainerInstancesRequest};
use rusoto_sts::{StsAssumeRoleSessionCredentialsProvider, StsClient};
use std::str::FromStr;
use structopt::StructOpt;

#[tokio::main]
async fn main() {
    let args = Args::from_args();

    let cw_dispatcher = HttpClient::new().expect("TODO 1");
    let cw_sts_client =
        StsClient::new(rusoto_core::region::Region::from_str("us-west-2").expect("TODO 2"));
    let cw_session_cred_provider = StsAssumeRoleSessionCredentialsProvider::new(
        cw_sts_client,
        "arn:aws:iam::554409873180:role/bottlerocket-ecs-updater/bottlerocket-ecs-updater-CrossAccountAccessRole-1EAFLBCNQSGT5".into(),
        "bottlerocket-ecs-updater-session".to_owned(),
        None,
        None,
        None,
        None,
    );

    let client = EcsClient::new_with(
        cw_dispatcher,
        cw_session_cred_provider,
        rusoto_core::region::Region::from_str("us-west-2").expect("TODO 3"),
    );

    let request = ListContainerInstancesRequest {
        cluster: Some("arn:aws:ecs:us-west-2:554409873180:cluster/bottlerocket".into()),
        filter: None,
        max_results: None,
        next_token: None,
        status: None,
    };

    let list = client
        .list_container_instances(request)
        .await
        .expect("TODO 4");

    if let Some(arns) = list.container_instance_arns {
        println!("response received");
        for arn in arns {
            println!("{}", arn);
        }
    } else {
        eprintln!("failed to list containers");
    }

    // rusoto_core::Client::new_with()
    println!("{:?}", args)
}

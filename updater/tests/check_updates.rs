mod mock;
use bottlerocket_ecs_updater::args::Args;
use bottlerocket_ecs_updater::aws::api::{
    ContainerInstances, Instances, SSMCommandResponse, SSMInvocationResult,
};
use bottlerocket_ecs_updater::check::check_updates;
use mock::AwsMediatorMock;
use std::collections::HashMap;

#[tokio::test]
async fn sample_test() {
    let cluster_arn = "test_cluster_arn";
    let test_region = "some_region";
    let container_instance_arns = vec!["container_instance_1".to_string()];
    let instance_1 = "instance1";
    let instance_ids = vec![instance_1.to_string()];
    let command_id = "test_command_id";
    let mut params = HashMap::new();
    params.insert("commands".into(), vec!["apiclient update check".into()]);
    let mediator_mock = AwsMediatorMock::new();
    mediator_mock
        .list_container_instances
        .given(cluster_arn.to_string())
        .will_return(Ok(ContainerInstances {
            container_instance_arns: container_instance_arns.clone(),
        }));
    mediator_mock
        .describe_container_instances
        .given((cluster_arn.to_string(), container_instance_arns.clone()))
        .will_return(Ok(Instances {
            instance_ids: instance_ids.clone(),
        }));
    mediator_mock
        .send_command
        .given((instance_ids.clone(), params, Some(120)))
        .will_return(Ok(SSMCommandResponse {
            command_id: command_id.to_string(),
        }));
    mediator_mock
        .get_command_invocation
        .given((command_id.to_string(), instance_1.to_string()))
        .will_return(Ok(SSMInvocationResult {
            output: "Some command output".to_string(),
            status: "Some command status".to_string(),
            status_details: "Some command details".to_string(),
            response_code: 0,
        }));
    async {
        let args = Args {
            cluster_arn: cluster_arn.to_string(),
            region: test_region.to_string(),
        };
        let aws_mediator_mock = Box::new(mediator_mock.clone());
        let result = check_updates(&args, aws_mediator_mock).await.unwrap();
        assert_eq!(result, ());
    }
    .await;
}

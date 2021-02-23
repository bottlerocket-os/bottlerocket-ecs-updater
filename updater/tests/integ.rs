mod mocks;
use bottlerocket_ecs_updater::{EcsMediator, Instance, Instances};
use mocks::MockEcsMediator;

#[tokio::test]
#[cfg(test)]
/// Currently this test only demonstrates our ability to create a mock of the `EcsMediator` trait
/// and call its functions. As program logic becomes more complicated, we can test interesting
/// things.
async fn sample_test() {
    let cluster = "test_cluster";
    let expected = Instances {
        bottlerocket_instances: vec![Instance {
            instance_id: "container_instance_1".to_string(),
            status: "Active".to_string(),
        }],
        next_token: None,
    };
    let ecs_mock = MockEcsMediator::new();
    ecs_mock
        .list_bottlerocket_instances
        .given((cluster.to_string(), None, None))
        .will_return(Ok(expected));

    let listed_instances = ecs_mock
        .list_bottlerocket_instances(cluster, None, None)
        .await
        .unwrap();

    assert_eq!(1, listed_instances.bottlerocket_instances.len());
    assert_eq!(
        "container_instance_1",
        listed_instances
            .bottlerocket_instances
            .get(0)
            .unwrap()
            .instance_id
    );
    assert_eq!(None, listed_instances.next_token);
}

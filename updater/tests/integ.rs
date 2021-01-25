mod mocks;
use bottlerocket_ecs_updater::EcsMediator;
use mocks::MockEcsMediator;

#[tokio::test]
#[cfg(test)]
/// Currently this test only demonstrates our ability to create a mock of the `EcsMediator` trait
/// and call its functions. As program logic becomes more complicated, we can test interesting
/// things.
async fn sample_test() {
    let cluster = "test_cluster";
    let expected = vec!["container_instance_1".to_string()];
    let ecs_mock = MockEcsMediator::new();
    ecs_mock
        .list_container_instances
        .given(cluster.to_string())
        .will_return(Ok(expected));

    let listed_instances = ecs_mock.list_container_instances(cluster).await.unwrap();

    assert_eq!(1, listed_instances.len());
    assert_eq!("container_instance_1", listed_instances.get(0).unwrap());
}

use async_trait::async_trait;
use bottlerocket_ecs_updater::{EcsMediator, Error, Result};
use mock_it::Mock;
use std::fmt::{Display, Formatter};

#[derive(Debug, Default, Clone, Eq, PartialEq)]
/// Reports any error that happens due to incorrect mocks, it implements `Send`, `Sync`
/// to format it as source `<Box<dyn std::error::Error + Send + Sync>>` which we can convert
/// to `aws::error::Error` by implementing `From` trait
pub struct MockErr {
    pub msg: Option<String>,
}

impl Display for MockErr {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        std::fmt::Debug::fmt(self, f)
    }
}

impl std::error::Error for MockErr {}
unsafe impl Sync for MockErr {}
unsafe impl Send for MockErr {}

pub type MockResult<T> = std::result::Result<T, MockErr>;

pub struct MockEcsMediator {
    pub list_container_instances: Mock<String, MockResult<Vec<String>>>,
}

#[async_trait]
impl EcsMediator for MockEcsMediator {
    async fn list_container_instances(&self, cluster: &str) -> Result<Vec<String>> {
        self.list_container_instances
            .called(cluster.to_string())
            .map_err(|e| Error::new(e))
    }
}

impl MockEcsMediator {
    pub fn new() -> MockEcsMediator {
        MockEcsMediator {
            list_container_instances: Mock::new(Err(MockErr {
                msg: Some("Mock does not exist for given input".into()),
            })),
        }
    }
}

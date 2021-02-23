use async_trait::async_trait;
use bottlerocket_ecs_updater::{EcsMediator, Error, Instances, Result};
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
    pub list_bottlerocket_instances:
        Mock<(String, Option<i64>, Option<String>), MockResult<Instances>>,
}

#[async_trait]
impl EcsMediator for MockEcsMediator {
    async fn list_bottlerocket_instances(
        &self,
        cluster: &str,
        max_results: Option<i64>,
        next_token: Option<String>,
    ) -> Result<Instances> {
        self.list_bottlerocket_instances
            .called((cluster.to_string(), max_results, next_token))
            .map_err(|e| Error::new(e))
    }
}

impl MockEcsMediator {
    pub fn new() -> MockEcsMediator {
        MockEcsMediator {
            list_bottlerocket_instances: Mock::new(Err(MockErr {
                msg: Some("Mock does not exist for given input".into()),
            })),
        }
    }
}

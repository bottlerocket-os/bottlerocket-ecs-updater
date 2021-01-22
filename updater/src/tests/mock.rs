use crate::aws::api::{
    ContainerInstances, Instances, Mediator, SSMCommandResponse, SSMInvocationResult,
};
use async_trait::async_trait;
use mock_it::Mock;
use snafu::{ResultExt, Snafu};
use std::collections::HashMap;
use std::result::Result;

#[derive(Clone)]
pub struct AwsMediatorMock {
    pub list_container_instances: Mock<String, Result<ContainerInstances, String>>,
    pub describe_container_instances: Mock<(String, Vec<String>), Result<Instances, String>>,
    pub send_command: Mock<
        (Vec<String>, HashMap<String, Vec<String>>, Option<i64>),
        Result<SSMCommandResponse, String>,
    >,
    pub get_command_invocation: Mock<(String, String), Result<SSMInvocationResult, String>>,
}

#[async_trait]
impl Mediator for AwsMediatorMock {
    async fn list_container_instances(
        &self,
        cluster_arn: String,
    ) -> Result<ContainerInstances, crate::aws::error::Error> {
        let result = self
            .list_container_instances
            .called(cluster_arn.to_string());
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        match result {
            Ok(a) => Ok(a),
            Err(e) => error::ListInstance { err: e }.fail()?,
        }
    }

    async fn describe_container_instances(
        &self,
        cluster_arn: String,
        container_instance_arns: &[String],
    ) -> Result<Instances, Box<dyn std::error::Error + Send + Sync + 'static>> {
        let result = self
            .describe_container_instances
            .called((cluster_arn.to_string(), container_instance_arns.to_vec()));
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        match result {
            Ok(a) => Ok(a),
            Err(e) => error::DescribeInstance { err: e }.fail()?,
        }
    }

    async fn send_command(
        &self,
        instance_ids: &[String],
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> Result<SSMCommandResponse, Box<dyn std::error::Error + Send + Sync + 'static>> {
        let result =
            self.send_command
                .called((instance_ids.to_vec(), params.clone(), timeout.clone()));
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        match result {
            Ok(a) => Ok(a),
            Err(e) => error::SendCommand { err: e }.fail()?,
        }
    }

    async fn list_command_invocation(
        &self,
        command_id: String,
        instance_id: String,
    ) -> Result<SSMInvocationResult, Box<dyn std::error::Error + Send + Sync + 'static>> {
        let result = self
            .get_command_invocation
            .called((command_id.clone(), instance_id.clone()));
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        match result {
            Ok(a) => Ok(a),
            Err(e) => error::GetCommandInvocation { err: e }.fail()?,
        }
    }
}

impl AwsMediatorMock {
    pub fn new() -> AwsMediatorMock {
        AwsMediatorMock {
            list_container_instances: Mock::new(Err("Failed to match given inputs".to_string())),
            describe_container_instances: Mock::new(
                Err("Failed to match given inputs".to_string()),
            ),
            send_command: Mock::new(Err("Failed to match given inputs".to_string())),
            get_command_invocation: Mock::new(Err("Failed to match given inputs".to_string())),
        }
    }
}

mod error {
    use snafu::Snafu;

    #[derive(Debug, Snafu)]
    #[snafu(visibility = "pub(crate)")]
    pub(crate) enum Error {
        #[snafu(display("Mock failed : {}", err))]
        ListInstance { err: String },

        #[snafu(display("Mock failed : {}", err))]
        DescribeInstance { err: String },

        #[snafu(display("Mock failed : {}", err))]
        SendCommand { err: String },

        #[snafu(display("Mock failed : {}", err))]
        GetCommandInvocation { err: String },
    }
}

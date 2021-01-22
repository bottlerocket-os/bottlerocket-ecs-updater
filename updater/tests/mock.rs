use async_trait::async_trait;
use bottlerocket_ecs_updater::aws;
use bottlerocket_ecs_updater::aws::api::{
    ContainerInstances, Instances, Mediator, SSMCommandResponse, SSMInvocationResult,
};
use mock_it::Mock;
use snafu::{ResultExt, Snafu};
use std::collections::HashMap;
use std::result::Result;
use rusoto_core::RusotoError;

#[derive(Clone)]
pub struct AwsMediatorMock {
    pub list_container_instances: Mock<String, aws::error::Result<ContainerInstances>,
    pub describe_container_instances:
        Mock<(String, Vec<String>), aws::error::Result<Instances>,
    pub send_command: Mock<
        (Vec<String>, HashMap<String, Vec<String>>, Option<i64>),
        aws::error::Result<SSMCommandResponse>,
    >,
    pub list_command_invocations:
        Mock<(String), aws::error::Result<Vec<SSMInvocationResult>>,
}

#[async_trait]
impl Mediator for AwsMediatorMock {
    async fn list_container_instances(
        &self,
        cluster_arn: String,
    ) -> aws::error::Result<ContainerInstances> {
        self
            .list_container_instances
            .called(cluster_arn.to_string())
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        // match result {
        //     Ok(a) => Ok(a),
        //     Err(e) => error::ListInstance { err: e }.fail()?,
        // }
    }

    async fn describe_container_instances(
        &self,
        cluster_arn: String,
        container_instance_arns: &[String],
    ) -> aws::error::Result<Instances> {
        self
            .describe_container_instances
            .called((cluster_arn.to_string(), container_instance_arns.to_vec()))
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        // match result {
        //     Ok(a) => Ok(a),
        //     Err(e) => error::DescribeInstance { err: e }.fail()?,
        // }
    }

    async fn send_command(
        &self,
        instance_ids: &[String],
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> aws::error::Result<SSMCommandResponse> {
            self.send_command
                .called((instance_ids.to_vec(), params.clone(), timeout.clone()))
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        // match result {
        //     Ok(a) => Ok(a),
        //     Err(e) => error::SendCommand { err: e }.fail()?,
        // }
    }

    async fn list_command_invocations(
        &self,
        command_id: String,
    ) -> aws::error::Result<Vec<SSMInvocationResult>> {
        self
            .list_command_invocations
            .called((command_id.clone()))
        // TODO: find a better way to convert enum std::result::Result<_, Box<(dyn snafu::Error + Send + Sync + 'static)>>
        // to enum `std::result::Result<_, mock::error::Error>`
        // match result {
        //     Ok(a) => Ok(a),
        //     Err(e) => error::GetCommandInvocation { err: e }.fail()?,
        // }
    }
}

impl AwsMediatorMock {
    pub fn new() -> AwsMediatorMock {
        AwsMediatorMock {
            list_container_instances: Mock::new((error::Error::ListInstance {err: "Heeloo".to_string() })),
            describe_container_instances: Mock::new(
                error::Error::ListInstance {err: "Heeloo".to_string() }),

            send_command: Mock::new(error::Error::ListInstance {err: "Heeloo".to_string() }),
            list_command_invocations: Mock::new(error::Error::ListInstance {err: "Heeloo".to_string() }),
        }
    }
}

mod error {
    use snafu::Snafu;

    #[derive(Debug, Snafu, Clone)]
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

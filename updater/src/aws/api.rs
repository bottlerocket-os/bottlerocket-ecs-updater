use crate::aws::error;
use async_trait::async_trait;
use rusoto_ecs::{
    DescribeContainerInstancesRequest, Ecs, EcsClient, ListContainerInstancesRequest,
};
use rusoto_ssm::{ListCommandInvocationsRequest, SendCommandRequest, Ssm, SsmClient};
use snafu::{OptionExt, ResultExt};
use std::collections::HashMap;

#[derive(Debug, Clone, PartialEq)]
pub struct SSMCommandResponse {
    pub command_id: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Instances {
    pub instance_ids: Vec<String>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct ContainerInstances {
    pub container_instance_arns: Vec<String>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct SSMInvocationResult {
    pub instance_id: String,
    pub output: String,
    pub status: String,
    pub status_details: String,
    pub response_code: i64,
}

/// The main program logic interacts with a mediator trait instead of touching Rusoto directly.
#[async_trait]
pub trait Mediator {
    /// Provides a list of container instances in a cluster
    async fn list_container_instances(
        &self,
        cluster_arn: String,
    ) -> std::result::Result<ContainerInstances, Box<dyn std::error::Error + Send + Sync + 'static>>;
    /// Describes each container instances and extracts their ec2 instance id
    async fn describe_container_instances(
        &self,
        cluster_arn: String,
        container_instance_arns: &[String],
    ) -> std::result::Result<Instances, Box<dyn std::error::Error + Send + Sync + 'static>>;
    /// Runs ssm document on the list of instances provided.
    async fn send_command(
        &self,
        instance_ids: &[String],
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> std::result::Result<SSMCommandResponse, Box<dyn std::error::Error + Send + Sync + 'static>>;
    /// Gets the ssm command result for each instance
    async fn list_command_invocation(
        &self,
        command_id: String,
    ) -> std::result::Result<
        Vec<SSMInvocationResult>,
        Box<dyn std::error::Error + Send + Sync + 'static>,
    >;
}

pub struct AwsMediator {
    ecs_client: EcsClient,
    ssm_client: SsmClient,
}

impl AwsMediator {
    const SSM_COMMAND_DEFAULT_TIMEOUT_SECS: i64 = 60;
    pub fn new_with(ecs_client: EcsClient, ssm_client: SsmClient) -> Self {
        AwsMediator {
            ssm_client,
            ecs_client,
        }
    }
}

#[async_trait]
impl Mediator for AwsMediator {
    async fn list_container_instances(
        &self,
        cluster_arn: String,
    ) -> std::result::Result<ContainerInstances, Box<dyn std::error::Error + Send + Sync + 'static>>
    {
        let resp = self
            .ecs_client
            .list_container_instances(ListContainerInstancesRequest {
                cluster: Some(cluster_arn.clone()),
                ..ListContainerInstancesRequest::default()
            })
            .await
            .context(error::ListContainerInstances {
                cluster_arn: cluster_arn.clone(),
            })?;
        let container_instance_arns =
            resp.container_instance_arns
                .context(error::ECSMissingField {
                    field: "container_instance_arns",
                    api: "list_container_instances",
                })?;
        Ok(ContainerInstances {
            container_instance_arns,
        })
    }

    async fn describe_container_instances(
        &self,
        cluster_arn: String,
        container_instance_arns: &[String],
    ) -> std::result::Result<Instances, Box<dyn std::error::Error + Send + Sync + 'static>> {
        let resp = self
            .ecs_client
            .describe_container_instances(DescribeContainerInstancesRequest {
                cluster: Some(cluster_arn.to_string()),
                container_instances: container_instance_arns.as_ref().into(),
                include: None,
            })
            .await
            .context(error::DescribeInstances {
                cluster_arn: cluster_arn.to_string(),
            })?;
        let mut instance_ids = Vec::new();
        for instances in resp.container_instances.context(error::ECSMissingField {
            field: "container_instances",
            api: "describe_container_instances",
        })? {
            instance_ids.push(
                instances
                    .ec_2_instance_id
                    .context(error::ECSMissingField {
                        api: "describe_container_instances",
                        field: "ec_2_instance_id",
                    })?
                    .clone(),
            );
        }
        Ok(Instances { instance_ids })
    }

    async fn send_command(
        &self,
        instance_ids: &[String],
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> std::result::Result<SSMCommandResponse, Box<dyn std::error::Error + Send + Sync + 'static>>
    {
        let resp = self
            .ssm_client
            .send_command(SendCommandRequest {
                comment: Some("Makes Bottlerocket API call via SSM".into()),
                instance_ids: Some(instance_ids.as_ref().to_vec()),
                document_name: String::from("AWS-RunShellScript"),
                document_version: Some("1".into()),
                parameters: Some(params.clone()),
                timeout_seconds: match timeout {
                    None => Some(Self::SSM_COMMAND_DEFAULT_TIMEOUT_SECS),
                    Some(_) => timeout,
                },
                ..SendCommandRequest::default()
            })
            .await
            .context(error::SendSSMCommand)?;
        let command_id = resp
            .command
            .context(error::SSMMissingField {
                field: "command",
                api: "send_command",
            })?
            .command_id
            .context(error::SSMMissingField {
                field: "command_id",
                api: "send_command",
            });
        Ok(SSMCommandResponse {
            command_id: command_id?,
        })
    }

    async fn list_command_invocation(
        &self,
        command_id: String,
    ) -> std::result::Result<
        Vec<SSMInvocationResult>,
        Box<dyn std::error::Error + Send + Sync + 'static>,
    > {
        let resp = self
            .ssm_client
            .list_command_invocations(ListCommandInvocationsRequest {
                command_id: Some(command_id.clone()),
                ..ListCommandInvocationsRequest::default()
            })
            .await
            .context(error::ListCommandInvocations {
                command_id: command_id.clone(),
            })?;
        let mut invocation_list = Vec::new();
        for invocation in resp.command_invocations.context(error::SSMMissingField {
            field: "command_invocations",
            api: "list_command_invocations",
        })? {
            let instance_id = invocation.instance_id.context(error::SSMMissingField {
                field: "instance_id",
                api: "list_command_invocations",
            })?;
            let plugin = invocation
                .command_plugins
                .context(error::SSMInvocationMissingField {
                    instance_id: instance_id.clone(),
                    field: "command_plugins",
                    api: "list_command_invocations",
                })?;
            let command_result = &plugin[0];
            let output =
                command_result
                    .output
                    .as_ref()
                    .context(error::SSMInvocationMissingField {
                        instance_id: instance_id.clone(),
                        field: "output",
                        api: "list_command_invocations",
                    })?;
            let response_code =
                command_result
                    .response_code
                    .context(error::SSMInvocationMissingField {
                        instance_id: instance_id.clone(),
                        field: "response_code",
                        api: "list_command_invocations",
                    })?;
            let status_details = command_result.status_details.as_ref().context(
                error::SSMInvocationMissingField {
                    instance_id: instance_id.clone(),
                    field: "status_details",
                    api: "list_command_invocations",
                },
            )?;
            let status =
                command_result
                    .status
                    .as_ref()
                    .context(error::SSMInvocationMissingField {
                        instance_id: instance_id.clone(),
                        field: "status",
                        api: "list_command_invocations",
                    })?;
            invocation_list.push(SSMInvocationResult {
                instance_id: instance_id.clone(),
                output: output.to_string(),
                response_code,
                status_details: status_details.to_string(),
                status: status.to_string(),
            });
        }
        Ok(invocation_list)
    }
}

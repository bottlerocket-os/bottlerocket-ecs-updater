use crate::aws::error;
use async_trait::async_trait;
use rusoto_ecs::{
    DescribeContainerInstancesRequest, Ecs, EcsClient, ListContainerInstancesRequest,
};
use rusoto_ssm::{GetCommandInvocationRequest, SendCommandRequest, Ssm, SsmClient};
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
    pub output: String,
    pub status: String,
    pub status_details: String,
    pub response_code: i64,
}

/// The main program logic interacts with a mediator trait instead of touching Rusoto directly.
#[async_trait]
pub trait Mediator {
    // provides a list of container instances in a cluster
    async fn list_container_instances(
        &self,
        cluster_arn: String,
    ) -> std::result::Result<ContainerInstances, Box<dyn std::error::Error + Send + Sync + 'static>>;
    // describes each container instances and extracts their ec2 instance id
    async fn describe_container_instances(
        &self,
        cluster_arn: String,
        container_instance_arns: &[String],
    ) -> std::result::Result<Instances, Box<dyn std::error::Error + Send + Sync + 'static>>;
    // runs ssm document on the list of instances provided.
    async fn send_command(
        &self,
        instance_ids: &[String],
        params: HashMap<String, Vec<String>>,
        timeout: Option<i64>,
    ) -> std::result::Result<SSMCommandResponse, Box<dyn std::error::Error + Send + Sync + 'static>>;
    // gets the ssm command result for each instance
    async fn get_command_invocation(
        &self,
        command_id: String,
        instance_id: String,
    ) -> std::result::Result<SSMInvocationResult, Box<dyn std::error::Error + Send + Sync + 'static>>;
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

    async fn get_command_invocation(
        &self,
        command_id: String,
        instance_id: String,
    ) -> std::result::Result<SSMInvocationResult, Box<dyn std::error::Error + Send + Sync + 'static>>
    {
        let resp = self
            .ssm_client
            .get_command_invocation(GetCommandInvocationRequest {
                command_id: command_id.clone(),
                instance_id: instance_id.clone(),
                plugin_name: None,
            })
            .await
            .context(error::GetCommandInvocation {
                command_id: command_id.clone(),
                instance_id: instance_id.clone(),
            })?;
        let output = resp
            .standard_output_content
            .context(error::SSMMissingField {
                field: "standard_output_content",
                api: "get_command_invocation",
            })?;
        let response_code = resp.response_code.context(error::SSMMissingField {
            field: "response_code",
            api: "get_command_invocation",
        })?;
        let status_details = resp.status_details.context(error::SSMMissingField {
            field: "status_details",
            api: "get_command_invocation",
        })?;
        let status = resp.status.context(error::SSMMissingField {
            field: "status",
            api: "get_command_invocation",
        })?;
        Ok(SSMInvocationResult {
            output,
            response_code,
            status_details,
            status,
        })
    }
}

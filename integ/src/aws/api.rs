use crate::aws::error::{self, Result};
use async_trait::async_trait;
use rusoto_cloudformation::{
    CloudFormation, CloudFormationClient, CreateStackInput, DeleteStackInput,
    DescribeStackResourcesInput, DescribeStacksInput, Parameter,
};
use rusoto_ec2::{
    Ec2, Ec2Client, IamInstanceProfileSpecification, RunInstancesRequest, TagSpecification,
};
use rusoto_ecs::{CapacityProviderStrategyItem, CreateClusterRequest, Ecs, EcsClient};
use snafu::{OptionExt, ResultExt};

#[derive(Debug, Clone, PartialEq)]
pub struct Instances {
    pub instance_ids: Vec<String>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct CreateStackResult {}

#[derive(Debug, Clone, PartialEq)]
pub struct Resources {
    pub subnet1_id: String,
    pub subnet2_id: String,
    pub security_group_id: String,
    pub ecs_instance_profile_id: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct StackDetails {
    pub status: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct ClusterDetails {
    pub cluster_arn: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct InstanceDetails {
    pub instance_id: String,
}

/// The main program logic interacts with a mediator trait instead of touching Rusoto directly.
#[async_trait]
pub trait Mediator {
    async fn create_stack(
        &self,
        template_body: String,
        stack_name: String,
        parameters: Option<Vec<Parameter>>,
    ) -> Result<()>;
    async fn describe_stacks(&self, stack_name: String) -> Result<()>;

    async fn describe_stack_resources(&self, stack_name: String) -> Result<Resources>;
    /// Gets the ssm command result for each instance
    async fn delete_stack(&self, stack_name: String) -> Result<()>;
    /// Provides a list of container instances in a cluster
    async fn create_cluster(&self, cluster_name: String) -> Result<ClusterDetails>;
    /// Describes each container instances and extracts their ec2 instance id
    async fn run_instances(
        &self,
        ami_id: String,
        cluster_name: String,
        subnet_id: String,
        security_group_id: String,
        instance_role_id: String,
    ) -> Result<Vec<InstanceDetails>>;
}

pub struct AwsMediator {
    ecs_client: EcsClient,
    ec2_client: Ec2Client,
    cfn_client: CloudFormationClient,
}

impl AwsMediator {
    const SSM_COMMAND_DEFAULT_TIMEOUT_SECS: i64 = 60;
    pub fn new_with(
        ecs_client: EcsClient,
        ec2_client: Ec2Client,
        cfn_client: CloudFormationClient,
    ) -> Self {
        AwsMediator {
            ecs_client,
            ec2_client,
            cfn_client,
        }
    }
}

#[async_trait]
impl Mediator for AwsMediator {
    async fn create_stack(
        &self,
        template_body: String,
        stack_name: String,
        parameters: Option<Vec<Parameter>>,
    ) -> Result<()> {
        self.cfn_client
            .create_stack(CreateStackInput {
                capabilities: Some(vec![String::from("CAPABILITY_NAMED_IAM")]),
                stack_name,
                template_body: Some(template_body),
                parameters,
                ..CreateStackInput::default()
            })
            .await
            .context(error::CreateStack)?;
        Ok(())
    }

    async fn describe_stacks(&self, stack_name: String) -> Result<()> {
        self.cfn_client
            .describe_stacks(DescribeStacksInput {
                stack_name: Some(stack_name),
                ..DescribeStacksInput::default()
            })
            .await
            .context(error::DescribeStacks)?;
        Ok(())
    }

    async fn describe_stack_resources(&self, stack_name: String) -> Result<Resources> {
        let resp = self
            .cfn_client
            .describe_stack_resources(DescribeStackResourcesInput {
                stack_name: Some(stack_name),
                ..DescribeStackResourcesInput::default()
            })
            .await
            .context(error::DescribeStackResources)?;
        let mut subnet1_id = String::new();
        let mut subnet2_id = String::new();
        let mut security_group_id = String::new();
        let mut ecs_instance_profile_id = String::new();
        for resource in resp.stack_resources.context(error::CfnMissingField {
            field: "stack_resources",
            api: "describe_stack_resources",
        })? {
            match resource.logical_resource_id.as_str() {
                "Subnet1" => {
                    subnet1_id =
                        resource
                            .physical_resource_id
                            .context(error::MissingPhysicalResourceID {
                                resource_name: resource.logical_resource_id,
                            })?
                }
                "Subnet2" => {
                    subnet2_id =
                        resource
                            .physical_resource_id
                            .context(error::MissingPhysicalResourceID {
                                resource_name: resource.logical_resource_id,
                            })?
                }
                "SecurityGroup" => {
                    security_group_id =
                        resource
                            .physical_resource_id
                            .context(error::MissingPhysicalResourceID {
                                resource_name: resource.logical_resource_id,
                            })?
                }
                "EcsInstanceProfile" => {
                    ecs_instance_profile_id =
                        resource
                            .physical_resource_id
                            .context(error::MissingPhysicalResourceID {
                                resource_name: resource.logical_resource_id,
                            })?
                }
                _ => println!(
                    "Resource {} information not required",
                    resource.logical_resource_id
                ),
            }
        }
        Ok(Resources {
            subnet1_id,
            subnet2_id,
            security_group_id,
            ecs_instance_profile_id,
        })
    }

    async fn delete_stack(&self, stack_name: String) -> Result<()> {
        self.cfn_client
            .delete_stack(DeleteStackInput {
                stack_name,
                ..DeleteStackInput::default()
            })
            .await
            .context(error::DeleteStack)?;
        Ok(())
    }

    async fn create_cluster(&self, cluster_name: String) -> Result<ClusterDetails> {
        let cluster_arn = self
            .ecs_client
            .create_cluster(CreateClusterRequest {
                capacity_providers: Some(vec!["FARGATE".to_string()]),
                cluster_name: Some(cluster_name),
                default_capacity_provider_strategy: Some(vec![CapacityProviderStrategyItem {
                    capacity_provider: String::from("FARGATE"),
                    weight: Some(1),
                    base: None,
                }]),
                settings: None,
                tags: Some(vec![rusoto_ecs::Tag {
                    key: Some(String::from("category")),
                    value: Some("ecs-updater-integ".to_string()),
                }]),
            })
            .await
            .context(error::CreateCluster)?
            .cluster
            .context(error::ECSMissingField {
                field: "cluster",
                api: "create_cluster",
            })?
            .cluster_arn
            .context(error::ECSMissingField {
                field: "cluster.cluster_arn",
                api: "create_cluster",
            })?;
        Ok(ClusterDetails { cluster_arn })
    }

    async fn run_instances(
        &self,
        ami_id: String,
        cluster_name: String,
        subnet_id: String,
        security_group_id: String,
        instance_role_id: String,
    ) -> Result<Vec<InstanceDetails>> {
        let userdata = format!(
            r#"[settings.ecs]
cluster = "{0}"
"#,
            cluster_name
        );
        let instances = self
            .ec2_client
            .run_instances(RunInstancesRequest {
                subnet_id: Some(subnet_id.to_owned()),
                image_id: Some(ami_id),
                max_count: 1,
                min_count: 1,
                instance_type: Some("c3.large".into()),
                security_group_ids: Some(vec![security_group_id.to_owned()]),
                tag_specifications: Some(vec![TagSpecification {
                    resource_type: Some(String::from("instance")),
                    tags: Some(vec![rusoto_ec2::Tag {
                        key: Some(String::from("cluster")),
                        value: Some(cluster_name.to_owned()),
                    }]),
                }]),
                iam_instance_profile: Some(IamInstanceProfileSpecification {
                    name: Some(instance_role_id),
                    arn: None,
                }),
                user_data: Some(base64::encode(userdata)),
                ..RunInstancesRequest::default()
            })
            .await
            .context(error::RunInstance)?
            .instances
            .context(error::Ec2MissingField {
                field: "instances",
                api: "run_instances",
            })?;
        let mut instances_details = Vec::new();
        for instance in instances {
            instances_details.push(InstanceDetails {
                instance_id: instance.instance_id.context(error::Ec2MissingField {
                    field: "instances[].instance_id",
                    api: "run_instances",
                })?,
            });
        }
        return Ok(instances_details);
    }
}

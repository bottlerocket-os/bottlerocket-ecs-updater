# Bottlerocket ECS Updater

The Bottlerocket ECS Updater is a service you can install into your ECS cluster that helps you keep your Bottlerocket container instances up to date.
When installed, the Bottlerocket ECS Updater will periodically query each Bottlerocket container instance to find whether an update is available and drain tasks while an update is in progress.
Updates to Bottlerocket are rolled out in [waves](https://github.com/bottlerocket-os/bottlerocket/tree/develop/sources/updater/waves) to reduce the impact of issues; the container instances in your cluster may not all see updates at the same time.

## Installation

You can install the Bottlerocket ECS Updater into your cluster with the provided [CloudFormation template](stacks/bottlerocket-ecs-updater.yaml).
The following information is required when creating the CloudFormation stack:

* The name of the ECS cluster where you are running Bottlerocket container instances
* The name of the CloudWatch Logs log group where the Bottlerocket ECS Updater will send its logs
* At least one subnet ID that has Internet access (which does not need to be shared with the rest of your cluster)

When installed, the CloudFormation template will create the following resources in your account:

* A task definition for the Bottlerocket ECS Updater
* A CloudWatch Events scheduled rule to execute the Bottlerocket ECS Updater
* An IAM role for the Bottlerocket ECS Updater task itself as well as roles for Fargate and CloudWatch Events
* SSM documents to query and execute updates on Bottlerocket instances

## Getting Started

To install the Bottlerocket ECS Updater, you will need to fetch some information first.

### Subnet info

You should either have a default virtual private cloud (VPC) or have already
[created a VPC](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/get-set-up-for-amazon-ecs.html#create-a-vpc)
in your account.

To find your default VPC, run this command.
(If you use an AWS region other than "us-west-2", make sure to change that.)

```sh
aws ec2 describe-vpcs \
   --region us-west-2 \
   --filters=Name=isDefault,Values=true \
   | jq --raw-output '.Vpcs[].VpcId'
```

If you want to use a different VPC you created, run this to get the ID for your VPC.
Make sure to change VPC_NAME to the name of the VPC you created.
(If you use an EC2 region other than "us-west-2", make sure to change that too.)

```sh
aws ec2 describe-vpcs \
   --region us-west-2 \
   --filters=Name=tag:Name,Values=VPC_NAME \
   | jq --raw-output '.Vpcs[].VpcId'
```

Next, run this to get information about the subnets in your VPC.
It will give you a list of the subnets and tell you whether each is public or private.
Make sure to change VPC_ID to the value you received from the previous command.
(If you use an EC2 region other than "us-west-2", make sure to change that too.)

```sh
aws ec2 describe-subnets \
   --region us-west-2 \
   --filter=Name=vpc-id,Values=VPC_ID \
   | jq '.Subnets[] | {id: .SubnetId, public: .MapPublicIpOnLaunch, az: .AvailabilityZone}'
```

You'll want to pick at least one and save it for the launch command later.
Make sure the subnets you select have Internet access so the updater can reach its dependencies.
Public subnets usually have Internet access via an [Internet gateway](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Internet_Gateway.html) while private subnets may be configured with NAT.
For more information, see [the VPC user guide](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Internet_Gateway.html#vpc-igw-internet-access).

We recommend picking several subnets in different availability zones.
However, if you want to launch in a specific availability zone, make sure you pick a subnet that matches; the AZ is listed right below the public/private status.

### Log Group

You can either choose an existing log group or create a new one to get your ECS updater logs.

You can run this to get the list of existing log-groups
```sh
aws logs describe-log-groups
```

You'll want to pick one and save it for the installation command later.

If you want to create a new log group, run this (Make sure to provide LOG_GROUP_NAME)
```sh
aws logs create-log-group --log-group-name LOG_GROUP_NAME
```

### Install

Now we can install the [CloudFormation template](stacks/bottlerocket-ecs-updater.yaml) to start the ECS updater for your cluster!

There are a few values to make sure you change in this command:
* CLUSTER_NAME: the name of the cluster you want ECS updater to manage Bottlerocket instances in
* SUBNET_IDS: a comma-separated list of the subnets you selected earlier
* LOG_GROUP_NAME: the log group name you selected or created earlier

```sh
aws cloudformation deploy \
    --stack-name "bottlerocket-ecs-updater" \
    --template-file "./stacks/bottlerocket-ecs-updater.yaml" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides \
    ClusterName="CLUSTER_NAME" \
    Subnets="SUBNET_IDS" \
    LogGroupName="LOG_GROUP_NAME"
```

## How it works

The Bottlerocket ECS Updater is designed to run as a scheduled Fargate task that queries, drains, and performs updates in your ECS cluster.
A rule in CloudWatch Events periodically launches the updater as a new Fargate task.
The updater queries the ECS API to discover all the container instances in your cluster and filters for Bottlerocket instances by reading the `bottlerocket.variant` attribute.
For each Bottlerocket instance found, the updater executes an SSM document that queries for available updates using the `apiclient update check` command.
When an update is available, the updater checks to see whether the tasks currently running on the container instance are part of a [service](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs_services.html) and eligible for replacement.
If all the tasks are part of a service, the updater marks the container instance for [draining](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/container-instance-draining.html) and waits for the tasks to be successfully drained.
After the container instance has been drained, the updater executes an SSM document to download the update, apply the update, and reboot.
Finally, the updater will mark the container instance as active and move on to the next one.

## Troubleshooting

When installed with the provided CloudFormation template, the logs for the updater will be available the CloudWatch Logs group you configured.
Checking the logs is a good first step in understanding why something happened or didn't happen.

### Why do only some of my Bottlerocket instances have an update available?

Updates to Bottlerocket are rolled out in [waves](https://github.com/bottlerocket-os/bottlerocket/tree/develop/sources/updater/waves) to reduce the impact of issues; the container instances in your cluster may not all see updates at the same time.
You can check whether an update is available on your instance by running the `apiclient update check` command from within the [control](https://github.com/bottlerocket-os/bottlerocket#control-container) or [admin](https://github.com/bottlerocket-os/bottlerocket#admin-container) container.

### My Bottlerocket instance has an update available.  Why didn't the Bottlerocket ECS Updater update it?

The Bottlerocket ECS Updater attempts to update container instances without disrupting the workloads in your cluster.
Applying an update to Bottlerocket requires a reboot.
To avoid disruption in your cluster, the Bottlerocket ECS Updater uses the [container instance draining](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/container-instance-draining.html) feature of ECS.
A container instance may be skipped for update when:

* _A non-service task is running._
  Non-service tasks are not automatically replaced when they are stopped.
  To avoid disrupting a critical workload, the Bottlerocket ECS Updater will not stop a non-service task.
* _No spare capacity is available in the cluster._
  The service scheduler attempts to replace the tasks according to the service's deployment configuration parameters, `minimumHealthyPercent` and `maximumPercent`.
  If stopping a task would reduce the running count below your service's `minimumHealthyPercent`, ECS will not stop the task.
  The Bottlerocket ECS Updater will wait for draining to complete for a fixed period of time (currently 25 minutes).
  If draining has not completed by the end of the period, the updater will restore the instance and move to the next one.
* _Draining takes too long._
  The Bottlerocket ECS Updater will wait for draining to complete for a fixed period of time (currently 25 minutes).
  If draining has not completed by the end of the period, the updater will restore the instance and move to the next one.
  The time it takes for a task to be stopped is related to the `stopTimeout` task definition parameter and to any associated resources like load balancers.
  If your tasks are taking too long to drain, you can ensure that your task responds to `SIGTERM`, shorten the `stopTimeout`, or shorten the load balancer's health check and deregistration delay settings.
* _Bottlerocket version is too old._
  The Bottlerocket ECS Updater uses newer [`apiclient update` commands](https://github.com/bottlerocket-os/bottlerocket#update-api) that were added in version [1.0.5](https://github.com/bottlerocket-os/bottlerocket/blob/develop/CHANGELOG.md#v105-2021-01-15).
  The SSM commands will fail if your Bottlerocket OS version is less than 1.0.5.
  Instances running Bottlerocket versions less than 1.0.5 need to be manually updated.
* _Too many instances are in the cluster._
  The Bottlerocket ECS Updater currently supports clusters of up to 50 container instances.
  If the updater is configured to target a cluster with more than 50 instances, some instances may not be updated.

### Why do new container instances launch with older Bottlerocket versions?

The Bottlerocket ECS Updater performs in-place updates for instances in your ECS cluster.
The updater does not influence how those instances are launched.
If you use an auto-scaling group to launch your instances, you can update the AMI ID in your launch configuration or launch template to use a newer version of Bottlerocket.

Note: We do not recommend using the Bottlerocket ECS Updater in conjunction with EC2 Spot.
The ECS Updater is designed to keep services safe from interruption by updating one instance at a time.
With the short average lifetime of Spot instances, the updater may not update them until relatively late in their life, meaning they may not be up to date when serving your application.

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This project is dual licensed under either the Apache-2.0 License or the MIT license, your choice.


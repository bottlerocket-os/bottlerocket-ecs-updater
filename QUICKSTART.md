# TEMPORARY FOR DEVELOPMENT

## ECS Updater Image

The `Dockerfile` inside `updater` directory produces a container image for use in an ECS Task to
update cluster nodes.

### Pre-requisite

#### Create ECR repository

```
aws ecr create-repository --repo bottlerocket-ecs-updater
```

#### Build container image
Declare an AWS Account Id and region in which to upload image

```shell script
ECR_ACCOUNT_ID=123456789
ECR_ACCOUNT_REGION="us-west-2""
```

Declare a variable to tag image

```shell script
UPDATER_IMAGE=${ECR_ACCOUNT_ID}.dkr.ecr.${ECR_ACCOUNT_REGION}.amazonaws.com/bottlerocket-ecs-updater:latest
```

Create Docker image and tag it

```shell script
make image UPDATER_IMAGE=${ECR_ACCOUNT_ID}.dkr.ecr.${ECR_ACCOUNT_REGION}.amazonaws.com/bottlerocket-ecs-updater:latest
```

Push docker image to ECR, 

```shell script
# ECR login
aws ecr get-login-password --region ${ECR_ACCOUNT_REGION} | docker login --username AWS --password-stdin ${ECR_ACCOUNT_ID}.dkr.ecr.${ECR_ACCOUNT_REGION}.amazonaws.com

# upload image
docker push ${UPDATER_IMAGE}
```

## Deploying the Updater

### Get your Cluster ARN
If you already have an ECS CLUSTER then get CLuster ARN,

```
# ARN of ECS Cluster that needs auto updates
ECS_CLUSTER_ARN=aws:ecs:us-west-2:123456789:cluster/bottlerocket-ecs
```

Or else, Follow [QuickStart-ECS](https://github.com/bottlerocket-os/bottlerocket/blob/develop/QUICKSTART-ECS.md) to start a ECS cluster with few nodes that you would like to update

### Deploy Cloudformation Stack

Deploy Cloudformation stack to start a cron job that runs the Updater image in a cluster where ECS nodes to be updated are running.


```shell script
# ECS Cluster VPC Subnets
SUBNET1=subnet-a3dfr
SUBNET2=subnet-2345667qs 

# ECS Cluster AWS Account ID
ECS_ACCOUNT_ID=123456789

aws cloudformation deploy \
    --stack-name  "ecs-updater" \
    --template-file "./stacks/bottlerocket-ecs-updater.yaml" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides \
          EcsClusterArn=arn:${ECS_CLUSTER_ARN} \
          EcsClusterVPCSUBNET1=${SUBNET1} \
          EcsClusterVPCSUBNET2=${SUBNET2} \
          UpdaterImage=${ECS_ACCOUNT_ID}.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:latest
```

### Integ Test


The command below will create an ECS cluster with one node in a separate VPC and deploy the [updater stack](stacks/bottlerocket-ecs-updater.yaml) to start updates.
 
Note: Be aware that every time you run the integ test a new cluster and updater are created.  
Cleanup code has not been implemented yet so you will need to manually delete the cluster and terminate instances.  

```shell script
# change directory to integration tests
cd integ

# start integration tests
cargo run --package integ --bin integ -- \
--ami-id "ami-0c34abecc4221bdc8" \
--region "us-west-2" \
--updater-image "123456789.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:latest"
```



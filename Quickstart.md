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
Declare an AWS Account Id in which to upload image

```
AWSAccountId=062205370538
```

Declare a variable to tag image

```
UPDATER_IMAGE=${AWSAccountId}.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:latest
```

Create Docker image and tag it

```
make image UPDATER_IMAGE=${AWSAccountId}.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:latest
```

Push docker image to ECR, 
Note: you may need ECR login credentials for this

```
docker push ${UPDATER_IMAGE}
```

## Manual Setup

### Create cluster

Follow [QuickStart-ECS](https://github.com/bottlerocket-os/bottlerocket/blob/develop/QUICKSTART-ECS.md) to start a ECS cluster with few nodes that you would like to update

### Deploy Cloudformation Stack

Deploy Cloudformation stack to start a cron job that runs the Updater image in a cluster where ECS nodes to be updated are running.

```
# ARN of ECS Cluster that needs auto updates
EcsClusterArn=aws:ecs:us-west-2:062205370538:cluster/bottlerocket-ecs

# ECS Cluster VPC Subnets
Subnet1=subnet-a3dfr
Subnet2=subnet-2345667qs 

# ECS Cluster AWS Account ID
AWSAccountId=123456789

aws cloudformation deploy \
    --stack-name  "ecs-updater" \
    --template-file "./stacks/bottlerocket-ecs-updater.yaml" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides \
          EcsClusterArn=arn:${EcsClusterArn} \
          EcsClusterVPCSubnet1=${Subnet1} \
          EcsClusterVPCSubnet2=${Subnet2} \
          UpdaterImage=${AWSAccountId}.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:latest
```

### Integ Test


Below command will create an ECS cluster with one node in a separate VPC and create [bottlerocket-ecs-updater.yaml](stacks/bottlerocket-ecs-updater.yaml) stack to start updates  
Note: Be aware, every time you run integ test a new cluster is created and updater task is ran. Since, cleanup code has not been added you would have to manually delete cluster and terminate nodes.  
```shell
# change directory to integration tests
cd integ

# start integration tests
cargo run --package integ --bin integ -- \
--ami-id "ami-0c34abecc4221bdc8" \
--region "us-west-2" \
--updater-image "123456789.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:latest"
```



#!/usr/bin/env bash

THISDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${THISDIR}/common.sh"

# Default ECS cluster name
DEFAULT_CLUSTER_NAME="ecs-updater-integ-cluster"

# Default number of instances to launch in the cluster
DEFAULT_INSTANCE_COUNT=10

# Default instance type for instances in the cluster
DEFAULT_INSTANCE_TYPE="m5.xlarge"

# Helper functions
usage() {
    cat >&2 <<EOF
${0##*/}
                 --ami-id AMI-ID
                 [--instance-type ${DEFAULT_INSTANCE_TYPE}]
                 [--instance-count ${DEFAULT_INSTANCE_COUNT}]
                 [--cluster ${DEFAULT_CLUSTER_NAME}]

Deploys templates '${INTEG_STACK_TEMPLATE}' and '${CLUSTER_STACK_TEMPLATE}' to set up an ECS cluster.

Required:
   --ami-id                           Image ID for test instance in cluster (an aws-ecs-1 AMI ID)

Optional:
   --instance-type                    Instance type for test instances (default ${DEFAULT_INSTANCE_TYPE})
   --instance-count                   Number of instances to launch in the cluster (default ${DEFAULT_INSTANCE_COUNT})
   --cluster                          Name of the cluster (default ${DEFAULT_CLUSTER_NAME}). New cluster is created if it does not exist.

EOF
}

parse_args() {
    while [ ${#} -gt 0 ]; do
        case "${1}" in
        --ami-id)
            shift
            AMI_ID="${1}"
            ;;
        --instance-type)
            shift
            INSTANCE_TYPE="${1}"
            ;;
        --instance-count)
            shift
            INSTANCE_COUNT="${1}"
            ;;
        --cluster)
            shift
            CLUSTER_STACK_NAME="${1}"
            ;;

        --help)
            usage
            exit 0
            ;;
        *)
            log ERROR "Unknown argument: ${1}" >&2
            usage
            exit 2
            ;;
        esac
        shift
    done

    INSTANCE_TYPE="${INSTANCE_TYPE:-$DEFAULT_INSTANCE_TYPE}"
    INSTANCE_COUNT="${INSTANCE_COUNT:-$DEFAULT_INSTANCE_COUNT}"
    CLUSTER_STACK_NAME="${CLUSTER_STACK_NAME:-$DEFAULT_CLUSTER_NAME}"

    # Required arguments
    required_arg "--ami-id" "${AMI_ID}"
}

# Initial setup and checks
parse_args "${@}"

# deploy stack to create integ resources
log INFO "Deploying stack template '${INTEG_STACK_TEMPLATE}'"
if ! aws cloudformation deploy \
    --stack-name "${INTEG_STACK_NAME}" \
    --template-file "${THISDIR}/stacks/${INTEG_STACK_TEMPLATE}" \
    --capabilities CAPABILITY_NAMED_IAM; then
    log ERROR "Failed to deploy '${INTEG_STACK_TEMPLATE}' stack template"
    exit 1
fi
log INFO "Stack template '${INTEG_STACK_TEMPLATE}' deployed with name '${INTEG_STACK_NAME}'"

# deploy stack to start ecs cluster using auto-scaling group
log INFO "Deploying stack template '${CLUSTER_STACK_TEMPLATE}' to set up an ECS cluster"
if ! aws cloudformation deploy \
    --stack-name "${CLUSTER_STACK_NAME}" \
    --template-file "${THISDIR}/stacks/${CLUSTER_STACK_TEMPLATE}" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides \
    IntegSharedResourceStack="${INTEG_STACK_NAME}" \
    InstanceCount="${INSTANCE_COUNT}" \
    ImageID="${AMI_ID}"; then
    log ERROR "Failed to deploy stack '${CLUSTER_STACK_TEMPLATE}' stack template"
    exit 1
fi
log INFO "ECS cluster '${CLUSTER_STACK_NAME}'  with '${INSTANCE_COUNT}' instances and instance type '${INSTANCE_TYPE}' created!"

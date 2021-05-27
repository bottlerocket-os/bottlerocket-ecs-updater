#!/usr/bin/env bash

THISDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${THISDIR}/common.sh"

# Helper functions
usage() {
    cat >&2 <<EOF
${0##*/}
                 --cluster CLUSTER --updater-image UPDATER-IMAGE

Starts an ECS updater to manage Bottlerocket instances in a given cluster

Required:
   --cluster                          Cluster name to manage Bottlerocket instances in
   --updater-image                    Bottlerocket ECS updater image ECR location

EOF
}

parse_args() {
    while [ ${#} -gt 0 ]; do
        case "${1}" in
        --cluster)
            shift
            CLUSTER="${1}"
            ;;
        --updater-image)
            shift
            UPDATER_IMAGE="${1}"
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

    UPDATER_STACK_NAME="${UPDATER_STACK_PREFIX}${CLUSTER}"

    # Required arguments
    required_arg "--cluster" "${CLUSTER}"
    required_arg "--updater-image" "${UPDATER_IMAGE}"
}

# Initial setup and checks
parse_args "${@}"

log INFO "Extracting output resource id's from '${INTEG_STACK_NAME}' stack"
if ! integ_resources=$(aws cloudformation describe-stacks \
    --stack-name "${INTEG_STACK_NAME}" \
    --output json \
    --query 'Stacks[].Outputs[]'); then
    log ERROR "Failed to get outputs from '${INTEG_STACK_NAME}' stack"
    exit 1
fi

# Get Subnets
if ! subnets=$(echo "${integ_resources}" | jq --raw-output '.[] | select(.OutputKey == "PublicSubnets") | .OutputValue'); then
    log ERROR "Failed to extract list of subnets from '${INTEG_STACK_NAME}' stack outputs"
    exit 1
fi
log INFO "Subnets are '${subnets}'"
# check the data to make sure its usable in our context
if [[ "${#subnets[@]}" -lt 1 ]]; then
    log ERROR "No usable subnets"
    exit 1
fi

# Get LogGroupName
if ! log_group=$(echo "${integ_resources}" | jq --raw-output '.[] | select(.OutputKey == "LogGroupName") | .OutputValue'); then
    log ERROR "Failed to extract LogGroup name from '${INTEG_STACK_NAME}' stack outputs"
    exit 1
fi
log INFO "LogGroup name is '${log_group}'"

# Get LogGroupName
if ! security_grp=$(echo "${integ_resources}" | jq --raw-output '.[] | select(.OutputKey == "SecurityGroupID") | .OutputValue'); then
    log ERROR "Failed to extract security group id from '${INTEG_STACK_NAME}' stack outputs"
    exit 1
fi
log INFO "Security group id is '${security_grp}'"

# start updater on cluster
log INFO "Deploying ECS updater stack on cluster '${CLUSTER}' with cron event rule disabled"
if ! aws cloudformation deploy \
    --stack-name "${UPDATER_STACK_NAME}" \
    --template-file "${THISDIR}/../stacks/bottlerocket-ecs-updater.yaml" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides \
    ClusterName="${CLUSTER}" \
    Subnets="${subnets}" \
    UpdaterImage="${UPDATER_IMAGE}" \
    LogGroupName="${log_group}" \
    ScheduleState="DISABLED"; then
    log ERROR "Failed to deploy Bottlerocket ECS updater"
    exit 1
fi

log INFO "Extracting updater task definition arn from '${UPDATER_STACK_NAME}' stack"
if ! output=$(aws cloudformation describe-stacks \
    --stack-name "${UPDATER_STACK_NAME}" \
    --output json \
    --query 'Stacks[].Outputs[]'); then
    log ERROR "Failed to get outputs from '${UPDATER_STACK_NAME}' stack"
    exit 1
fi

if ! task_def=$(echo "${output}" | jq --raw-output '.[] | select(.OutputKey == "UpdaterTaskDefinitionArn") | .OutputValue'); then
    log ERROR "Failed to extract updater task definition arn from '${UPDATER_STACK_NAME}' stack outputs"
    exit 1
fi

log INFO "Starting ECS updater task on cluster '${CLUSTER}'"
if ! aws ecs run-task \
    --cluster "${CLUSTER}" \
    --task-definition "${task_def}" \
    --launch-type "FARGATE" \
    --network-configuration="awsvpcConfiguration={subnets=[${subnets}],securityGroups=${security_grp},assignPublicIp=ENABLED}"; then
    log ERROR "Failed to start updater task '${task_def}'"
    exit 1
fi

log INFO "ECS updater is running on cluster '${CLUSTER}'. Check logs in Cloudwatch LogGroup '${log_group}'"

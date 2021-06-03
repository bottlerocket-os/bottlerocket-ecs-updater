#!/usr/bin/env bash

# Cloudformation stack template file name to set up VPC, security group, IAM roles, and log group
INTEG_STACK_TEMPLATE="integ-shared.yaml"

# Cloudformation stack template file name to set up an ECS cluster
CLUSTER_STACK_TEMPLATE="cluster.yaml"

# The stack name for deploying `integ-shared.yaml` template
INTEG_STACK_NAME="ecs-updater-integ-shared"

# Prefix for ECS Updater stack name, resulting stack name will be below prefix + cluster name
UPDATER_STACK_PREFIX="UPDATER-"

log() {
    local lvl="$1"
    shift
    local msg="$*"
    echo "${lvl}: ${msg}" >&2
}

required_arg() {
    local arg="${1:?}"
    local value="${2}"
    if [ -z "${value}" ]; then
        echo "ERROR: ${arg} is required" >&2
        exit 2
    fi
}

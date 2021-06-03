#!/usr/bin/env bash

THISDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${THISDIR}/common.sh"

delete_integ=0

# Helper functions
usage() {
    cat >&2 <<EOF
${0##*/}
                 --cluster CLUSTER-NAME
                 [--delete-integ-stack]

Cleans up resources started for integration testing

Required:
   --cluster                          Name of the cluster to delete

Optional:
   --delete-integ-stack               deletes Integ resources stack '${INTEG_STACK_NAME}' along with the cluster

EOF
}

parse_args() {
    while [ ${#} -gt 0 ]; do
        case "${1}" in
        --cluster)
            shift
            CLUSTER="${1}"
            ;;
        --delete-integ-stack)
            delete_integ=1
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

    # Required arguments
    required_arg "--cluster" "${CLUSTER}"
}

delete_stack() {
    local stack_name="${1:?}"
    log INFO "Deleting Cloudformation stack '${stack_name}'"
    if ! aws cloudformation delete-stack \
        --stack-name "${stack_name}"; then
        log ERROR "Failed to delete '${stack_name}'"
    fi

    log INFO "Waiting for Cloudformation stack '${stack_name}' to be deleted"
    if ! aws cloudformation wait stack-delete-complete \
        --stack-name "${stack_name}"; then
        log ERROR "Failed to wait for ${stack_name} to delete"
        aws cloudformation describe-stack-events \
            --stack-name "${stack_name}"
    fi
    log INFO "Cloudformation stack '${stack_name}' deleted!"
}

terminate_instances() {
    local cluster="${1:?}"
    log INFO "Extracting auto-scaling group name from '${cluster}' stack"
    if ! output=$(aws cloudformation describe-stacks \
        --stack-name "${cluster}" \
        --output json \
        --query 'Stacks[].Outputs[]'); then
        log ERROR "Failed to get outputs from '${cluster}' stack"
        return
    fi

    if ! auto_scaling_group=$(echo "${output}" | jq --raw-output '.[] | select(.OutputKey == "AutoScalingGroupName") | .OutputValue'); then
        log ERROR "Failed to extract auto scaling group name from '${cluster}' stack outputs"
        return
    fi

    log INFO "Describing auto-scaling group '${auto_scaling_group}' to get instance ids"
    if ! instance_ids=$(aws autoscaling describe-auto-scaling-groups \
        --auto-scaling-group-name "${auto_scaling_group}" \
        --query "AutoScalingGroups[].Instances[].InstanceId" \
        --output text); then
        log ERROR "Failed to get instance ids from auto scaling group '${auto_scaling_group}'"
        return
    fi
    log INFO "Instances '${instance_ids}' found"

    log INFO "Setting auto scaling group desired count to zero"
    if ! aws autoscaling update-auto-scaling-group \
        --auto-scaling-group-name "${auto_scaling_group}" \
        --desired-capacity 0 \
        --min-size 0; then
        log ERROR "Failed to change auto scaling group '${auto_scaling_group}' desired count to 0"
        return
    fi

    for inst_id in ${instance_ids}; do
        log INFO "Waiting for instance '${inst_id}' to terminate"
        if ! aws ec2 wait instance-terminated \
            --instance-ids "${inst_id}"; then
            log ERROR "Failed to terminate instance '${inst_id}'"
        fi
    done
}

# Initial setup and checks
parse_args "${@}"

delete_stack "${UPDATER_STACK_PREFIX}${CLUSTER}"

terminate_instances "${CLUSTER}"

delete_stack "${CLUSTER}"

if [[ "${delete_integ}" -eq 1 ]]; then
    delete_stack "${INTEG_STACK_NAME}"
fi

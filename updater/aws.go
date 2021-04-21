package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

const (
	pageSize = 50
)

func (u *updater) listContainerInstances() ([]*string, error) {
	resp, err := u.ecs.ListContainerInstances(&ecs.ListContainerInstancesInput{
		Cluster:    &u.cluster,
		MaxResults: aws.Int64(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list container instances: %#v", err)
	}
	log.Printf("%#v", resp)
	return resp.ContainerInstanceArns, nil
}

// filterBottlerocketInstances returns a map of EC2 instance IDs to container instance ARNs
// provided as input where the container instance is a Bottlerocket host.
func (u *updater) filterBottlerocketInstances(instances []*string) (map[string]string, error) {
	resp, err := u.ecs.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster:            &u.cluster,
		ContainerInstances: instances,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot describe container instances: %#v", err)
	}

	ec2IDtoECSARN := make(map[string]string)
	// Check the DescribeInstances response for Bottlerocket nodes, add them to map if detected.
	for _, instance := range resp.ContainerInstances {
		if containsAttribute(instance.Attributes, "bottlerocket.variant") {
			ec2IDtoECSARN[aws.StringValue(instance.Ec2InstanceId)] = aws.StringValue(instance.ContainerInstanceArn)
			log.Printf("Bottlerocket instance detected. Instance %s added to check updates", aws.StringValue(instance.Ec2InstanceId))
		}
	}
	return ec2IDtoECSARN, nil
}

// containsAttribute checks if a slice of ECS Attributes struct contains a specified name.
func containsAttribute(attrs []*ecs.Attribute, searchString string) bool {
	for _, attr := range attrs {
		if aws.StringValue(attr.Name) == searchString {
			return true
		}
	}
	return false
}

// drain drains eligible container instances. Container instances are eligible if all running
// tasks were started by a service, or if there are no running tasks.
func (u *updater) drain(containerInstance string) error {
	if !u.eligible(&containerInstance) {
		return errors.New("ineligible for updates")
	}
	return u.drainInstance(aws.String(containerInstance))
}

func (u *updater) eligible(containerInstance *string) bool {
	list, err := u.ecs.ListTasks(&ecs.ListTasksInput{
		Cluster:           &u.cluster,
		ContainerInstance: containerInstance,
	})
	if err != nil {
		log.Printf("failed to list tasks for container instance %s: %#v",
			aws.StringValue(containerInstance), err)
		return false
	}

	taskARNs := list.TaskArns
	if len(list.TaskArns) == 0 {
		return true
	}

	desc, err := u.ecs.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: &u.cluster,
		Tasks:   taskARNs,
	})
	if err != nil {
		log.Printf("Could not describe tasks")
		return false
	}

	for _, listResult := range desc.Tasks {
		startedBy := aws.StringValue(listResult.StartedBy)
		if !strings.HasPrefix(startedBy, "ecs-svc/") {
			return false
		}
	}
	return true
}

func (u *updater) drainInstance(containerInstance *string) error {
	resp, err := u.ecs.UpdateContainerInstancesState(&ecs.UpdateContainerInstancesStateInput{
		Cluster:            &u.cluster,
		ContainerInstances: []*string{containerInstance},
		Status:             aws.String("DRAINING"),
	})
	if err != nil {
		log.Printf("failed to update container instance %s state to DRAINING: %#v", aws.StringValue(containerInstance), err)
		return err
	}
	if len(resp.Failures) != 0 {
		err = u.activateInstance(containerInstance)
		if err != nil {
			log.Printf("instance failed to reactivate after failing to drain: %#v", err)
		}
		return fmt.Errorf("Container instance %s failed to drain: %#v", aws.StringValue(containerInstance), resp.Failures)
	}
	log.Printf("Container instance state changed to DRAINING")

	err = u.waitUntilDrained(aws.StringValue(containerInstance))
	if err != nil {
		err2 := u.activateInstance(containerInstance)
		if err2 != nil {
			log.Printf("failed to reactivate %s: %s", aws.StringValue(containerInstance), err2.Error())
		}
		return err
	}
	return nil
}

func (u *updater) activateInstance(containerInstance *string) error {
	resp, err := u.ecs.UpdateContainerInstancesState(&ecs.UpdateContainerInstancesStateInput{
		Cluster:            &u.cluster,
		ContainerInstances: []*string{containerInstance},
		Status:             aws.String("ACTIVE"),
	})
	if err != nil {
		log.Printf("failed to update container %s instance state to ACTIVE: %#v", aws.StringValue(containerInstance), err)
		return err
	}
	if len(resp.Failures) != 0 {
		return fmt.Errorf("Container instance %s failed to activate: %#v", aws.StringValue(containerInstance), resp.Failures)
	}
	log.Printf("Container instance state changed to ACTIVE")
	return nil
}

func (u *updater) waitUntilDrained(containerInstance string) error {
	list, err := u.ecs.ListTasks(&ecs.ListTasksInput{
		Cluster:           &u.cluster,
		ContainerInstance: aws.String(containerInstance),
	})
	if err != nil {
		log.Printf("failed to identify a task to wait on")
		return err
	}

	taskARNs := list.TaskArns

	if len(taskARNs) == 0 {
		return nil
	}
	// TODO Tune MaxAttempts
	return u.ecs.WaitUntilTasksStopped(&ecs.DescribeTasksInput{
		Cluster: &u.cluster,
		Tasks:   taskARNs,
	})
}

func (u *updater) sendCommand(instanceIDs []string, ssmCommand string) (string, error) {
	log.Printf("Checking InstanceIDs: %q", instanceIDs)

	resp, err := u.ssm.SendCommand(&ssm.SendCommandInput{
		DocumentName:    aws.String("AWS-RunShellScript"),
		DocumentVersion: aws.String("$DEFAULT"),
		InstanceIds:     aws.StringSlice(instanceIDs),
		Parameters: map[string][]*string{
			"commands": {aws.String(ssmCommand)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("command invocation failed: %#v", err)
	}

	commandID := *resp.Command.CommandId
	// Wait for the sent commands to complete.
	// TODO Update this to use WaitGroups
	for _, v := range instanceIDs {
		u.ssm.WaitUntilCommandExecuted(&ssm.GetCommandInvocationInput{
			CommandId:  &commandID,
			InstanceId: &v,
		})
	}
	log.Printf("CommandID: %s", commandID)
	return commandID, nil
}

func (u *updater) checkSSMCommandOutput(commandID string, instanceIDs []string) ([]string, error) {
	updateCandidates := make([]string, 0)
	for _, v := range instanceIDs {
		resp, err := u.ssm.GetCommandInvocation(&ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: aws.String(v),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve command invocation output: %#v", err)
		}

		type updateCheckResult struct {
			UpdateState string `json:"update_state"`
		}

		var result updateCheckResult
		err = json.Unmarshal([]byte(*resp.StandardOutputContent), &result)
		if err != nil {
			log.Printf("failed to unmarshal command invocation output: %#v", err)
		}
		log.Println("update_state: ", result)

		switch result.UpdateState {
		case "Available":
			updateCandidates = append(updateCandidates, v)
		}
	}

	if len(updateCandidates) == 0 {
		log.Printf("No instances to update")
		return nil, nil
	}
	return updateCandidates, nil
}

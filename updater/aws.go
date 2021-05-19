package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

type instance struct {
	instanceID          string
	containerInstanceID string
}

type ECSAPI interface {
	ListContainerInstancesPages(*ecs.ListContainerInstancesInput, func(*ecs.ListContainerInstancesOutput, bool) bool) error
	DescribeContainerInstances(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error)
	UpdateContainerInstancesState(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error)
	ListTasks(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error)
	DescribeTasks(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error)
	WaitUntilTasksStopped(input *ecs.DescribeTasksInput) error
}

const ecsPageSize = 100

func (u *updater) listContainerInstances() ([]*string, error) {
	containerInstances := make([]*string, 0)
	input := &ecs.ListContainerInstancesInput{
		Cluster: &u.cluster,
	}
	if err := u.ecs.ListContainerInstancesPages(input, func(output *ecs.ListContainerInstancesOutput, lastpage bool) bool {
		if len(output.ContainerInstanceArns) > 0 {
			containerInstances = append(containerInstances, output.ContainerInstanceArns...)
		}
		return !lastpage
	}); err != nil {
		return nil, fmt.Errorf("cannot list container instances: %v", err)
	}
	return containerInstances, nil
}

// filterBottlerocketInstances filters container instances and returns list of
// instances that are running Bottlerocket OS
func (u *updater) filterBottlerocketInstances(instances []*string) ([]instance, error) {
	containerInstances := make([]*ecs.ContainerInstance, 0)
	err := eachPage(len(instances), ecsPageSize, func(start, stop int) error {
		resp, err := u.ecs.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
			Cluster:            &u.cluster,
			ContainerInstances: instances[start:stop],
		})
		if err != nil {
			return fmt.Errorf("cannot describe container instances: %#v", err)
		}
		containerInstances = append(containerInstances, resp.ContainerInstances...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// check the DescribeContainerInstances response and add only Bottlerocket instances to the list
	bottlerocketInstances := make([]instance, 0)
	for _, containerInstance := range containerInstances {
		if containsAttribute(containerInstance.Attributes, "bottlerocket.variant") {
			bottlerocketInstances = append(bottlerocketInstances, instance{
				instanceID:          aws.StringValue(containerInstance.Ec2InstanceId),
				containerInstanceID: aws.StringValue(containerInstance.ContainerInstanceArn),
			})
			log.Printf("Bottlerocket instance detected. Instance %s added to check updates", aws.StringValue(containerInstance.Ec2InstanceId))
		}
	}
	return bottlerocketInstances, nil
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

func eachPage(inputLen int, size int, fn func(start, stop int) error) error {
	for start := 0; start < inputLen; start += size {
		stop := start + size
		if stop > inputLen {
			stop = inputLen
		}
		if err := fn(start, stop); err != nil {
			return err
		}
	}
	return nil
}

// filterAvailableUpdates returns a list of instances that have updates available
func (u *updater) filterAvailableUpdates(bottlerocketInstances []instance) ([]instance, error) {
	// make slice of Bottlerocket instances to use with SendCommand and checkCommandOutput
	instances := make([]string, 0)
	for _, inst := range bottlerocketInstances {
		instances = append(instances, inst.instanceID)
	}

	candidates := make([]instance, 0)
	// iterate through instances in batches of 50 or less for sendCommand
	err := eachPage(len(instances), ssmPageSzie, func(start, stop int) error {
		commandID, err := u.sendCommand(instances, "apiclient update check")
		if err != nil {
			return err
		}

		for _, inst := range bottlerocketInstances {
			commandOutput, err := u.getCommandResult(commandID, inst.instanceID)
			if err != nil {
				return err
			}
			updateState, err := isUpdateAvailable(commandOutput)
			if err != nil {
				return err
			}
			if updateState {
				candidates = append(candidates, inst)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

// eligible checks the eligibility of container instance for update. It's eligible
// if all the running tasks were started by a service.
func (u *updater) eligible(containerInstance string) (bool, error) {
	list, err := u.ecs.ListTasks(&ecs.ListTasksInput{
		Cluster:           &u.cluster,
		ContainerInstance: aws.String(containerInstance),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list tasks: %w", err)
	}
	taskARNs := list.TaskArns
	if len(list.TaskArns) == 0 {
		return true, nil
	}

	desc, err := u.ecs.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: &u.cluster,
		Tasks:   taskARNs,
	})
	if err != nil {
		return false, fmt.Errorf("could not describe tasks: %w", err)
	}
	for _, listResult := range desc.Tasks {
		startedBy := aws.StringValue(listResult.StartedBy)
		if !strings.HasPrefix(startedBy, "ecs-svc/") {
			return false, nil
		}
	}
	return true, nil
}

func (u *updater) drainInstance(containerInstance string) error {
	log.Printf("Starting drain on container instance %q", containerInstance)
	resp, err := u.ecs.UpdateContainerInstancesState(&ecs.UpdateContainerInstancesStateInput{
		Cluster:            &u.cluster,
		ContainerInstances: aws.StringSlice([]string{containerInstance}),
		Status:             aws.String("DRAINING"),
	})
	if err != nil {
		return fmt.Errorf("failed to change instance state to DRAINING: %w", err)
	}
	if len(resp.Failures) != 0 {
		log.Printf("There are API failures in draining the container instance %q, therefore attempting to"+
			" re-activate", containerInstance)
		err = u.activateInstance(containerInstance)
		if err != nil {
			log.Printf("Instance failed to re-activate after failing to change state to DRAINING: %v", err)
		}
		return fmt.Errorf("failures in API call: %v", resp.Failures)
	}
	log.Printf("Container instance state changed to DRAINING")

	err = u.waitUntilDrained(containerInstance)
	if err != nil {
		log.Printf("Container instance %q failed to drain, therefore attempting to re-activate", containerInstance)
		err2 := u.activateInstance(containerInstance)
		if err2 != nil {
			log.Printf("Instance failed to re-activate after failing to wait for drain to complete: %v", err2)
		}
		return fmt.Errorf("error while waiting to drain: %w", err)
	}
	log.Printf("Container instance %q drained successfully!", containerInstance)
	return nil
}

func (u *updater) activateInstance(containerInstance string) error {
	resp, err := u.ecs.UpdateContainerInstancesState(&ecs.UpdateContainerInstancesStateInput{
		Cluster:            &u.cluster,
		ContainerInstances: aws.StringSlice([]string{containerInstance}),
		Status:             aws.String("ACTIVE"),
	})
	if err != nil {
		return fmt.Errorf("failed to change state to ACTIVE: %w", err)
	}
	if len(resp.Failures) != 0 {
		return fmt.Errorf("API failures while activating: %v", resp.Failures)
	}
	log.Printf("Container instance %q state changed to ACTIVE successfully!", containerInstance)
	return nil
}

func (u *updater) waitUntilDrained(containerInstance string) error {
	list, err := u.ecs.ListTasks(&ecs.ListTasksInput{
		Cluster:           &u.cluster,
		ContainerInstance: aws.String(containerInstance),
	})
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}
	taskARNs := list.TaskArns

	if len(taskARNs) == 0 {
		log.Printf("No tasks to drain")
		return nil
	}
	// TODO Tune MaxAttempts
	return u.ecs.WaitUntilTasksStopped(&ecs.DescribeTasksInput{
		Cluster: &u.cluster,
		Tasks:   taskARNs,
	})
}

func (u *updater) sendCommand(instanceIDs []string, ssmCommand string) (string, error) {
	log.Printf("Sending SSM command %q", ssmCommand)
	resp, err := u.ssm.SendCommand(&ssm.SendCommandInput{
		DocumentName:    aws.String("AWS-RunShellScript"),
		DocumentVersion: aws.String("$DEFAULT"),
		InstanceIds:     aws.StringSlice(instanceIDs),
		Parameters: map[string][]*string{
			"commands": {aws.String(ssmCommand)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("send command failed: %w", err)
	}

	commandID := *resp.Command.CommandId
	// Wait for the sent commands to complete.
	// TODO Update this to use WaitGroups
	for _, v := range instanceIDs {
		// TODO handle error
		u.ssm.WaitUntilCommandExecuted(&ssm.GetCommandInvocationInput{
			CommandId:  &commandID,
			InstanceId: &v,
		})
	}
	log.Printf("SSM command %q posted with command id %q", ssmCommand, commandID)
	return commandID, nil
}

func (u *updater) getCommandResult(commandID string, instanceID string) ([]byte, error) {
	resp, err := u.ssm.GetCommandInvocation(&ssm.GetCommandInvocationInput{
		CommandId:  aws.String(commandID),
		InstanceId: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve command invocation output: %w", err)
	}
	commandResults := []byte(aws.StringValue(resp.StandardOutputContent))
	return commandResults, nil
}

func isUpdateAvailable(commandOutput []byte) (bool, error) {
	type updateCheckResult struct {
		UpdateState string `json:"update_state"`
	}

	var updateState updateCheckResult
	err := json.Unmarshal(commandOutput, &updateState)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal update state: %#v", err)
	}
	return updateState.UpdateState == "Available", nil
}

// getActiveVersion unmarshals GetCommandInvocation output to determine the active version of a Bottlerocket instance.
// Takes GetCommandInvocation output as a parameter and returns the active version in use.
func getActiveVersion(commandOutput []byte) (string, error) {
	type version struct {
		Version string `json:"version"`
	}

	type image struct {
		Image version `json:"image"`
	}

	type partition struct {
		ActivePartition image `json:"active_partition"`
	}

	var activeVersion partition
	err := json.Unmarshal(commandOutput, &activeVersion)
	if err != nil {
		log.Printf("failed to unmarshal command invocation output: %#v", err)
		return "", err
	}
	versionInUse := activeVersion.ActivePartition.Image.Version
	return versionInUse, nil
}

// waitUntilOk takes an EC2 ID as a parameter and waits until the specified EC2 instance is in an Ok status.
func (u *updater) waitUntilOk(ec2ID string) error {
	return u.ec2.WaitUntilInstanceStatusOk(&ec2.DescribeInstanceStatusInput{
		InstanceIds: []*string{aws.String(ec2ID)},
	})
}

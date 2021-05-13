package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

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
	ListContainerInstances(*ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error)
	ListContainerInstancesPages(*ecs.ListContainerInstancesInput, func(*ecs.ListContainerInstancesOutput, bool) bool) error
	DescribeContainerInstances(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error)
	UpdateContainerInstancesState(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error)
	ListTasks(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error)
	DescribeTasks(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error)
	WaitUntilTasksStopped(input *ecs.DescribeTasksInput) error
}

func (u *updater) listContainerInstances() ([]*string, error) {
	containerInstances := make([]*string, 0)
	input := &ecs.ListContainerInstancesInput{
		Cluster: &u.cluster,
	}
	fn := func(output *ecs.ListContainerInstancesOutput, lastpage bool) bool {
		if len(output.ContainerInstanceArns) > 0 {
			containerInstances = append(containerInstances, output.ContainerInstanceArns...)
		}
		return !lastpage
	}
	if err := u.ecs.ListContainerInstancesPages(input, fn); err != nil {
		return nil, fmt.Errorf("cannot list container instances: %#v", err)
	}
	return containerInstances, nil
}

// filterBottlerocketInstances filters container instances and returns list of
// instance that are running Bottlerocket OS
func (u *updater) filterBottlerocketInstances(instances []*string) ([]instance, error) {
	resp, err := u.ecs.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster:            &u.cluster,
		ContainerInstances: instances,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot describe container instances: %#v", err)
	}

	bottlerocketInstances := make([]instance, 0)
	// check the DescribeContainerInstances response and add only Bottlerocket instances to the list
	for _, containerInstance := range resp.ContainerInstances {
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

// filterAvailableUpdates returns a list of instances that have updates available
func (u *updater) filterAvailableUpdates(bottlerocketInstances []instance) ([]instance, error) {
	// make slice of Bottlerocket instances to use with SendCommand and checkCommandOutput
	instances := make([]string, 0)
	for _, inst := range bottlerocketInstances {
		instances = append(instances, inst.instanceID)
	}

	commandID, err := u.sendCommand(instances, "apiclient update check")
	if err != nil {
		return nil, err
	}

	candidates := make([]instance, 0)
	for _, inst := range bottlerocketInstances {
		commandOutput, err := u.getCommandResult(commandID, inst.instanceID)
		if err != nil {
			return nil, err
		}
		updateState, err := isUpdateAvailable(commandOutput)
		if err != nil {
			return nil, err
		}
		if updateState {
			candidates = append(candidates, inst)
		}
	}
	return candidates, nil
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
	log.Printf("Container instance %s state changed to ACTIVE", aws.StringValue(containerInstance))
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
	wg := sync.WaitGroup{}
	for _, v := range instanceIDs {
		wg.Add(1)
		go func(instanceID string) {
			u.ssm.WaitUntilCommandExecuted(&ssm.GetCommandInvocationInput{
				CommandId:  aws.String(commandID),
				InstanceId: aws.String(instanceID),
			})
			wg.Done()
		}(aws.StringValue(&v))
	}
	wg.Wait()
	log.Printf("CommandID: %s", commandID)
	return commandID, nil
}

func (u *updater) getCommandResult(commandID string, instanceID string) ([]byte, error) {
	resp, err := u.ssm.GetCommandInvocation(&ssm.GetCommandInvocationInput{
		CommandId:  aws.String(commandID),
		InstanceId: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve command invocation output: %#v", err)
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

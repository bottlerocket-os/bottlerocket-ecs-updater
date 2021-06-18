package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

const (
	pageSize             = 50
	updateStateIdle      = "Idle"
	updateStateStaged    = "Staged"
	updateStateAvailable = "Available"
	updateStateReady     = "Ready"
	waiterDelay          = time.Duration(15) * time.Second
	waiterMaxAttempts    = 100
	// If this time is reached and the ssm command has not already started running, it will not run.
	deliveryTimeoutSeconds = 600
)

type instance struct {
	instanceID          string
	containerInstanceID string
	bottlerocketVersion string
}

type checkOutput struct {
	UpdateState     string `json:"update_state"`
	ActivePartition struct {
		Image struct {
			Version string `json:"version"`
		} `json:"image"`
	} `json:"active_partition"`
}

type ECSAPI interface {
	ListContainerInstances(*ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error)
	DescribeContainerInstances(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error)
	UpdateContainerInstancesState(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error)
	ListTasks(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error)
	DescribeTasks(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error)
	WaitUntilTasksStoppedWithContext(ctx aws.Context, input *ecs.DescribeTasksInput, opts ...request.WaiterOption) error
}

type SSMAPI interface {
	WaitUntilCommandExecutedWithContext(ctx aws.Context, input *ssm.GetCommandInvocationInput, opts ...request.WaiterOption) error
	SendCommand(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error)
	GetCommandInvocation(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error)
}

type EC2API interface {
	WaitUntilInstanceStatusOk(input *ec2.DescribeInstanceStatusInput) error
}

func (u *updater) listContainerInstances() ([]*string, error) {
	log.Printf("Listing active container instances in cluster %q", u.cluster)
	resp, err := u.ecs.ListContainerInstances(&ecs.ListContainerInstancesInput{
		Cluster:    &u.cluster,
		MaxResults: aws.Int64(pageSize),
		Status:     aws.String("ACTIVE"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list container instances: %w", err)
	}
	log.Printf("Found %d container instances in the cluster", len(resp.ContainerInstanceArns))
	return resp.ContainerInstanceArns, nil
}

// filterBottlerocketInstances filters container instances and returns list of
// instances that are running Bottlerocket OS
func (u *updater) filterBottlerocketInstances(instances []*string) ([]instance, error) {
	log.Printf("Filtering container instances running Bottlerocket OS")
	resp, err := u.ecs.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster:            &u.cluster,
		ContainerInstances: instances,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe container instances: %w", err)
	}

	bottlerocketInstances := make([]instance, 0)
	// check the DescribeContainerInstances response and add only Bottlerocket instances to the list
	for _, containerInstance := range resp.ContainerInstances {
		if containsAttribute(containerInstance.Attributes, "bottlerocket.variant") {
			bottlerocketInstances = append(bottlerocketInstances, instance{
				instanceID:          aws.StringValue(containerInstance.Ec2InstanceId),
				containerInstanceID: aws.StringValue(containerInstance.ContainerInstanceArn),
			})
			log.Printf("Bottlerocket instance %q detected", aws.StringValue(containerInstance.Ec2InstanceId))
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
	log.Printf("Filtering instances with available updates")
	// make slice of Bottlerocket instances to use with SendCommand and checkCommandOutput
	instances := make([]string, 0)
	for _, inst := range bottlerocketInstances {
		instances = append(instances, inst.instanceID)
	}

	commandID, err := u.sendCommand(instances, u.checkDocument)
	if err != nil {
		return nil, err
	}

	candidates := make([]instance, 0)
	for _, inst := range bottlerocketInstances {
		commandOutput, err := u.getCommandResult(commandID, inst.instanceID)
		if err != nil {
			return nil, err
		}
		output, err := parseCommandOutput(commandOutput)
		if err != nil {
			// not a fatal error, we can continue checking other instances.
			log.Printf("Failed to parse command output %s: %v", string(commandOutput), err)
			continue
		}
		if output.UpdateState == updateStateAvailable || output.UpdateState == updateStateReady {
			inst.bottlerocketVersion = output.ActivePartition.Image.Version
			candidates = append(candidates, inst)
		}
	}
	return candidates, nil
}

// eligible checks the eligibility of container instance for update. It's eligible
// if all the running tasks were started by a service.
func (u *updater) eligible(containerInstance string) (bool, error) {
	log.Printf("Checking eligiblity for update of container instance %q", containerInstance)
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
		return false, fmt.Errorf("failed to describe tasks: %w", err)
	}
	for _, listResult := range desc.Tasks {
		startedBy := aws.StringValue(listResult.StartedBy)
		if !strings.HasPrefix(startedBy, "ecs-svc/") {
			log.Printf("Container instance %q has a non-service task running: %s", containerInstance, aws.StringValue(listResult.TaskArn))
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
	log.Printf("Waiting for container instance %q to drain", containerInstance)
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

	return u.ecs.WaitUntilTasksStoppedWithContext(aws.BackgroundContext(), &ecs.DescribeTasksInput{
		Cluster: &u.cluster,
		Tasks:   taskARNs,
	},
		request.WithWaiterMaxAttempts(waiterMaxAttempts),
		request.WithWaiterDelay(request.ConstantWaiterDelay(waiterDelay)),
	)
}

// updateInstance starts an update process on an instance.
func (u *updater) updateInstance(inst instance) error {
	log.Printf("Starting update on instance %q", inst.instanceID)
	ec2IDs := []string{inst.instanceID}
	log.Printf("Checking current update state of instance %q", inst.instanceID)

	commandID, err := u.sendCommand(ec2IDs, u.checkDocument)
	if err != nil {
		return fmt.Errorf("failed to send check command: %w", err)
	}
	output, err := u.getCommandResult(commandID, inst.instanceID)
	if err != nil {
		return fmt.Errorf("failed to get check command output: %w", err)
	}
	check, err := parseCommandOutput(output)
	if err != nil {
		return fmt.Errorf("failed to parse command output %s: %w", string(output), err)
	}

	switch check.UpdateState {
	case updateStateIdle:
		log.Printf("No new update available for instance %q", inst.instanceID)
		return nil
	case updateStateStaged:
		return fmt.Errorf("unexpected update state %q; skipping instance", check.UpdateState)
	case updateStateAvailable:
		log.Printf("Starting update apply on instance %q", inst.instanceID)
		_, err := u.sendCommand(ec2IDs, u.applyDocument)
		if err != nil {
			return fmt.Errorf("failed to send update apply command: %w", err)
		}
	case updateStateReady:
		log.Printf("Update is previously applied on instance %q", inst.instanceID)
	default:
		return fmt.Errorf("unknown update state %q", check.UpdateState)
	}

	// occasionally instance goes into reboot before reporting command output, therefore
	// we do not poll for command output. Instead we rely on verifyUpdate to confirm update
	// success or failure.
	log.Printf("Sending SSM document %q on instance %q", u.rebootDocument, inst.instanceID)
	// SendCommand is directly called here because we do not want to wait on command complete.
	resp, err := u.ssm.SendCommand(&ssm.SendCommandInput{
		DocumentName:    aws.String(u.rebootDocument),
		DocumentVersion: aws.String("$DEFAULT"),
		InstanceIds:     aws.StringSlice(ec2IDs),
		TimeoutSeconds:  aws.Int64(deliveryTimeoutSeconds),
	})
	if err != nil {
		return fmt.Errorf("failed to send reboot command: %w", err)
	}
	rebootID := *resp.Command.CommandId
	log.Printf("SSM document %q posted with command ID %q", u.rebootDocument, rebootID)

	// added some sleep time for reboot to start before we check instance state
	time.Sleep(15 * time.Second)
	err = u.waitUntilOk(inst.instanceID)
	if err != nil {
		return fmt.Errorf("failed to reach Ok status after reboot: %w", err)
	}
	return nil
}

// verifyUpdate verifies if instance was properly updated
func (u *updater) verifyUpdate(inst instance) (bool, error) {
	log.Println("Verifying update by checking there is no new version available to update" +
		" and validate the active version")
	ec2IDs := []string{inst.instanceID}
	updateStatus, err := u.sendCommand(ec2IDs, u.checkDocument)
	if err != nil {
		return false, fmt.Errorf("failed to send update check command: %w", err)
	}

	updateResult, err := u.getCommandResult(updateStatus, inst.instanceID)
	if err != nil {
		return false, fmt.Errorf("failed to get check command output: %w", err)
	}
	output, err := parseCommandOutput(updateResult)
	if err != nil {
		return false, fmt.Errorf("failed to parse command output %s, manual verification required: %w", string(updateResult), err)
	}
	updatedVersion := output.ActivePartition.Image.Version
	if updatedVersion == inst.bottlerocketVersion {
		log.Printf("Container instance %q did not update, its current "+
			"version %s and updated version %s are the same", inst.containerInstanceID, inst.bottlerocketVersion, updatedVersion)
		return false, nil
	} else if output.UpdateState == updateStateAvailable {
		log.Printf("Container instance %q was updated to version %q successfully, however another newer version was recently released;"+
			" Instance will be updated to newer version in next iteration.", inst.containerInstanceID, updatedVersion)
		return true, nil
	} else {
		log.Printf("Container instance %q updated to version %q", inst.containerInstanceID, updatedVersion)
	}
	return true, nil
}

func (u *updater) sendCommand(instanceIDs []string, ssmDocument string) (string, error) {
	log.Printf("Sending SSM document %q", ssmDocument)
	resp, err := u.ssm.SendCommand(&ssm.SendCommandInput{
		DocumentName:    aws.String(ssmDocument),
		DocumentVersion: aws.String("$DEFAULT"),
		InstanceIds:     aws.StringSlice(instanceIDs),
		TimeoutSeconds:  aws.Int64(deliveryTimeoutSeconds),
	})
	if err != nil {
		return "", fmt.Errorf("send command failed: %w", err)
	}
	commandID := *resp.Command.CommandId
	log.Printf("SSM document %q posted with command id %q", ssmDocument, commandID)

	// Wait for the sent commands to complete.
	errCount := 0
	for _, v := range instanceIDs {
		log.Printf("Waiting for command %q to complete for instance %q", commandID, v)
		err = u.ssm.WaitUntilCommandExecutedWithContext(aws.BackgroundContext(), &ssm.GetCommandInvocationInput{
			CommandId:  &commandID,
			InstanceId: &v,
		},
			request.WithWaiterMaxAttempts(waiterMaxAttempts),
			request.WithWaiterDelay(request.ConstantWaiterDelay(waiterDelay)))
		if err != nil {
			errCount++
			log.Printf("Error encountered while awaiting document %q execution for instance: %q: %s", ssmDocument, v, err)
		}
	}
	// TODO return a list of instanceIDs which ecnountered no waiter errors.
	if errCount == len(instanceIDs) {
		return "", fmt.Errorf("too many failures while awaiting document execution: %w", err)
	}
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

// waitUntilOk takes an EC2 ID as a parameter and waits until the specified EC2 instance is in an Ok status.
func (u *updater) waitUntilOk(ec2ID string) error {
	log.Printf("Waiting for instance %q to reach Ok status", ec2ID)
	return u.ec2.WaitUntilInstanceStatusOk(&ec2.DescribeInstanceStatusInput{
		InstanceIds: []*string{aws.String(ec2ID)},
	})
}

// parseCommandOutput takes raw bytes of ssm command output and converts it into a struct
func parseCommandOutput(commandOutput []byte) (checkOutput, error) {
	output := checkOutput{}
	err := json.Unmarshal(commandOutput, &output)
	if err != nil {
		return output, fmt.Errorf("failed to unmarshal json: %w", err)
	}
	if output.UpdateState == "" || output.ActivePartition.Image.Version == "" {
		return output, fmt.Errorf("mandatory fields are not available")
	}
	return output, nil
}

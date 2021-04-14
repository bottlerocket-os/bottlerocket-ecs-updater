package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

const (
	pageSize = 50
)

func listContainerInstances(ecsClient *ecs.ECS, cluster string, pageSize int64) ([]*string, error) {
	resp, err := ecsClient.ListContainerInstances(&ecs.ListContainerInstancesInput{
		Cluster: &cluster,
		MaxResults: aws.Int64(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list container instances: %#v", err)
	}
	log.Printf("%#v", resp)
	return resp.ContainerInstanceArns, nil
}

func filterBottlerocketInstances(ecsClient *ecs.ECS, cluster string, instances []*string) ([]*string, error) {
	resp, err := ecsClient.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster: &cluster, ContainerInstances: instances,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot describe container instances: %#v", err)
	}

	log.Printf("Container descriptions: %#v", resp)

	ec2IDs := make([]*string, 0)
	// Check the DescribeInstances response for Bottlerocket container instances, add them to ec2ids if detected
	for _, instance := range resp.ContainerInstances {
		if containsAttribute(instance.Attributes, "bottlerocket.variant") {
			ec2IDs = append(ec2IDs, instance.Ec2InstanceId)
			log.Printf("Bottlerocket instance detected. Instance %#v added to check updates", *instance.Ec2InstanceId)
		}
	}
	return ec2IDs, nil
}

// checks if ECS Attributes struct contains a specified string
func containsAttribute(attrs []*ecs.Attribute, searchString string) bool {
	for _, attr := range attrs {
		if aws.StringValue(attr.Name) == searchString {
			return true
		}
	}
	return false
}

func sendCommand(ssmClient *ssm.SSM, instanceIDs []*string, ssmCommand string) (string, error) {
	log.Printf("Checking InstanceIDs: %#v", instanceIDs)

	resp, err := ssmClient.SendCommand(&ssm.SendCommandInput{
		DocumentName:    aws.String("AWS-RunShellScript"),
		DocumentVersion: aws.String("$DEFAULT"),
		InstanceIds:     instanceIDs,
		Parameters: map[string][]*string{
			"commands": {aws.String(ssmCommand)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("command invocation failed: %#v", err)
	}

	commandID := *resp.Command.CommandId
	// Wait for the sent commands to complete
	// TODO Update this to use WaitGroups
	for _, v := range instanceIDs {
		ssmClient.WaitUntilCommandExecuted(&ssm.GetCommandInvocationInput{
			CommandId:  &commandID,
			InstanceId: v,
		})
	}
	log.Printf("CommandID: %#v", commandID)
	return commandID, nil
}

func checkCommandOutput(ssmClient *ssm.SSM, commandID string, instanceIDs []*string) ([]string, error) {
	updateCandidates := make([]string, 0)
	for _, v := range instanceIDs {
		resp, err := ssmClient.GetCommandInvocation(&ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: v,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to retreive command invocation output: %#v", err)
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
			updateCandidates = append(updateCandidates, *v)
		}
	}

	if updateCandidates == nil {
		log.Printf("No instances to update")
	}
	return updateCandidates, nil
}

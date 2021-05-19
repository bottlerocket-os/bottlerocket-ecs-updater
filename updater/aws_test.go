package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendCommand(t *testing.T) {
	cases := []struct {
		name          string
		sendOutput    *ssm.SendCommandOutput
		sendError     error
		expectedError string
		expectedOut   string
		waitError     error
	}{
		{
			name: "send success",
			sendOutput: &ssm.SendCommandOutput{
				Command: &ssm.Command{CommandId: aws.String("id1")},
			},
			expectedOut: "id1",
		},
		{
			name:          "send fail",
			sendError:     errors.New("failed to send command"),
			expectedError: "send command failed",
		},
		{
			name: "wait failure",
			sendOutput: &ssm.SendCommandOutput{
				Command: &ssm.Command{CommandId: aws.String("")},
			},
			waitError:     errors.New("exceeded max attempts"),
			expectedError: "too many failures while awaiting SSM Command execution",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockSSM := &MockSSM{
				SendCommandFn: func(_ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					return tc.sendOutput, tc.sendError
				},
				WaitUntilCommandExecutedFn: func(_ *ssm.GetCommandInvocationInput) error {
					return tc.waitError
				},
			}
			u := &updater{ssm: mockSSM}
			actual, err := u.sendCommand([]string{"inst-id-1", "inst-id-2", "inst-id-3"}, "run me")
			if tc.expectedError != "" && tc.sendError != nil {
				assert.EqualError(t, err, fmt.Sprintf("%s: %v", tc.expectedError, tc.sendError))
			} else if tc.expectedError != "" && tc.waitError != nil {
				assert.EqualError(t, err, fmt.Sprintf("%s: error while waiting on command execution %v", tc.expectedError, tc.waitError))
			} else {
				assert.EqualValues(t, tc.expectedOut, actual)
			}
		})
	}
}

func TestListContainerInstances(t *testing.T) {
	cases := []struct {
		name          string
		listOutput    *ecs.ListContainerInstancesOutput
		listOutput2   *ecs.ListContainerInstancesOutput
		listError     error
		expectedError string
		expectedOut   []*string
	}{
		{
			name: "with instances",
			listOutput: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{
					aws.String("cont-inst-arn1"),
					aws.String("cont-inst-arn2"),
					aws.String("cont-inst-arn3")},
				NextToken: aws.String("token"),
			},
			listOutput2: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{
					aws.String("cont-inst-arn4"),
					aws.String("cont-inst-arn5"),
					aws.String("cont-inst-arn6")},
				NextToken: nil,
			},
			expectedOut: []*string{
				aws.String("cont-inst-arn1"),
				aws.String("cont-inst-arn2"),
				aws.String("cont-inst-arn3"),
				aws.String("cont-inst-arn4"),
				aws.String("cont-inst-arn5"),
				aws.String("cont-inst-arn6")},
			expectedError: "",
		},
		{
			name: "without instances",
			listOutput: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
				NextToken:             nil,
			},
			listOutput2: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
				NextToken:             nil,
			},
			expectedOut: []*string{},
		},
		{
			name:      "list fail",
			listError: errors.New("failed to list instances"),
			listOutput: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
			},
			listOutput2: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
			},
			expectedError: "cannot list container instances",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockECS := MockECS{
				ListContainerInstancesPagesFn: func(_ *ecs.ListContainerInstancesInput, fn func(*ecs.ListContainerInstancesOutput, bool) bool) error {
					fn(tc.listOutput, true)
					fn(tc.listOutput2, false)
					return tc.listError
				},
			}
			u := updater{ecs: mockECS}
			actual, err := u.listContainerInstances()
			if tc.expectedError != "" && tc.listError != nil {
				assert.EqualError(t, err, fmt.Sprintf("%s: %v", tc.expectedError, tc.listError))
			} else if actual == nil {
				assert.EqualValues(t, tc.expectedOut, actual)
			} else {
				require.NoError(t, err)
				assert.EqualValues(t, tc.expectedOut, actual)
			}
		},
		)
	}
}

func TestFilterBottlerocketInstances(t *testing.T) {
	output := &ecs.DescribeContainerInstancesOutput{
		ContainerInstances: []*ecs.ContainerInstance{{
			// Bottlerocket with single attribute
			Attributes:           []*ecs.Attribute{{Name: aws.String("bottlerocket.variant")}},
			ContainerInstanceArn: aws.String("cont-inst-br1"),
			Ec2InstanceId:        aws.String("ec2-id-br1"),
		}, {
			// Bottlerocket with extra attribute
			Attributes: []*ecs.Attribute{
				{Name: aws.String("different-attribute")},
				{Name: aws.String("bottlerocket.variant")},
			},
			ContainerInstanceArn: aws.String("cont-inst-br2"),
			Ec2InstanceId:        aws.String("ec2-id-br2"),
		}, {
			// Not Bottlerocket, single attribute
			Attributes: []*ecs.Attribute{
				{Name: aws.String("different-attribute")},
			},
			ContainerInstanceArn: aws.String("cont-inst-not1"),
			Ec2InstanceId:        aws.String("ec2-id-not1"),
		}, {
			// Not Bottlerocket, no attribute
			ContainerInstanceArn: aws.String("cont-inst-not2"),
			Ec2InstanceId:        aws.String("ec2-id-not2"),
		}},
	}
	expected := []instance{
		{
			instanceID:          "ec2-id-br1",
			containerInstanceID: "cont-inst-br1",
		},
		{
			instanceID:          "ec2-id-br2",
			containerInstanceID: "cont-inst-br2",
		},
	}

	mockECS := MockECS{
		DescribeContainerInstancesFn: func(_ *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error) {
			return output, nil
		},
	}
	u := updater{ecs: mockECS}

	actual, err := u.filterBottlerocketInstances([]*string{
		aws.String("ec2-id-br1"),
		aws.String("ec2-id-br2"),
		aws.String("ec2-id-not1"),
		aws.String("ec2-id-not2"),
	})
	require.NoError(t, err)
	assert.EqualValues(t, expected, actual)
}

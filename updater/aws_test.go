package main

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendCommand(t *testing.T) {
	// commandSuccessInstance indicates an instance for which the command should succeed
	// regardless of whether `waitError` is set.
	const commandSuccessInstance = "inst-success"
	cases := []struct {
		name          string
		sendOutput    *ssm.SendCommandOutput
		sendError     error
		expectedError string
		expectedOut   string
		waitError     error
		instances     []string
	}{
		{
			name: "send success",
			sendOutput: &ssm.SendCommandOutput{
				Command: &ssm.Command{CommandId: aws.String("id1")},
			},
			instances:   []string{"inst-id-1"},
			expectedOut: "id1",
		},
		{
			name:          "send fail",
			sendError:     errors.New("failed to send command"),
			expectedError: "send command failed",
			instances:     []string{"inst-id-1"},
		},
		{
			name:      "wait single failure",
			waitError: errors.New("exceeded max attempts"),
			sendOutput: &ssm.SendCommandOutput{
				Command: &ssm.Command{CommandId: aws.String("")},
			},
			expectedError: "too many failures while awaiting document execution",
			instances:     []string{"inst-id-1"},
		},
		{
			name:      "wait one succcess",
			waitError: errors.New("exceeded max attempts"),
			sendOutput: &ssm.SendCommandOutput{
				Command: &ssm.Command{CommandId: aws.String("id1")},
			},
			instances:   []string{"inst-id-1", "inst-id-2", commandSuccessInstance},
			expectedOut: "id1",
		},
		{
			name:      "wait fail all",
			waitError: errors.New("exceeded max attempts"),
			sendOutput: &ssm.SendCommandOutput{
				Command: &ssm.Command{CommandId: aws.String("id1")},
			},
			expectedError: "too many failures while awaiting document execution",
			instances:     []string{"inst-id-1", "inst-id-2", "inst-id-3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockSSM := MockSSM{
				SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					assert.Equal(t, "test-doc", aws.StringValue(input.DocumentName))
					assert.Equal(t, "$DEFAULT", aws.StringValue(input.DocumentVersion))
					assert.Equal(t, aws.StringSlice(tc.instances), input.InstanceIds)
					return tc.sendOutput, tc.sendError
				},
				WaitUntilCommandExecutedWithContextFn: func(ctx aws.Context, input *ssm.GetCommandInvocationInput, opts ...request.WaiterOption) error {
					if aws.StringValue(input.InstanceId) == commandSuccessInstance {
						return nil
					}
					return tc.waitError
				},
			}
			u := updater{ssm: mockSSM}
			actual, err := u.sendCommand(tc.instances, "test-doc")
			if tc.expectedOut != "" {
				require.NoError(t, err)
				assert.EqualValues(t, tc.expectedOut, actual)
			} else if tc.sendError != nil {
				assert.ErrorIs(t, err, tc.sendError)
				assert.Contains(t, err.Error(), tc.expectedError)
			} else {
				assert.ErrorIs(t, err, tc.waitError)
				assert.Contains(t, err.Error(), tc.expectedError)
			}
		})
	}
}

func TestListContainerInstances(t *testing.T) {
	cases := []struct {
		name          string
		listOutput    *ecs.ListContainerInstancesOutput
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
			},
			expectedOut: []*string{
				aws.String("cont-inst-arn1"),
				aws.String("cont-inst-arn2"),
				aws.String("cont-inst-arn3"),
			},
		},
		{
			name: "without instances",
			listOutput: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
			},
			expectedOut: []*string{},
		},
		{
			name:          "list fail",
			listError:     errors.New("failed to list instances"),
			expectedError: "cannot list container instances",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockECS := MockECS{
				ListContainerInstancesFn: func(input *ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error) {
					assert.Equal(t, int64(pageSize), aws.Int64Value(input.MaxResults))
					assert.Equal(t, "ACTIVE", aws.StringValue(input.Status))
					return tc.listOutput, tc.listError
				},
			}
			u := updater{ecs: mockECS}
			actual, err := u.listContainerInstances()
			if tc.expectedOut != nil {
				assert.EqualValues(t, tc.expectedOut, actual)
				assert.NoError(t, err)
			} else {
				assert.Empty(t, actual)
				assert.ErrorIs(t, err, tc.listError)
				assert.Contains(t, err.Error(), tc.expectedError)
			}
		})
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

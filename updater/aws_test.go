package main

import (
	"errors"
	"fmt"
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
			expectedError: "failed to list container instances",
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

func TestEligible(t *testing.T) {
	cases := []struct {
		name        string
		listOut     *ecs.ListTasksOutput
		describeOut *ecs.DescribeTasksOutput
		expectedOk  bool
	}{
		{
			name: "only service tasks",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
				},
			},
			describeOut: &ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{
					{
						// contains proper prefix "ecs-svc" for task started by service
						StartedBy: aws.String("ecs-svc/svc-id"),
					},
				},
			},
			expectedOk: true,
		}, {
			name: "no task",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{},
			},
			expectedOk: true,
		}, {
			name: "non service task",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
				},
			},
			describeOut: &ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{{
					// Does not contain prefix "ecs-svc"
					StartedBy: aws.String("standalone-task-id"),
				}},
			},
			expectedOk: false,
		}, {
			name: "non service task empty StartedBy",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
				},
			},
			describeOut: &ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{{}},
			},
			expectedOk: false,
		}, {
			name: "service and non service tasks",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
					aws.String("task-arn-2"),
				},
			},
			describeOut: &ecs.DescribeTasksOutput{
				Tasks: []*ecs.Task{{
					// Does not contain prefix "ecs-svc"
					StartedBy: aws.String("standalone-task-id"),
				}, {
					// contains proper prefix "ecs-svc" for task started by service
					StartedBy: aws.String("ecs-svc/svc-id"),
				}},
			},
			expectedOk: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockECS := MockECS{
				ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
					return tc.listOut, nil
				},
				DescribeTasksFn: func(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error) {
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					assert.Equal(t, tc.listOut.TaskArns, input.Tasks)
					return tc.describeOut, nil
				},
			}
			u := updater{ecs: mockECS, cluster: "test-cluster"}
			ok, err := u.eligible("cont-inst-id")
			require.NoError(t, err)
			assert.Equal(t, ok, tc.expectedOk)
		})
	}
}

func TestEligibleErr(t *testing.T) {
	t.Run("list task err", func(t *testing.T) {
		listErr := errors.New("failed to list tasks")
		mockECS := MockECS{
			ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
				return nil, listErr
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		ok, err := u.eligible("cont-inst-id")
		require.Error(t, err)
		assert.ErrorIs(t, err, listErr)
		assert.False(t, ok)
	})

	t.Run("describe task err", func(t *testing.T) {
		describeErr := errors.New("failed to describe tasks")
		mockECS := MockECS{
			ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
				return &ecs.ListTasksOutput{
					TaskArns: []*string{
						aws.String("task-arn-1"),
					},
				}, nil
			},
			DescribeTasksFn: func(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error) {
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, []*string{
					aws.String("task-arn-1"),
				}, input.Tasks)
				return nil, describeErr
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		ok, err := u.eligible("cont-inst-id")
		require.Error(t, err)
		assert.ErrorIs(t, err, describeErr)
		assert.False(t, ok)
	})
}

func TestDrainInstance(t *testing.T) {
	stateChangeCalls := []string{}
	mockStateChange := func(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error) {
		stateChangeCalls = append(stateChangeCalls, aws.StringValue(input.Status))
		assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
		assert.Equal(t, []*string{aws.String("cont-inst-id")}, input.ContainerInstances)
		return &ecs.UpdateContainerInstancesStateOutput{
			Failures: []*ecs.Failure{},
		}, nil
	}
	mockListTasks := func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
		assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
		assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
		return &ecs.ListTasksOutput{
			TaskArns: []*string{
				aws.String("task-arn-1"),
			},
		}, nil
	}
	cleanup := func() {
		stateChangeCalls = []string{}
	}

	t.Run("no tasks success", func(t *testing.T) {
		defer cleanup()
		listTaskCount := 0
		mockECS := MockECS{
			UpdateContainerInstancesStateFn: mockStateChange,
			ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
				listTaskCount++
				return &ecs.ListTasksOutput{
					TaskArns: []*string{},
				}, nil
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		err := u.drainInstance("cont-inst-id")
		require.NoError(t, err)
		assert.Equal(t, 1, listTaskCount)
		assert.Equal(t, []string{"DRAINING"}, stateChangeCalls)
	})

	t.Run("with tasks success", func(t *testing.T) {
		defer cleanup()
		waitCount := 0
		mockECS := MockECS{
			UpdateContainerInstancesStateFn: mockStateChange,
			ListTasksFn:                     mockListTasks,
			WaitUntilTasksStoppedWithContextFn: func(ctx aws.Context, input *ecs.DescribeTasksInput, opts ...request.WaiterOption) error {
				assert.Equal(t, []*string{
					aws.String("task-arn-1"),
				}, input.Tasks)
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				waitCount++
				return nil
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		err := u.drainInstance("cont-inst-id")
		require.NoError(t, err)
		assert.Equal(t, []string{"DRAINING"}, stateChangeCalls)
		assert.Equal(t, 1, waitCount)
	})

	t.Run("state change err", func(t *testing.T) {
		defer cleanup()
		stateOutErr := errors.New("failed to change state")
		mockECS := MockECS{
			UpdateContainerInstancesStateFn: func(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error) {
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, []*string{aws.String("cont-inst-id")}, input.ContainerInstances)
				return nil, stateOutErr
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		err := u.drainInstance("cont-inst-id")
		require.Error(t, err)
		assert.ErrorIs(t, err, stateOutErr)
	})

	t.Run("state change api err", func(t *testing.T) {
		defer cleanup()
		stateOutAPIFailure := &ecs.UpdateContainerInstancesStateOutput{
			Failures: []*ecs.Failure{
				{
					Reason: aws.String("failed"),
				},
			},
		}
		mockECS := MockECS{
			UpdateContainerInstancesStateFn: func(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error) {
				stateChangeCalls = append(stateChangeCalls, aws.StringValue(input.Status))
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, []*string{aws.String("cont-inst-id")}, input.ContainerInstances)
				return stateOutAPIFailure, nil
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		err := u.drainInstance("cont-inst-id")
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("%v", stateOutAPIFailure.Failures))
		assert.Equal(t, []string{"DRAINING", "ACTIVE"}, stateChangeCalls)
	})

	t.Run("list task err", func(t *testing.T) {
		defer cleanup()
		listTaskErr := errors.New("failed to list tasks")
		mockECS := MockECS{
			UpdateContainerInstancesStateFn: mockStateChange,
			ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
				return nil, listTaskErr
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		err := u.drainInstance("cont-inst-id")
		require.Error(t, err)
		assert.ErrorIs(t, err, listTaskErr)
		assert.Equal(t, []string{"DRAINING", "ACTIVE"}, stateChangeCalls)
	})

	t.Run("wait tasks stop err", func(t *testing.T) {
		defer cleanup()
		waitTaskErr := errors.New("failed to wait for tasks to stop")
		mockECS := MockECS{
			UpdateContainerInstancesStateFn: mockStateChange,
			ListTasksFn:                     mockListTasks,
			WaitUntilTasksStoppedWithContextFn: func(ctx aws.Context, input *ecs.DescribeTasksInput, opts ...request.WaiterOption) error {
				assert.Equal(t, []*string{
					aws.String("task-arn-1"),
				}, input.Tasks)
				assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
				return waitTaskErr
			},
		}
		u := updater{ecs: mockECS, cluster: "test-cluster"}
		err := u.drainInstance("cont-inst-id")
		require.Error(t, err)
		assert.ErrorIs(t, err, waitTaskErr)
		assert.Equal(t, []string{"DRAINING", "ACTIVE"}, stateChangeCalls)
	})
}

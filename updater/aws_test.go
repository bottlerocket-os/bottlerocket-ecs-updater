package main

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterAvailableUpdates(t *testing.T) {
	instances := []instance{
		{
			instanceID:          "inst-id-1",
			containerInstanceID: "cont-inst-1",
		},
		{
			instanceID:          "inst-id-2",
			containerInstanceID: "cont-inst-2",
		},
		{
			instanceID:          "inst-id-3",
			containerInstanceID: "cont-inst-3",
		},
		{
			instanceID:          "inst-id-4",
			containerInstanceID: "cont-inst-4",
		},
		{
			instanceID:          "inst-id-5",
			containerInstanceID: "cont-inst-5",
		},
	}
	expected := []instance{
		{
			instanceID:          "inst-id-1",
			containerInstanceID: "cont-inst-1",
			bottlerocketVersion: "v1.0.5",
		},
		{
			instanceID:          "inst-id-2",
			containerInstanceID: "cont-inst-2",
			bottlerocketVersion: "v1.0.5",
		},
		{
			instanceID:          "inst-id-5",
			containerInstanceID: "cont-inst-5",
			bottlerocketVersion: "v1.0.5",
		},
	}
	responses := map[string]string{
		"inst-id-1": `{"update_state": "Available", "active_partition": { "image": { "version": "v1.0.5"}}}`,
		"inst-id-2": `{"update_state": "Ready", "active_partition": { "image": { "version": "v1.0.5"}}}`,
		"inst-id-3": `{"update_state": "Idle", "active_partition": { "image": { "version": "v1.1.1"}}}`,
		"inst-id-4": `{"update_state": "Staged", "active_partition": { "image": { "version": "v1.1.1"}}}`,
		"inst-id-5": `{"update_state": "Available", "active_partition": { "image": { "version": "v1.0.5"}}}`,
	}

	// mutex needed to prevent race condition when incrementing counter in concurrent
	// execution of WaitUntilCommandExecutedWithContextFn
	var m sync.Mutex
	sendCommandCalls := 0
	commandWaiterCalls := 0
	getCommandInvocationCalls := 0
	mockSSM := MockSSM{
		GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			getCommandInvocationCalls++
			return &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(responses[*input.InstanceId]),
			}, nil
		},
		SendCommandFn: func(_ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			sendCommandCalls++
			return &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId:    aws.String("command-id"),
					DocumentName: aws.String("check-document"),
				},
			}, nil
		},
		WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
			m.Lock()
			commandWaiterCalls++
			m.Unlock()
			assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
			return nil
		},
	}
	u := updater{ssm: mockSSM, checkDocument: "check-document"}
	actual, err := u.filterAvailableUpdates(instances)
	require.NoError(t, err)
	assert.Equal(t, expected, actual, "Should only contain instances in Aavailable or Ready update state")
	assert.Equal(t, 1, sendCommandCalls, "should send commands for each page")
	assert.Equal(t, 5, commandWaiterCalls, "should wait for each instance")
	assert.Equal(t, 5, getCommandInvocationCalls, "should collect output for each instance")
}

func TestPaginatedFilterAvailableUpdatesSuccess(t *testing.T) {
	checkPattern := `{"update_state": "%s", "active_partition": { "image": { "version": "%s"}}}`
	expected := make([]instance, 0)
	instances := make([]instance, 0)
	getOut := &ssm.GetCommandInvocationOutput{
		Status:                aws.String("Success"),
		StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateAvailable, "v1.0.5")),
	}

	for i := 0; i < 100; i++ { // 100 is chosen here to reprsent 2 full pages of SSM (limited to 50 per page)
		containerID := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, instance{
			instanceID:          ec2ID,
			containerInstanceID: containerID,
		})
		expected = append(expected, instance{
			instanceID:          ec2ID,
			containerInstanceID: containerID,
			bottlerocketVersion: "v1.0.5",
		})
	}

	// mutex needed to prevent race condition when incrementing counter in concurrent
	// execution of WaitUntilCommandExecutedWithContextFn
	var m sync.Mutex
	sendCommandCalls := 0
	commandWaiterCalls := 0
	getCommandInvocationCalls := 0
	mockSSM := MockSSM{
		GetCommandInvocationFn: func(_ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			getCommandInvocationCalls++
			return getOut, nil
		},
		SendCommandFn: func(_ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			sendCommandCalls++
			return &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId:    aws.String("command-id"),
					DocumentName: aws.String("check-document"),
				},
			}, nil
		},
		WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
			m.Lock()
			commandWaiterCalls++
			m.Unlock()
			assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
			return nil
		},
	}
	u := updater{ssm: mockSSM}
	actual, err := u.filterAvailableUpdates(instances)
	require.NoError(t, err)
	assert.EqualValues(t, expected, actual, "should contain all instances")
	assert.Equal(t, 2, sendCommandCalls, "should send commands for each page")
	assert.Equal(t, 100, commandWaiterCalls, "should wait for each instance")
	assert.Equal(t, 100, getCommandInvocationCalls, "should collect output for each instance")
}

func TestPaginatedFilterAvailableUpdatesAllFail(t *testing.T) {
	instances := make([]instance, 0)

	for i := 0; i < 100; i++ {
		containerID := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, instance{
			instanceID:          ec2ID,
			containerInstanceID: containerID,
		})
	}

	sendCommandCalls := 0
	mockSSM := MockSSM{
		SendCommandFn: func(_ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			sendCommandCalls++
			return nil, errors.New("Failed to send document")
		},
	}
	u := updater{ssm: mockSSM}
	actual, err := u.filterAvailableUpdates(instances)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to send document")
	assert.Empty(t, actual)
	assert.Equal(t, 2, sendCommandCalls, "should send commands for each page")
}

func TestPaginatedFilterAvailableUpdatesInPageFailures(t *testing.T) {
	instances := make([]instance, 0)
	checkPattern := `{"update_state": "%s", "active_partition": { "image": { "version": "%s"}}}`
	for i := 0; i < 120; i++ { // 120 chosen here to ensure multiple pages are tested and that number instances divides by 3 evenly
		containerID := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, instance{
			instanceID:          ec2ID,
			containerInstanceID: containerID,
		})
	}

	// mutex needed to prevent race condition when incrementing counter in concurrent
	// execution of WaitUntilCommandExecutedWithContextFn
	var m sync.Mutex
	sendCommandCalls := 0
	commandWaiterCalls := 0
	getCommandInvocationCalls := 0
	count := 0
	mockSSM := MockSSM{
		GetCommandInvocationFn: func(_ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			count++
			getCommandInvocationCalls++
			switch count % 3 {
			case 0:
				return nil, errors.New("Failed to get command output") // validate getCommandResult failure
			case 1:
				return &ssm.GetCommandInvocationOutput{
					Status:                aws.String("Success"),
					StandardOutputContent: aws.String("{}"),
				}, nil // validates parseCommandOutput failure
			case 2:
				return &ssm.GetCommandInvocationOutput{
					Status:                aws.String("Success"),
					StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateAvailable, "v1.0.5")),
				}, nil // validate success case
			}
			return nil, nil
		},
		SendCommandFn: func(_ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			sendCommandCalls++
			return &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId:    aws.String("command-id"),
					DocumentName: aws.String("check-document"),
				},
			}, nil
		},
		WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
			assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
			m.Lock()
			commandWaiterCalls++
			m.Unlock()
			return nil
		},
	}
	u := updater{ssm: mockSSM}
	actual, err := u.filterAvailableUpdates(instances)
	require.NoError(t, err)
	assert.EqualValues(t, 40, len(actual), "Every 3rd instance of 120 should succeed")
	assert.Equal(t, 3, sendCommandCalls, "should send commands for each page")
	assert.Equal(t, 120, commandWaiterCalls, "should wait for each instance")
	assert.Equal(t, 120, getCommandInvocationCalls, "should collect output for each instance")
}

func TestPaginatedFilterAvailableUpdatesSingleErr(t *testing.T) {
	checkPattern := `{"update_state": "%s", "active_partition": { "image": { "version": "%s"}}}`
	expected := make([]instance, 0)
	instances := make([]instance, 0)
	getOut := &ssm.GetCommandInvocationOutput{
		Status:                aws.String("Success"),
		StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateAvailable, "v1.0.5")),
	}

	for i := 0; i < 100; i++ {
		containerID := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, instance{
			instanceID:          ec2ID,
			containerInstanceID: containerID,
		})
		expected = append(expected, instance{
			instanceID:          ec2ID,
			containerInstanceID: containerID,
			bottlerocketVersion: "v1.0.5",
		})
	}

	pageErrors := []error{errors.New("Failed to send document"), nil}

	// mutex needed to prevent race condition when incrementing counter in concurrent
	// execution of WaitUntilCommandExecutedWithContextFn
	var m sync.Mutex
	sendCommandCalls := 0
	commandWaiterCalls := 0
	getCommandInvocationCalls := 0
	callCount := 0
	mockSSM := MockSSM{
		GetCommandInvocationFn: func(_ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			getCommandInvocationCalls++
			return getOut, nil
		},
		SendCommandFn: func(_ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			require.Less(t, callCount, len(pageErrors))
			failErr := pageErrors[callCount]
			callCount++
			sendCommandCalls++
			return &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId:    aws.String("command-id"),
					DocumentName: aws.String("check-document"),
				},
			}, failErr
		},
		WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
			assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
			m.Lock()
			commandWaiterCalls++
			m.Unlock()
			return nil
		},
	}
	u := updater{ssm: mockSSM}
	actual, err := u.filterAvailableUpdates(instances)

	require.NoError(t, err)
	assert.EqualValues(t, actual, expected[50:], "Should only contain instances from the 2nd page")
	assert.Equal(t, 2, sendCommandCalls, "should send commands for each page")
	assert.Equal(t, 50, commandWaiterCalls, "should wait for each instance")
	assert.Equal(t, 50, getCommandInvocationCalls, "should collect output for each instance")
}

func TestGetCommandResult(t *testing.T) {
	cases := []struct {
		name            string
		invocationOut   *ssm.GetCommandInvocationOutput
		expectedError   string
		expectedOut     []byte
		invocationError error
	}{
		{
			name: "getCommand success",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String("OutputContent"),
			},
			expectedOut: []byte(aws.StringValue(aws.String("OutputContent"))),
		},
		{
			name:            "getCommand fail",
			invocationError: errors.New("failed to get command invocation"),
			expectedError:   "failed to retrieve command invocation output: failed to get command invocation",
			invocationOut:   nil,
			expectedOut:     nil,
		},
		{
			name: "command status non-Success",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("TimedOut"),
				StandardOutputContent: nil,
			},
			expectedError: "command command-id has not reached success status, current status \"TimedOut\"",
			expectedOut:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockSSM := MockSSM{
				GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					return tc.invocationOut, tc.invocationError
				},
			}
			u := updater{ssm: mockSSM}
			actual, err := u.getCommandResult("command-id", "instance-id")
			if tc.expectedOut != nil {
				require.NoError(t, err)
				assert.EqualValues(t, tc.expectedOut, actual)
			} else {
				require.Error(t, err)
				assert.EqualError(t, err, tc.expectedError)
			}
		})
	}
}

func TestSendCommandSuccess(t *testing.T) {
	instances := []string{"inst-id-1", "inst-id-2"}
	// mutex needed to prevent race condition when appending to instances slice in concurrent
	// execution of WaitUntilCommandExecutedWithContextFn
	var m sync.Mutex
	waitInstanceIDs := []string{}
	mockSSM := MockSSM{
		SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			assert.Equal(t, "test-doc", aws.StringValue(input.DocumentName))
			assert.Equal(t, "$DEFAULT", aws.StringValue(input.DocumentVersion))
			assert.Equal(t, aws.StringSlice(instances), input.InstanceIds)
			return &ssm.SendCommandOutput{Command: &ssm.Command{CommandId: aws.String("command-id")}}, nil
		},
		WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
			assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
			m.Lock()
			waitInstanceIDs = append(waitInstanceIDs, aws.StringValue(input.InstanceId))
			m.Unlock()
			return nil
		},
	}
	u := updater{ssm: mockSSM}
	commandID, err := u.sendCommand(instances, "test-doc")
	require.NoError(t, err)
	assert.EqualValues(t, "command-id", commandID)
	assert.ElementsMatch(t, instances, waitInstanceIDs)
}

func TestSendCommandErr(t *testing.T) {
	instances := []string{"inst-id-1", "inst-id-2"}
	sendError := errors.New("failed to send command")
	mockSSM := MockSSM{
		SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			assert.Equal(t, "test-doc", aws.StringValue(input.DocumentName))
			assert.Equal(t, "$DEFAULT", aws.StringValue(input.DocumentVersion))
			assert.Equal(t, aws.StringSlice(instances), input.InstanceIds)
			return nil, sendError
		},
	}
	u := updater{ssm: mockSSM}
	commandID, err := u.sendCommand(instances, "test-doc")
	require.Error(t, err)
	assert.Equal(t, "", commandID)
	assert.ErrorIs(t, err, sendError)

}

func TestSendCommandWaitErr(t *testing.T) {
	cases := []struct {
		name      string
		instances []string
	}{
		{
			name:      "wait single failure",
			instances: []string{"inst-id-1"},
		},
		{
			name:      "wait fail all",
			instances: []string{"inst-id-1", "inst-id-2", "inst-id-3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			waitError := errors.New("exceeded max attempts")
			failedInstanceIDs := []string{}
			mockSSM := MockSSM{
				SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					assert.Equal(t, "test-doc", aws.StringValue(input.DocumentName))
					assert.Equal(t, aws.StringSlice(tc.instances), input.InstanceIds)
					return &ssm.SendCommandOutput{
						Command: &ssm.Command{CommandId: aws.String("command-id")},
					}, nil
				},
				WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					return waitError
				},
				GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					failedInstanceIDs = append(failedInstanceIDs, aws.StringValue(input.InstanceId))
					return &ssm.GetCommandInvocationOutput{}, nil
				},
			}
			u := updater{ssm: mockSSM}
			commandID, err := u.sendCommand(tc.instances, "test-doc")
			require.Error(t, err)
			assert.ErrorIs(t, err, waitError)
			assert.Equal(t, "", commandID)
			assert.ElementsMatch(t, tc.instances, failedInstanceIDs, "should match instances for which wait fails")
		})
	}
}

func TestSendCommandWaitSuccess(t *testing.T) {
	mockSendCommand := func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
		assert.Equal(t, "test-doc", aws.StringValue(input.DocumentName))
		return &ssm.SendCommandOutput{
			Command: &ssm.Command{CommandId: aws.String("command-id")},
		}, nil
	}
	t.Run("wait one success", func(t *testing.T) {
		// commandSuccessInstance indicates an instance for which the command should succeed
		const commandSuccessInstance = "inst-success"
		instances := []string{"inst-id-1", "inst-id-2", commandSuccessInstance}
		expectedFailInstances := []string{"inst-id-1", "inst-id-2"}
		failedInstanceIDs := []string{}
		mockSSM := MockSSM{
			SendCommandFn: mockSendCommand,
			WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
				if aws.StringValue(input.InstanceId) == commandSuccessInstance {
					return nil
				}
				return errors.New("exceeded max attempts")
			},
			GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				failedInstanceIDs = append(failedInstanceIDs, aws.StringValue(input.InstanceId))
				return &ssm.GetCommandInvocationOutput{}, nil
			},
		}
		u := updater{ssm: mockSSM}
		commandID, err := u.sendCommand(instances, "test-doc")
		require.NoError(t, err)
		assert.Equal(t, "command-id", commandID)
		assert.ElementsMatch(t, expectedFailInstances, failedInstanceIDs, "should match instances for which wait fails")
	})
	t.Run("wait all success", func(t *testing.T) {
		instances := []string{"inst-id-1", "inst-id-2"}
		// mutex needed to prevent race condition when appending to instances slice in concurrent
		// execution of WaitUntilCommandExecutedWithContextFn
		var m sync.Mutex
		waitInstanceIDs := []string{}
		mockSSM := MockSSM{
			SendCommandFn: mockSendCommand,
			WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				m.Lock()
				waitInstanceIDs = append(waitInstanceIDs, aws.StringValue(input.InstanceId))
				m.Unlock()
				return nil
			},
		}
		u := updater{ssm: mockSSM}
		commandID, err := u.sendCommand(instances, "test-doc")
		require.NoError(t, err)
		assert.Equal(t, "command-id", commandID)
		assert.ElementsMatch(t, instances, waitInstanceIDs, "should match instances for which wait succeeds")
	})

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
		},
		{
			name: "without instances",
			listOutput: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
			},
			listOutput2: &ecs.ListContainerInstancesOutput{
				ContainerInstanceArns: []*string{},
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
			expectedError: "failed to list container instances",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockECS := MockECS{
				ListContainerInstancesPagesFn: func(input *ecs.ListContainerInstancesInput, fn func(*ecs.ListContainerInstancesOutput, bool) bool) error {
					assert.Equal(t, ecs.ContainerInstanceStatusActive, aws.StringValue(input.Status))
					fn(tc.listOutput, true)
					fn(tc.listOutput2, false)
					return tc.listError
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

func TestPaginatedFilterBottlerocketInstancesAllFail(t *testing.T) {
	instances := make([]*string, 0)
	for i := 0; i < 150; i++ {
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, aws.String(ec2ID))
	}

	responses := []struct {
		inputLen           int
		ContainerInstances []*ecs.ContainerInstance
		err                error
	}{{
		100,
		nil,
		errors.New("Failed to describe container instances"),
	}, {
		50,
		nil,
		errors.New("Failed to describe container instances"),
	}}

	callCount := 0
	mockECS := MockECS{
		DescribeContainerInstancesFn: func(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error) {
			require.Less(t, callCount, len(responses))
			resp := responses[callCount]
			callCount++
			assert.Equal(t, resp.inputLen, len(input.ContainerInstances))
			return &ecs.DescribeContainerInstancesOutput{ContainerInstances: resp.ContainerInstances}, resp.err
		},
	}

	u := updater{ecs: mockECS}
	actual, err := u.filterBottlerocketInstances(instances)
	require.Error(t, err)
	assert.Empty(t, actual)
	assert.Contains(t, err.Error(), "Failed to describe container instances")
}

func TestPaginatedFilterBottlerocketInstancesSingleFailure(t *testing.T) {
	descOut := make([]*ecs.ContainerInstance, 0)
	instances := make([]*string, 0)
	expected := make([]instance, 0)
	for i := 0; i < 150; i++ {
		instanceARN := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, aws.String(ec2ID))
		descOut = append(descOut, &ecs.ContainerInstance{
			Attributes:           []*ecs.Attribute{{Name: aws.String("bottlerocket.variant")}},
			ContainerInstanceArn: aws.String(instanceARN),
			Ec2InstanceId:        aws.String(ec2ID),
		})
		expected = append(expected, instance{
			instanceID:          ec2ID,
			containerInstanceID: instanceARN,
		})
	}

	responses := []struct {
		inputLen           int
		ContainerInstances []*ecs.ContainerInstance
		err                error
	}{{
		100,
		nil,
		errors.New("Failed to describe container instances"),
	}, {
		50,
		descOut[100:],
		nil,
	}}

	callCount := 0
	mockECS := MockECS{
		DescribeContainerInstancesFn: func(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error) {
			require.Less(t, callCount, len(responses))
			resp := responses[callCount]
			callCount++
			assert.Equal(t, resp.inputLen, len(input.ContainerInstances))
			return &ecs.DescribeContainerInstancesOutput{ContainerInstances: resp.ContainerInstances}, resp.err
		},
	}

	u := updater{ecs: mockECS}
	actual, err := u.filterBottlerocketInstances(instances)
	require.NoError(t, err)
	assert.EqualValues(t, expected[100:], actual, "should contain only the last 50 instnaces")
}

func TestPaginatedFilterBottlerocketInstancesNoBR(t *testing.T) {
	descOut := make([]*ecs.ContainerInstance, 0)
	instances := make([]*string, 0)
	for i := 0; i < 150; i++ {
		instanceARN := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, aws.String(ec2ID))
		descOut = append(descOut, &ecs.ContainerInstance{
			Attributes:           []*ecs.Attribute{{Name: aws.String("nottlerocket.variant")}},
			ContainerInstanceArn: aws.String(instanceARN),
			Ec2InstanceId:        aws.String(ec2ID),
		})
	}

	responses := []struct {
		inputLen           int
		ContainerInstances []*ecs.ContainerInstance
		err                error
	}{{
		100,
		descOut[:100],
		nil,
	}, {
		50,
		descOut[100:],
		nil,
	}}

	callCount := 0
	mockECS := MockECS{
		DescribeContainerInstancesFn: func(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error) {
			require.Less(t, callCount, len(responses))
			resp := responses[callCount]
			callCount++
			assert.Equal(t, resp.inputLen, len(input.ContainerInstances))
			return &ecs.DescribeContainerInstancesOutput{ContainerInstances: resp.ContainerInstances}, resp.err
		},
	}

	u := updater{ecs: mockECS}
	actual, err := u.filterBottlerocketInstances(instances)
	require.NoError(t, err)
	assert.Empty(t, actual)
}

func TestPaginatedFilterBottlerocketInstancesAllBRInstances(t *testing.T) {
	descOut := make([]*ecs.ContainerInstance, 0)
	instances := make([]*string, 0)
	expected := make([]instance, 0)
	for i := 0; i < 150; i++ {
		instanceARN := "cont-inst-br" + strconv.Itoa(i)
		ec2ID := "ec2-id-br" + strconv.Itoa(i)
		instances = append(instances, aws.String(ec2ID))
		descOut = append(descOut, &ecs.ContainerInstance{
			Attributes:           []*ecs.Attribute{{Name: aws.String("bottlerocket.variant")}},
			ContainerInstanceArn: aws.String(instanceARN),
			Ec2InstanceId:        aws.String(ec2ID),
		})
		expected = append(expected, instance{
			instanceID:          ec2ID,
			containerInstanceID: instanceARN,
		})
	}

	responses := []struct {
		inputLen           int
		ContainerInstances []*ecs.ContainerInstance
		err                error
	}{{
		100,
		descOut[:100],
		nil,
	}, {
		50,
		descOut[100:],
		nil,
	}}

	callCount := 0
	mockECS := MockECS{
		DescribeContainerInstancesFn: func(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error) {
			require.Less(t, callCount, len(responses))
			resp := responses[callCount]
			callCount++
			assert.Equal(t, resp.inputLen, len(input.ContainerInstances))
			return &ecs.DescribeContainerInstancesOutput{ContainerInstances: resp.ContainerInstances}, resp.err
		},
	}

	u := updater{ecs: mockECS}
	actual, err := u.filterBottlerocketInstances(instances)
	require.NoError(t, err)
	assert.EqualValues(t, expected, actual, "should contain all the instances")
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
			WaitUntilTasksStoppedWithContextFn: func(_ aws.Context, input *ecs.DescribeTasksInput, _ ...request.WaiterOption) error {
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
			WaitUntilTasksStoppedWithContextFn: func(_ aws.Context, input *ecs.DescribeTasksInput, _ ...request.WaiterOption) error {
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

func TestUpdateInstance(t *testing.T) {
	checkPattern := "{\"update_state\": \"%s\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"
	cases := []struct {
		name                        string
		invocationOut               *ssm.GetCommandInvocationOutput
		expectedSSMCommandCallOrder []string
		expectedErr                 string
	}{
		{
			name: "update state available",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateAvailable)),
			},
			expectedSSMCommandCallOrder: []string{"check-document", "apply-document", "reboot-document"},
		}, {
			name: "update state ready",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateReady)),
			},
			expectedSSMCommandCallOrder: []string{"check-document", "reboot-document"},
		}, {
			name: "update state idle",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateIdle)),
			},
			expectedSSMCommandCallOrder: []string{"check-document"},
		}, {
			name: "update state staged",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateStaged)),
			},
			expectedSSMCommandCallOrder: []string{"check-document"},
			expectedErr:                 "unexpected update state \"Staged\"; skipping instance",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ssmCommandCallOrder := []string{}
			mockSSM := MockSSM{
				SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					ssmCommandCallOrder = append(ssmCommandCallOrder, aws.StringValue(input.DocumentName))
					assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
					return &ssm.SendCommandOutput{
						Command: &ssm.Command{
							CommandId: aws.String("command-id"),
						},
					}, nil
				},
				GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					return tc.invocationOut, nil
				},
				WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					return nil
				},
			}
			mockEC2 := MockEC2{
				WaitUntilInstanceStatusOkFn: func(input *ec2.DescribeInstanceStatusInput) error {
					assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
					return nil
				},
			}
			u := updater{ssm: mockSSM, ec2: mockEC2, checkDocument: "check-document", applyDocument: "apply-document", rebootDocument: "reboot-document"}
			err := u.updateInstance(instance{
				instanceID:          "instance-id",
				containerInstanceID: "cont-inst-id",
				bottlerocketVersion: "v0.1.0",
			})
			if tc.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.expectedSSMCommandCallOrder, ssmCommandCallOrder)
		})
	}
}

func TestUpdateInstanceErr(t *testing.T) {
	commandOutput := &ssm.SendCommandOutput{
		Command: &ssm.Command{
			CommandId: aws.String("command-id"),
		},
	}
	mockSendCommand := func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
		assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
		return commandOutput, nil
	}
	mockGetCommandInvocation := func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
		assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
		assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
		return &ssm.GetCommandInvocationOutput{
			Status:                aws.String("Success"),
			StandardOutputContent: aws.String("{\"update_state\": \"Available\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
		}, nil
	}
	mockWaitCommandExecution := func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
		assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
		assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
		return nil
	}

	t.Run("check err", func(t *testing.T) {
		checkErr := errors.New("failed to send check command")
		mockSSM := MockSSM{
			SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
				assert.Equal(t, "check-document", aws.StringValue(input.DocumentName))
				assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
				return nil, checkErr
			},
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		err := u.updateInstance(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, checkErr)
	})
	t.Run("apply err", func(t *testing.T) {
		applyErr := errors.New("failed to send apply command")
		mockSSM := MockSSM{
			SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
				assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
				if aws.StringValue(input.DocumentName) == "apply-document" {
					return nil, applyErr
				}
				return commandOutput, nil
			},
			GetCommandInvocationFn:                mockGetCommandInvocation,
			WaitUntilCommandExecutedWithContextFn: mockWaitCommandExecution,
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document", applyDocument: "apply-document"}
		err := u.updateInstance(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, applyErr)
	})
	t.Run("reboot err", func(t *testing.T) {
		rebootErr := errors.New("failed to send reboot command")
		mockSSM := MockSSM{
			SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
				assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
				if aws.StringValue(input.DocumentName) == "reboot-document" {
					return nil, rebootErr
				}
				return commandOutput, nil
			},
			GetCommandInvocationFn:                mockGetCommandInvocation,
			WaitUntilCommandExecutedWithContextFn: mockWaitCommandExecution,
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document", applyDocument: "apply-document", rebootDocument: "reboot-document"}
		err := u.updateInstance(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, rebootErr)
	})
	t.Run("invocation err", func(t *testing.T) {
		ssmGetInvocationErr := errors.New("failed to get command invocation")
		mockSSM := MockSSM{
			SendCommandFn: mockSendCommand,
			GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
				return nil, ssmGetInvocationErr
			},
			WaitUntilCommandExecutedWithContextFn: mockWaitCommandExecution,
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		err := u.updateInstance(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ssmGetInvocationErr)
	})
	t.Run("wait ssm err", func(t *testing.T) {
		waitExecErr := errors.New("failed to wait ssm execution complete")
		mockSSM := MockSSM{
			SendCommandFn: mockSendCommand,
			WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
				return waitExecErr
			},
			GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
				return &ssm.GetCommandInvocationOutput{}, nil
			},
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		err := u.updateInstance(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, waitExecErr)
	})
	t.Run("wait instance ok err", func(t *testing.T) {
		waitErr := errors.New("failed to wait instance ok")
		mockSSM := MockSSM{
			SendCommandFn:                         mockSendCommand,
			GetCommandInvocationFn:                mockGetCommandInvocation,
			WaitUntilCommandExecutedWithContextFn: mockWaitCommandExecution,
		}

		mockEC2 := MockEC2{
			WaitUntilInstanceStatusOkFn: func(input *ec2.DescribeInstanceStatusInput) error {
				assert.Equal(t, []*string{aws.String("instance-id")}, input.InstanceIds)
				return waitErr
			},
		}
		u := updater{ssm: mockSSM, ec2: mockEC2, checkDocument: "check-document", applyDocument: "apply-document", rebootDocument: "reboot-document"}
		err := u.updateInstance(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, waitErr)
	})
}

func TestVerifyUpdate(t *testing.T) {
	checkPattern := "{\"update_state\": \"%s\", \"active_partition\": { \"image\": { \"version\": \"%s\"}}}"
	cases := []struct {
		name          string
		invocationOut *ssm.GetCommandInvocationOutput
		expectedOk    bool
	}{
		{
			name: "verify success",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateIdle, "0.0.1")),
			},
			expectedOk: true,
		},
		{
			name: "version is same",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateIdle, "0.0.0")),
			},
			expectedOk: false,
		},
		{
			name: "another version is available",
			invocationOut: &ssm.GetCommandInvocationOutput{
				Status:                aws.String("Success"),
				StandardOutputContent: aws.String(fmt.Sprintf(checkPattern, updateStateAvailable, "0.0.1")),
			},
			expectedOk: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockSSM := MockSSM{
				SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					assert.Equal(t, "check-document", aws.StringValue(input.DocumentName))
					return &ssm.SendCommandOutput{
						Command: &ssm.Command{
							CommandId: aws.String("command-id"),
						},
					}, nil
				},
				GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					return tc.invocationOut, nil
				},
				WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					return nil
				},
			}
			u := updater{ssm: mockSSM, checkDocument: "check-document"}
			ok, err := u.verifyUpdate(instance{
				instanceID:          "instance-id",
				containerInstanceID: "cont-inst-id",
				bottlerocketVersion: "0.0.0",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.expectedOk, ok)
		})
	}
}

func TestVerifyUpdateErr(t *testing.T) {
	mockSSMCommandOut := func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
		assert.Equal(t, "check-document", aws.StringValue(input.DocumentName))
		assert.Equal(t, 1, len(input.InstanceIds))
		assert.Equal(t, "instance-id", aws.StringValue(input.InstanceIds[0]))
		return &ssm.SendCommandOutput{
			Command: &ssm.Command{
				CommandId: aws.String("command-id"),
			},
		}, nil
	}
	mockWaitCommandExecution := func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
		assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
		assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
		return nil
	}
	mockGetCommandInvocation := func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
		assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
		assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
		return &ssm.GetCommandInvocationOutput{
			Status: aws.String("Success"),
		}, nil
	}
	t.Run("check err", func(t *testing.T) {
		ssmCheckErr := errors.New("failed to send check command")
		mockSSM := MockSSM{
			SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
				assert.Equal(t, "check-document", aws.StringValue(input.DocumentName))
				assert.Equal(t, 1, len(input.InstanceIds))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceIds[0]))
				return nil, ssmCheckErr
			},
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		ok, err := u.verifyUpdate(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
			bottlerocketVersion: "0.0.0",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ssmCheckErr)
		assert.False(t, ok)
	})
	t.Run("wait ssm err", func(t *testing.T) {
		waitExecErr := errors.New("failed to wait ssm execution complete")
		mockSSM := MockSSM{
			SendCommandFn: mockSSMCommandOut,
			WaitUntilCommandExecutedWithContextFn: func(_ aws.Context, input *ssm.GetCommandInvocationInput, _ ...request.WaiterOption) error {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
				return waitExecErr
			},
			GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
				return &ssm.GetCommandInvocationOutput{}, nil
			},
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		ok, err := u.verifyUpdate(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
			bottlerocketVersion: "0.0.0",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, waitExecErr)
		assert.False(t, ok)
	})
	t.Run("invocation err", func(t *testing.T) {
		ssmGetInvocationErr := errors.New("failed to get command invocation")
		mockSSM := MockSSM{
			SendCommandFn:                         mockSSMCommandOut,
			WaitUntilCommandExecutedWithContextFn: mockWaitCommandExecution,
			GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
				assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
				assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
				return nil, ssmGetInvocationErr
			},
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		ok, err := u.verifyUpdate(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
			bottlerocketVersion: "0.0.0",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ssmGetInvocationErr)
		assert.False(t, ok)
	})

	t.Run("parse output err", func(t *testing.T) {
		mockSSM := MockSSM{
			SendCommandFn:                         mockSSMCommandOut,
			WaitUntilCommandExecutedWithContextFn: mockWaitCommandExecution,
			GetCommandInvocationFn:                mockGetCommandInvocation,
		}
		u := updater{ssm: mockSSM, checkDocument: "check-document"}
		ok, err := u.verifyUpdate(instance{
			instanceID:          "instance-id",
			containerInstanceID: "cont-inst-id",
			bottlerocketVersion: "0.0.0",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `failed to parse command output "", manual verification required`)
		assert.False(t, ok)
	})
}

func TestActivateInstance(t *testing.T) {
	cases := []struct {
		name        string
		stateOut    *ecs.UpdateContainerInstancesStateOutput
		stateErr    error
		expectedErr string
	}{
		{
			name:     "activate success",
			stateOut: &ecs.UpdateContainerInstancesStateOutput{},
		}, {
			name: "activate api fail",
			stateOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{
					{
						Reason: aws.String("OTHER"),
					},
				},
			},
			expectedErr: "API failures while activating: [{\n  Reason: \"OTHER\"\n}]",
		},
		{
			name: "activate api fail inactive",
			stateOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{
					{
						Reason: aws.String("INACTIVE"),
					},
				},
			},
		},
		{
			name:        "activate failure",
			stateErr:    errors.New("failed to activate"),
			expectedErr: "failed to activate",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockECS := MockECS{
				UpdateContainerInstancesStateFn: func(_ *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error) {
					return tc.stateOut, tc.stateErr
				},
			}
			u := updater{ecs: mockECS}
			err := u.activateInstance("cont-inst-id")
			if tc.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
		})
	}
}

func TestAlreadyRunning(t *testing.T) {
	cases := []struct {
		name        string
		listOut     *ecs.ListTasksOutput
		listErr     error
		expectedOk  bool
		expectedErr string
	}{
		{
			name: "success",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
					aws.String("task-arn-2"),
				},
			},
			expectedOk: true,
		},
		{
			name: "only one task",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("tarsk-arn-1"),
				},
			},
			expectedOk: false,
		},
		{
			name:        "fail list task",
			listErr:     errors.New("failed to list task"),
			expectedOk:  false,
			expectedErr: "failed to list task",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockECS := MockECS{
				ListTasksFn: func(_ *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
					return tc.listOut, tc.listErr
				},
			}
			u := updater{ecs: mockECS, cluster: "ecs-cluster"}
			ok, err := u.alreadyRunning("updater-family")
			if tc.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
			assert.Equal(t, tc.expectedOk, ok)
		})
	}
}

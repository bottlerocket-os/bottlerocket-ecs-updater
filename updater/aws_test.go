package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestEligible(t *testing.T) {
	cases := []struct {
		name                  string
		listOut               *ecs.ListTasksOutput
		listErr               error
		describeOut           *ecs.DescribeTasksOutput
		describeErr           error
		expectedErr           string
		expectedListCount     int
		expectedDescribeCount int
		expectedOk            bool
	}{
		{
			name: "with task success",
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
			expectedListCount:     1,
			expectedDescribeCount: 1,
			expectedOk:            true,
		}, {
			name: "no task success",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{},
			},
			expectedListCount:     1,
			expectedDescribeCount: 0,
			expectedOk:            true,
		}, {
			name: "not eligible",
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
			expectedListCount:     1,
			expectedDescribeCount: 1,
			expectedOk:            false,
		},
		{
			name:                  "list task fail",
			listErr:               errors.New("list task failed"),
			expectedErr:           "failed to list tasks",
			expectedListCount:     1,
			expectedDescribeCount: 0,
			expectedOk:            false,
		},
		{
			name: "describe task fail",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
				},
			},
			describeErr:           errors.New("describe task failed"),
			expectedErr:           "could not describe tasks",
			expectedListCount:     1,
			expectedDescribeCount: 1,
			expectedOk:            false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listCount := 0
			describeCount := 0
			mockECS := MockECS{
				ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
					listCount++
					return tc.listOut, tc.listErr
				},
				DescribeTasksFn: func(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error) {
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					assert.Equal(t, []*string{
						aws.String("task-arn-1"),
					}, input.Tasks)
					describeCount++
					return tc.describeOut, tc.describeErr
				},
			}
			u := updater{ecs: mockECS, cluster: "test-cluster"}
			ok, err := u.eligible("cont-inst-id")
			if tc.expectedErr != "" {
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
			switch {
			case tc.listErr != nil:
				assert.ErrorIs(t, err, tc.listErr)
			case tc.describeErr != nil:
				assert.ErrorIs(t, err, tc.describeErr)
			case tc.expectedErr == "":
				require.NoError(t, err)
			}
			assert.Equal(t, tc.expectedListCount, listCount, "should match ListTasks call count")
			assert.Equal(t, tc.expectedDescribeCount, describeCount, "should match DescribeTasks call count")
			assert.Equal(t, ok, tc.expectedOk, "should get expected eligibility value")
		})
	}
}

func TestDrainInstance(t *testing.T) {
	cases := []struct {
		name               string
		stateChangeOut     *ecs.UpdateContainerInstancesStateOutput
		stateChangeErr     error
		listTaskOut        *ecs.ListTasksOutput
		listTaskErr        error
		waitTaskErr        error
		expectedErr        string
		expectedStateCount int
		expectedListCount  int
		expectedWaitCount  int
	}{
		{
			name: "no task",
			stateChangeOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{},
			},
			listTaskOut: &ecs.ListTasksOutput{
				TaskArns: []*string{},
			},
			expectedStateCount: 1,
			expectedListCount:  1,
			expectedWaitCount:  0,
		},
		{
			name: "with task",
			stateChangeOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{},
			},
			listTaskOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
				},
			},
			expectedStateCount: 1,
			expectedListCount:  1,
			expectedWaitCount:  1,
		},
		{
			name:           "drain state change fail",
			stateChangeErr: errors.New("state change error"),
			listTaskOut: &ecs.ListTasksOutput{
				TaskArns: []*string{},
			},
			expectedErr:        "failed to change instance state to DRAINING",
			expectedStateCount: 1,
			expectedListCount:  0,
			expectedWaitCount:  0,
		},
		{
			name: "state change api fail",
			stateChangeOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{
					{
						Reason: aws.String("failed"),
					},
				},
			},
			listTaskOut: &ecs.ListTasksOutput{
				TaskArns: []*string{},
			},
			expectedErr:        "failures in API call",
			expectedStateCount: 2,
			expectedListCount:  0,
			expectedWaitCount:  0,
		},
		{
			name: "list task fail",
			stateChangeOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{},
			},
			listTaskErr:        errors.New("list task error"),
			expectedErr:        "error while waiting to drain: failed to list tasks",
			expectedStateCount: 2,
			expectedListCount:  1,
			expectedWaitCount:  0,
		},
		{
			name: "wait task stop fail",
			stateChangeOut: &ecs.UpdateContainerInstancesStateOutput{
				Failures: []*ecs.Failure{},
			},
			listTaskOut: &ecs.ListTasksOutput{
				TaskArns: []*string{
					aws.String("task-arn-1"),
				},
			},
			waitTaskErr:        errors.New("wait error"),
			expectedErr:        "error while waiting to drain",
			expectedStateCount: 2,
			expectedListCount:  1,
			expectedWaitCount:  1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stateCount := 0
			listCount := 0
			waitCount := 0
			mockECS := MockECS{
				UpdateContainerInstancesStateFn: func(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error) {
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					assert.Equal(t, []*string{aws.String("cont-inst-id")}, input.ContainerInstances)
					stateCount++
					return tc.stateChangeOut, tc.stateChangeErr
				},
				ListTasksFn: func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					assert.Equal(t, "cont-inst-id", aws.StringValue(input.ContainerInstance))
					listCount++
					return tc.listTaskOut, tc.listTaskErr
				},
				WaitUntilTasksStoppedWithContextFn: func(ctx aws.Context, input *ecs.DescribeTasksInput, opts ...request.WaiterOption) error {
					assert.Equal(t, []*string{
						aws.String("task-arn-1"),
					}, input.Tasks)
					assert.Equal(t, "test-cluster", aws.StringValue(input.Cluster))
					waitCount++
					return tc.waitTaskErr
				},
			}
			u := updater{ecs: mockECS, cluster: "test-cluster"}
			err := u.drainInstance("cont-inst-id")
			if tc.expectedErr != "" {
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
			switch {
			case tc.stateChangeErr != nil:
				assert.ErrorIs(t, err, tc.stateChangeErr)
			case len(tc.stateChangeOut.Failures) != 0:
				assert.Contains(t, err.Error(), fmt.Sprintf("%v", tc.stateChangeOut.Failures))
			case tc.listTaskErr != nil:
				assert.ErrorIs(t, err, tc.listTaskErr)
			case tc.waitTaskErr != nil:
				assert.ErrorIs(t, err, tc.waitTaskErr)
			case tc.expectedErr == "":
				require.NoError(t, err)
			}
			assert.Equal(t, tc.expectedStateCount, stateCount, "should match UpdateContainerInstancesState call count")
			assert.Equal(t, tc.expectedListCount, listCount, "should match ListTasks call count")
			assert.Equal(t, tc.expectedWaitCount, waitCount, "should match WaitUntilTasksStopped call count")
		})
	}

}

func TestUpdateInstance(t *testing.T) {
	cases := []struct {
		name                    string
		checkCmdOut             *ssm.SendCommandOutput
		checkCmdErr             error
		applyCmdOut             *ssm.SendCommandOutput
		applyCmdErr             error
		rebootCmdOut            *ssm.SendCommandOutput
		rebootCmdErr            error
		invocationOut           *ssm.GetCommandInvocationOutput
		invocationErr           error
		waitCmdErr              error
		waitInstanceOkErr       error
		expectedErr             string
		expectedSendCount       int
		expectedInvocationCount int
		expectedWaitCount       int
	}{
		{
			name: "update available",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Available\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			applyCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			rebootCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("cmd-reboot"),
				},
			},
			expectedSendCount:       3,
			expectedInvocationCount: 1,
			expectedWaitCount:       2,
		}, {
			name: "update already ready",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Ready\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			rebootCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("cmd-reboot"),
				},
			},
			expectedSendCount:       2,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
		}, {
			name: "no updates",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Idle\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
		}, {
			name: "update already staged",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Staged\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			expectedErr:             "unexpected update state \"Staged\"; skipping instance",
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
		}, {
			name:                    "check command failure",
			checkCmdErr:             errors.New("command failed"),
			expectedErr:             "failed to send check command: send command failed",
			expectedSendCount:       1,
			expectedInvocationCount: 0,
			expectedWaitCount:       0,
		}, {
			name: "wait command complete failure",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			waitCmdErr:              errors.New("wait timed out"),
			expectedErr:             "failed to send check command: send command failed",
			expectedSendCount:       1,
			expectedInvocationCount: 0,
			expectedWaitCount:       1,
		},
		{
			name: "apply command failure",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Available\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			applyCmdErr:             errors.New("command failed"),
			expectedErr:             "failed to send update apply command: send command failed",
			expectedSendCount:       2,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
		},
		{
			name: "reboot command failure",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Available\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			applyCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			rebootCmdErr:            errors.New("command failed"),
			expectedErr:             "failed to send reboot command",
			expectedSendCount:       3,
			expectedInvocationCount: 1,
			expectedWaitCount:       2,
		},
		{
			name: "check invocation failure",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationErr:           errors.New("failed to get result"),
			expectedErr:             "failed to get check command output: failed to retrieve command invocation output",
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
		},
		{
			name: "wait instance ok failure",
			checkCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Available\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			applyCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			rebootCmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("cmd-reboot"),
				},
			},
			waitInstanceOkErr:       errors.New("failed to wait until instance ok"),
			expectedErr:             "failed to reach Ok status after reboot",
			expectedSendCount:       3,
			expectedInvocationCount: 1,
			expectedWaitCount:       2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "wait command complete failure" {
				t.Skip("skip until error from WaitUntilCommandExecuted is handled")
			}
			sendCount := 0
			invocationCount := 0
			waitCount := 0
			mockSSM := MockSSM{
				SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					sendCount++
					if aws.StringValue(input.DocumentName) == "check-document" {
						return tc.checkCmdOut, tc.checkCmdErr
					} else if aws.StringValue(input.DocumentName) == "apply-document" {
						return tc.applyCmdOut, tc.applyCmdErr
					} else if aws.StringValue(input.DocumentName) == "reboot-document" {
						return tc.rebootCmdOut, tc.rebootCmdErr
					} else {
						t.Fatal("found un-expected SSM document name")
						return nil, nil
					}
				},
				GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					invocationCount++
					return tc.invocationOut, tc.invocationErr
				},
				WaitUntilCommandExecutedWithContextFn: func(ctx aws.Context, input *ssm.GetCommandInvocationInput, opts ...request.WaiterOption) error {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					waitCount++
					return tc.waitCmdErr
				},
			}
			mockEC2 := MockEC2{
				WaitUntilInstanceStatusOkFn: func(input *ec2.DescribeInstanceStatusInput) error {
					return tc.waitInstanceOkErr
				},
			}
			u := updater{ssm: mockSSM, ec2: mockEC2, checkDocument: "check-document", applyDocument: "apply-document", rebootDocument: "reboot-document"}
			err := u.updateInstance(instance{
				instanceID:          "instance-id",
				containerInstanceID: "cont-inst-id",
				bottlerocketVersion: "v0.1.0",
			})
			if tc.expectedErr != "" {
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
			switch {
			case tc.checkCmdErr != nil:
				assert.ErrorIs(t, err, tc.checkCmdErr)
			case tc.applyCmdErr != nil:
				assert.ErrorIs(t, err, tc.applyCmdErr)
			case tc.rebootCmdErr != nil:
				assert.ErrorIs(t, err, tc.rebootCmdErr)
			case tc.invocationErr != nil:
				assert.ErrorIs(t, err, tc.invocationErr)
			case tc.waitInstanceOkErr != nil:
				assert.ErrorIs(t, err, tc.waitInstanceOkErr)
			case tc.expectedErr == "":
				require.NoError(t, err)
			}
			assert.Equal(t, tc.expectedSendCount, sendCount, "should match SendCommand call count")
			assert.Equal(t, tc.expectedInvocationCount, invocationCount, "should match GetCommandInvocation call count")
			assert.Equal(t, tc.expectedWaitCount, waitCount, "should match WaitUntilCommandExecuted call count")
		})
	}

}

func TestVerifyUpdate(t *testing.T) {
	cases := []struct {
		name                    string
		cmdOut                  *ssm.SendCommandOutput
		cmdErr                  error
		invocationOut           *ssm.GetCommandInvocationOutput
		invocationErr           error
		waitCommandErr          error
		expectedErr             string
		expectedSendCount       int
		expectedInvocationCount int
		expectedWaitCount       int
		expectedOk              bool
	}{
		{
			name: "verify success",
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"idle\", \"active_partition\": { \"image\": { \"version\": \"0.0.1\"}}}"),
			},
			cmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
			expectedOk:              true,
		}, {
			name:                    "check command failure",
			cmdErr:                  errors.New("check api command failed"),
			expectedErr:             "failed to send update check command: send command failed",
			expectedSendCount:       1,
			expectedInvocationCount: 0,
			expectedWaitCount:       0,
			expectedOk:              false,
		},
		{
			name: "invocation failure",
			cmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationErr:           errors.New("failed to get check update result"),
			expectedOk:              false,
			expectedErr:             "failed to get check command output: failed to retrieve command invocation output",
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
		}, {
			name: "wait command failure",
			cmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			waitCommandErr:          errors.New("failed to wait for command complete"),
			expectedOk:              false,
			expectedErr:             "failed to get check command output: failed to retrieve command invocation output",
			expectedSendCount:       1,
			expectedInvocationCount: 0,
			expectedWaitCount:       1,
		},
		{
			name: "version is same",
			cmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Idle\", \"active_partition\": { \"image\": { \"version\": \"0.0.0\"}}}"),
			},
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
			expectedOk:              false,
		},
		{
			name: "another version is available",
			cmdOut: &ssm.SendCommandOutput{
				Command: &ssm.Command{
					CommandId: aws.String("command-id"),
				},
			},
			invocationOut: &ssm.GetCommandInvocationOutput{
				StandardOutputContent: aws.String("{\"update_state\": \"Available\", \"active_partition\": { \"image\": { \"version\": \"0.0.2\"}}}"),
			},
			expectedSendCount:       1,
			expectedInvocationCount: 1,
			expectedWaitCount:       1,
			expectedOk:              true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "wait command failure" {
				t.Skip("skip until error from WaitUntilCommandExecuted is handled")
			}
			sendCount := 0
			invocationCount := 0
			waitCount := 0
			mockSSM := MockSSM{
				SendCommandFn: func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					assert.Equal(t, "check-document", aws.StringValue(input.DocumentName))
					sendCount++
					return tc.cmdOut, tc.cmdErr
				},
				GetCommandInvocationFn: func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					invocationCount++
					return tc.invocationOut, tc.invocationErr
				},
				WaitUntilCommandExecutedWithContextFn: func(ctx aws.Context, input *ssm.GetCommandInvocationInput, opts ...request.WaiterOption) error {
					assert.Equal(t, "command-id", aws.StringValue(input.CommandId))
					assert.Equal(t, "instance-id", aws.StringValue(input.InstanceId))
					waitCount++
					return tc.waitCommandErr
				},
			}
			u := updater{ssm: mockSSM, checkDocument: "check-document"}
			ok, err := u.verifyUpdate(instance{
				instanceID:          "instance-id",
				containerInstanceID: "cont-inst-id",
				bottlerocketVersion: "0.0.0",
			})
			if tc.expectedErr != "" {
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
			switch {
			case tc.cmdErr != nil:
				assert.ErrorIs(t, err, tc.cmdErr)
			case tc.invocationErr != nil:
				assert.ErrorIs(t, err, tc.invocationErr)
			case tc.expectedErr == "":
				require.NoError(t, err)
			}
			assert.Equal(t, tc.expectedOk, ok, "should get expected verify status")
			assert.Equal(t, tc.expectedSendCount, sendCount, "should match SendCommand call count")
			assert.Equal(t, tc.expectedInvocationCount, invocationCount, "should match GetCommandInvocation call count")
			assert.Equal(t, tc.expectedWaitCount, waitCount, "should match WaitUntilCommandExecuted call count")
		})
	}
}

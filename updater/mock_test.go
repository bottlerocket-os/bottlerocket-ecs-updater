package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

type MockECS struct {
	ListContainerInstancesFn           func(input *ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error)
	DescribeContainerInstancesFn       func(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error)
	UpdateContainerInstancesStateFn    func(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error)
	ListTasksFn                        func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error)
	DescribeTasksFn                    func(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error)
	WaitUntilTasksStoppedWithContextFn func(ctx aws.Context, input *ecs.DescribeTasksInput, opts ...request.WaiterOption) error
}

var _ ECSAPI = (*MockECS)(nil)

type MockSSM struct {
	WaitUntilCommandExecutedWithContextFn func(ctx aws.Context, input *ssm.GetCommandInvocationInput, opts ...request.WaiterOption) error
	SendCommandFn                         func(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error)
	GetCommandInvocationFn                func(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error)
}

var _ SSMAPI = (*MockSSM)(nil)

func (m MockECS) ListContainerInstances(input *ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error) {
	return m.ListContainerInstancesFn(input)
}

func (m MockECS) DescribeContainerInstances(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error) {
	return m.DescribeContainerInstancesFn(input)
}

func (m MockECS) UpdateContainerInstancesState(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error) {
	return m.UpdateContainerInstancesStateFn(input)
}

func (m MockECS) ListTasks(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error) {
	return m.ListTasksFn(input)
}

func (m MockECS) DescribeTasks(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error) {
	return m.DescribeTasksFn(input)
}

func (m MockECS) WaitUntilTasksStoppedWithContext(ctx aws.Context, input *ecs.DescribeTasksInput, opts ...request.WaiterOption) error {
	return m.WaitUntilTasksStoppedWithContextFn(ctx, input, opts...)
}

func (m MockSSM) SendCommand(input *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
	return m.SendCommandFn(input)
}

func (m MockSSM) WaitUntilCommandExecutedWithContext(ctx aws.Context, input *ssm.GetCommandInvocationInput, opts ...request.WaiterOption) error {
	return m.WaitUntilCommandExecutedWithContextFn(ctx, input, opts...)
}

func (m MockSSM) GetCommandInvocation(input *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
	return m.GetCommandInvocationFn(input)
}

package main

import "github.com/aws/aws-sdk-go/service/ecs"

type MockECS struct {
	ListContainerInstancesFn        func(input *ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error)
	ListContainerInstancesPagesFn   func(input *ecs.ListContainerInstancesInput, fn func(*ecs.ListContainerInstancesOutput, bool) bool) error
	DescribeContainerInstancesFn    func(input *ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error)
	UpdateContainerInstancesStateFn func(input *ecs.UpdateContainerInstancesStateInput) (*ecs.UpdateContainerInstancesStateOutput, error)
	ListTasksFn                     func(input *ecs.ListTasksInput) (*ecs.ListTasksOutput, error)
	DescribeTasksFn                 func(input *ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error)
	WaitUntilTasksStoppedFn         func(input *ecs.DescribeTasksInput) error
}

var _ ECSAPI = (*MockECS)(nil)

func (m MockECS) ListContainerInstances(input *ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error) {
	return m.ListContainerInstancesFn(input)
}

func (m MockECS) ListContainerInstancesPages(input *ecs.ListContainerInstancesInput, fn func(*ecs.ListContainerInstancesOutput, bool) bool) error {
	return m.ListContainerInstancesPagesFn(input, fn)
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

func (m MockECS) WaitUntilTasksStopped(input *ecs.DescribeTasksInput) error {
	return m.WaitUntilTasksStoppedFn(input)
}

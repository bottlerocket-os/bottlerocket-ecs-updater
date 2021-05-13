package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListContainerInstances(t *testing.T) {
	output := &ecs.ListContainerInstancesOutput{
		ContainerInstanceArns: []*string{
			aws.String("cont-inst-arn1"),
			aws.String("cont-inst-arn2"),
			aws.String("cont-inst-arn3")},
		NextToken: aws.String("token"),
	}
	output2 := &ecs.ListContainerInstancesOutput{
		ContainerInstanceArns: []*string{
			aws.String("cont-inst-arn4"),
			aws.String("cont-inst-arn5"),
			aws.String("cont-inst-arn6")},
		NextToken: nil,
	}
	expected := []*string{
		aws.String("cont-inst-arn1"),
		aws.String("cont-inst-arn2"),
		aws.String("cont-inst-arn3"),
		aws.String("cont-inst-arn4"),
		aws.String("cont-inst-arn5"),
		aws.String("cont-inst-arn6")}

	mockECS := MockECS{
		ListContainerInstancesPagesFn: func(_ *ecs.ListContainerInstancesInput, fn func(*ecs.ListContainerInstancesOutput, bool) bool) error {
			fn(output, true)
			fn(output2, false)
			return nil
		},
		ListContainerInstancesFn: func(_ *ecs.ListContainerInstancesInput) (*ecs.ListContainerInstancesOutput, error) {
			return output, nil
		},
	}
	u := updater{ecs: mockECS}

	actual, err := u.listContainerInstances()
	require.NoError(t, err)
	assert.EqualValues(t, expected, actual)
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

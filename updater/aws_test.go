package main

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
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

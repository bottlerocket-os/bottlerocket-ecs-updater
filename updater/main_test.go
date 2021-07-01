package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskDefFamily(t *testing.T) {
	cases := []struct {
		name           string
		taskDefARN     string
		expectedErr    string
		expectedFamily string
	}{
		{
			name:           "success",
			taskDefARN:     "arn:aws:ecs:us-west-2:1234567:task-definition/updater-family:1",
			expectedFamily: "updater-family",
		},
		{
			name:           "fail parse arn",
			taskDefARN:     "arn:ecs:us-west-2:1234567updater-family:1",
			expectedFamily: "",
			expectedErr:    "arn: not enough sections",
		},
		{
			name:           "fail empty arn",
			taskDefARN:     "",
			expectedFamily: "",
			expectedErr:    "arn: invalid prefix",
		},
		{
			name:           "fail extract family",
			taskDefARN:     "arn:aws:ecs:us-west-2:1234567:task-def/updater-family1",
			expectedFamily: "",
			expectedErr:    "not a task definition arn:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalValue := os.Getenv(taskDefARNEnv)
			defer func() { os.Setenv(taskDefARNEnv, originalValue) }()
			os.Setenv(taskDefARNEnv, tc.taskDefARN)
			family, err := taskDefFamily()
			if tc.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			}
			assert.Equal(t, tc.expectedFamily, family)
		})
	}
}

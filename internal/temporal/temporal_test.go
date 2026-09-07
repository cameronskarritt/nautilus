package temporal_test

import (
	"testing"

	"nautilus/internal/config"
	"nautilus/internal/temporal"
	"nautilus/internal/testutil/require"
)

type settings map[string]string

func (s settings) Get(key string) (string, bool) {
	value, ok := s[key]
	return value, ok
}

func TestTaskQueues(t *testing.T) {
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
	tests := []struct {
		name    string
		values  settings
		want    []string
		wantErr bool
	}{
		{name: "default", want: []string{"nautilus"}},
		{name: "legacy", values: settings{"TEMPORAL_TASK_QUEUE": "uploads"}, want: []string{"uploads"}},
		{name: "plural wins", values: settings{"TEMPORAL_TASK_QUEUE": "legacy", "TEMPORAL_TASK_QUEUES": "uploads,ocr"}, want: []string{"uploads", "ocr"}},
		{name: "trim and deduplicate", values: settings{"TEMPORAL_TASK_QUEUES": " uploads, ocr,uploads , indexing "}, want: []string{"uploads", "ocr", "indexing"}},
		{name: "preserve case", values: settings{"TEMPORAL_TASK_QUEUES": "ocr,OCR"}, want: []string{"ocr", "OCR"}},
		{name: "empty overrides legacy", values: settings{"TEMPORAL_TASK_QUEUE": "legacy", "TEMPORAL_TASK_QUEUES": ""}, wantErr: true},
		{name: "blank", values: settings{"TEMPORAL_TASK_QUEUES": " "}, wantErr: true},
		{name: "leading comma", values: settings{"TEMPORAL_TASK_QUEUES": ",ocr"}, wantErr: true},
		{name: "trailing comma", values: settings{"TEMPORAL_TASK_QUEUES": "ocr,"}, wantErr: true},
		{name: "empty entry", values: settings{"TEMPORAL_TASK_QUEUES": "uploads, ,ocr"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.SetProvider(tt.values)
			got, err := temporal.TaskQueues()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRunWorkersRequiresQueue(t *testing.T) {
	t.Parallel()
	require.Error(t, temporal.RunWorkers(t.Context(), nil, nil))
}

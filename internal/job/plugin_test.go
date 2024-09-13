package job

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/job/shell"
)

func Test_checkPluginsEnabled(t *testing.T) {
	type args struct {
		logger         shell.Logger
		executorConfig ExecutorConfig
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "Plugins and local hooks are Enabled",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:    true,
					LocalHooksEnabled: true,
					CommandEval:       true,
				},
			},
			want: true,
		},
		{
			name: "Plugins are disabled",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:         false,
					LocalHooksEnabled:      true,
					CommandEval:            true,
					PluginsFailureBehavior: "error",
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "Plugins are disabled, and failure behavior is warn",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:         false,
					LocalHooksEnabled:      true,
					CommandEval:            true,
					PluginsFailureBehavior: "warn",
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "Local hooks are disabled, and failure behavior is error",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:            true,
					LocalHooksEnabled:         false,
					LocalHooksFailureBehavior: "error",
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "Local hooks are disabled, and failure behavior is warn",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:            true,
					LocalHooksEnabled:         false,
					CommandEval:               true,
					LocalHooksFailureBehavior: "warn",
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "Local hooks are disabled, and failure behavior is warn",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:         false,
					LocalHooksEnabled:      true,
					CommandEval:            true,
					PluginsFailureBehavior: "warn",
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "command eval is disabled",
			args: args{
				logger: shell.DiscardLogger,
				executorConfig: ExecutorConfig{
					PluginsEnabled:    true,
					LocalHooksEnabled: true,
					CommandEval:       false,
				},
			},
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkPluginsEnabled(tt.args.logger, tt.args.executorConfig)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("checkPluginsEnabled() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
			if got != tt.want {
				t.Errorf("checkPluginsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}

}

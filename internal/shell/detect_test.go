package shell

import "testing"

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		env       map[string]string
		parent    string
		want      Kind
		wantErr   bool
		errSubstr string
	}{
		{
			name:   "windows powershell parent",
			goos:   "windows",
			env:    map[string]string{},
			parent: "powershell.exe",
			want:   KindPowerShell,
		},
		{
			name:   "windows pwsh parent",
			goos:   "windows",
			env:    map[string]string{},
			parent: "pwsh.exe",
			want:   KindPowerShell,
		},
		{
			name:      "windows cmd parent rejected",
			goos:      "windows",
			env:       map[string]string{},
			parent:    "cmd.exe",
			wantErr:   true,
			errSubstr: "cmd.exe is not supported",
		},
		{
			name: "windows wsl interop",
			goos: "windows",
			env: map[string]string{
				"WSL_INTEROP": "/run/WSL/123_interop",
			},
			parent: "bash",
			want:   KindWSLBash,
		},
		{
			name: "linux wsl",
			goos: "linux",
			env: map[string]string{
				"WSL_DISTRO_NAME": "Ubuntu",
			},
			want: KindWSLBash,
		},
		{
			name:      "plain linux rejected",
			goos:      "linux",
			env:       map[string]string{"SHELL": "/usr/bin/bash"},
			wantErr:   true,
			errSubstr: "llmctl requires WSL2",
		},
		{
			name:      "unsupported os",
			goos:      "darwin",
			env:       map[string]string{},
			wantErr:   true,
			errSubstr: "unsupported operating system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detectShell(tt.goos, func(key string) string {
				return tt.env[key]
			}, func() (string, error) {
				return tt.parent, nil
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("kind mismatch: got %q want %q", got, tt.want)
			}
		})
	}
}

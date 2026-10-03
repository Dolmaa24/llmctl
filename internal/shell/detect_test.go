package shell

import "testing"

func TestDetectFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		want    Kind
		wantErr bool
	}{
		{
			name: "windows powershell",
			goos: "windows",
			env:  map[string]string{},
			want: KindPowerShell,
		},
		{
			name: "windows wsl interop",
			goos: "windows",
			env: map[string]string{
				"WSL_INTEROP": "/run/WSL/123_interop",
			},
			want: KindWSLBash,
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
			name: "linux bash",
			goos: "linux",
			env: map[string]string{
				"SHELL": "/usr/bin/bash",
			},
			want: KindWSLBash,
		},
		{
			name:    "unsupported os",
			goos:    "darwin",
			env:     map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detectFromEnv(tt.goos, func(key string) string {
				return tt.env[key]
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
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

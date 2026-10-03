package shell

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type SystemDetector struct {
	Getenv func(string) string
	GOOS   string
}

func (d SystemDetector) Detect() (Kind, error) {
	goos := d.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	getenv := d.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	return detectFromEnv(goos, getenv)
}

func detectFromEnv(goos string, getenv func(string) string) (Kind, error) {
	switch goos {
	case "windows":
		if getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != "" {
			return KindWSLBash, nil
		}
		return KindPowerShell, nil
	case "linux":
		if getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != "" {
			return KindWSLBash, nil
		}
		if strings.Contains(strings.ToLower(getenv("SHELL")), "bash") {
			return KindWSLBash, nil
		}
		return "", fmt.Errorf("unsupported shell environment on linux")
	default:
		return "", fmt.Errorf("unsupported operating system: %s", goos)
	}
}

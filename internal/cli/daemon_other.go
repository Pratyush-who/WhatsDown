//go:build !windows

package cli

import (
	"os/exec"
)

func setDetachedProcess(cmd *exec.Cmd) {
	// Default process attributes for Unix-like systems
}

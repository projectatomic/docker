// Main package for forward-journald
package main

import (
	"bytes"
	"os/exec"
	"strings"
)

// GetProcess uses the system command pgrep to determine the pid for the process name given
// It returns the pid or any error reported by os/exec.Command.Run()
func GetProcess(filter string) (string, error) {
	var out bytes.Buffer
	command := exec.Command("pgrep", filter)
	command.Stdout = &out

	err := command.Run()
	if err == nil {
		return strings.TrimSpace(out.String()), err
	} else {
		return "", err
	}
}

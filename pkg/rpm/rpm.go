package rpm

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Version returns package version for the specified package or executable path
func Version(name string) (string, error) {
	options := "-q"
	if filepath.IsAbs(name) {
		options = options + "f"
	}
	rpmPath, err := exec.LookPath("rpm")
	if err != nil {
		return "", err
	}

	cmd := exec.Command(rpmPath, options, name)

	var out bytes.Buffer
	cmd.Stdout = &out

	cmd.Start()

	done := make(chan error)
	go func() { done <- cmd.Wait() }()

	timeout := time.After(10 * time.Second)

	select {
	case <-timeout:
		cmd.Process.Kill()
		return "rpm timed out", errors.New("rpm timed out")
	case err := <-done:
		return strings.TrimSpace(out.String()), err
	}
}

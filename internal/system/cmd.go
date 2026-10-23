package system

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// ExecuteWithInput runs a command with the given text as input.
func ExecuteWithInput(exe, text string, args ...string) (string, error) {
	return ExecuteWithInputEnv(exe, text, nil, args...)
}

// ExecuteWithInputEnv is ExecuteWithInput with variables added to the
// command's environment.
func ExecuteWithInputEnv(exe, text string, env []string, args ...string) (string, error) {
	var out bytes.Buffer
	var eut bytes.Buffer

	cmd := exec.Command(exe, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = &out
	cmd.Stderr = &eut

	if err := cmd.Run(); err != nil {
		return "", errors.New(eut.String())
	}

	return out.String(), nil
}

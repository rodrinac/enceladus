// Package rreport mirrors src/report_runner.py: the execution boundary for the
// R report scripts.
package rreport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const DefaultTimeout = 900 * time.Second

// ExecFn is the process runner seam (stubbed in tests).
var ExecFn = func(cmd []string, dir string, timeout time.Duration) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return 124, string(output) + "\nRscript excedeu o timeout de " + fmtTimeout(timeout) + "."
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(output)
		}
		return 1, string(output) + "\n" + err.Error()
	}
	return 0, string(output)
}

func fmtTimeout(timeout time.Duration) string {
	return fmt.Sprintf("%gs", timeout.Seconds())
}

// Runner executes R scripts from the rscripts directory with the working
// directory set to the source root, exactly like run_rscript did.
type Runner struct {
	SourceRoot  string
	RscriptsDir string
	Timeout     time.Duration
	ScriptBin   string
}

func (r *Runner) bin() string {
	if r.ScriptBin != "" {
		return r.ScriptBin
	}
	return "Rscript"
}

// Run executes the given script with the provided stringified arguments.
func (r *Runner) Run(script string, arguments []string) (int, string) {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	command := append([]string{r.bin(), filepath.Join(r.RscriptsDir, script)}, arguments...)
	cwd := r.SourceRoot
	if cwd == "" {
		cwd = "."
	}
	return ExecFn(command, cwd, timeout)
}

// EnsureRscriptsDir exists mirrors the module-level mkdir calls for report
// folders (Python created them at import time).
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

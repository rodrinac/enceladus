// Package rreport mirrors src/report_runner.py: the execution boundary for the
// R report scripts.
package rreport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

// allowedScripts is the closed set of R entry points the API may execute.
// The script argument never comes from the request; this allowlist keeps a
// compromised caller from pointing Rscript at an arbitrary file.
var allowedScripts = map[string]bool{
	"densidade_municipal_por_periodo_geral.R":  true,
	"densidade_municipal_por_periodo.R":        true,
	"casos_mensais_por_municipio_por_estado.R": true,
}

var safeScriptName = regexp.MustCompile(`^[a-z0-9_]+\.R$`)

// validArg rejects option injection ("-e ...") and shell metacharacters. R
// arguments are states, ISO dates/years and report paths built by the worker;
// none of them legitimately starts with "-" or contains NUL bytes, quotes,
// semicolons, pipes, subshell or redirection tokens.
func validArg(arg string) bool {
	if arg == "" || strings.ContainsRune(arg, 0) {
		return false
	}
	if strings.HasPrefix(arg, "-") {
		return false
	}
	if strings.ContainsAny(arg, "\n\r;|$`&<>!#*?~(){}[]'\"\\") {
		return false
	}
	return true
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
// The script must be in the allowlist and every argument must pass validArg,
// so request-derived states/dates/paths can never become Rscript options or
// shell metacharacters (the command is executed without a shell).
func (r *Runner) Run(script string, arguments []string) (int, string) {
	if !safeScriptName.MatchString(script) || !allowedScripts[script] {
		return 1, "script de relatório desconhecido"
	}
	for _, arg := range arguments {
		if !validArg(arg) {
			return 1, "argumento de relatório inválido"
		}
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	scriptPath := filepath.Join(r.RscriptsDir, script)
	absBase, err := filepath.Abs(r.RscriptsDir)
	if err != nil {
		return 1, "script de relatório inválido"
	}
	absScript, err := filepath.Abs(scriptPath)
	if err != nil || (absScript != absBase && !strings.HasPrefix(absScript, absBase+string(os.PathSeparator))) {
		return 1, "script de relatório inválido"
	}
	command := append([]string{r.bin(), absScript}, arguments...)
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

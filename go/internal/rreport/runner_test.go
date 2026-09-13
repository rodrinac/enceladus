package rreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerInvokesScriptWithArguments(t *testing.T) {
	original := ExecFn
	defer func() { ExecFn = original }()

	var capturedCommand []string
	var capturedDir string
	var capturedTimeout time.Duration
	ExecFn = func(command []string, dir string, timeout time.Duration) (int, string) {
		capturedCommand = command
		capturedDir = dir
		capturedTimeout = timeout
		return 7, "boom"
	}

	runner := &Runner{SourceRoot: "/src", RscriptsDir: "/src/rscripts", Timeout: 42 * time.Second}
	code, output := runner.Run("densidade_municipal_por_periodo_geral.R", []string{"CE,BA", "2020", "2022", "/out.pdf", "/work"})

	if code != 7 || output != "boom" {
		t.Fatalf("unexpected run result: %d %q", code, output)
	}
	joined := strings.Join(capturedCommand, " ")
	if !strings.Contains(joined, "/src/rscripts/densidade_municipal_por_periodo_geral.R") ||
		!strings.Contains(joined, "CE,BA 2020 2022") {
		t.Fatalf("unexpected command: %v", capturedCommand)
	}
	if capturedDir != "/src" {
		t.Fatalf("expected source root cwd, got %q", capturedDir)
	}
	if capturedTimeout != 42*time.Second {
		t.Fatalf("unexpected timeout: %v", capturedTimeout)
	}
}

func TestRunnerDefaults(t *testing.T) {
	original := ExecFn
	defer func() { ExecFn = original }()

	var capturedCommand []string
	ExecFn = func(command []string, dir string, timeout time.Duration) (int, string) {
		capturedCommand = command
		return 0, ""
	}

	runner := &Runner{RscriptsDir: "rscripts"}
	_, _ = runner.Run("densidade_municipal_por_periodo.R", nil)
	if len(capturedCommand) == 0 || capturedCommand[0] != "Rscript" {
		t.Fatalf("expected default Rscript binary, got %v", capturedCommand)
	}
	if _, err := os.Stat(filepath.Join("rscripts")); err != nil {
		t.Skip("no rscripts dir here")
	}
}

func TestRunnerRejectsUnknownScript(t *testing.T) {
	original := ExecFn
	defer func() { ExecFn = original }()

	called := false
	ExecFn = func(command []string, dir string, timeout time.Duration) (int, string) {
		called = true
		return 0, ""
	}

	runner := &Runner{RscriptsDir: "rscripts"}
	for _, script := range []string{"script.R", "../evil.R", "-e.R", "casos_mensais_por_municipio_por_estado.r"} {
		if code, _ := runner.Run(script, nil); code == 0 {
			t.Fatalf("expected rejection for script %q", script)
		}
	}
	if called {
		t.Fatal("ExecFn must not run for rejected scripts")
	}
}

func TestRunnerRejectsOptionInjection(t *testing.T) {
	original := ExecFn
	defer func() { ExecFn = original }()

	called := false
	ExecFn = func(command []string, dir string, timeout time.Duration) (int, string) {
		called = true
		return 0, ""
	}

	runner := &Runner{RscriptsDir: "rscripts"}
	if code, _ := runner.Run("densidade_municipal_por_periodo.R",
		[]string{"CE", "2020", "2022", "/out.pdf", "-e;rm -rf /"}); code == 0 {
		t.Fatal("expected rejection for option-injection argument")
	}
	if called {
		t.Fatal("ExecFn must not run for rejected arguments")
	}
}

func TestExecTimeoutMapsToMessage(t *testing.T) {
	code, output := ExecFn([]string{"sh", "-c", "sleep 5"}, ".", 100*time.Millisecond)
	if code != 124 {
		t.Fatalf("expected exit code 124, got %d", code)
	}
	if !strings.Contains(output, "Rscript excedeu o timeout de 0.1s.") {
		t.Fatalf("missing timeout message: %q", output)
	}
}

func TestExecFailureReturnsExitCode(t *testing.T) {
	code, _ := ExecFn([]string{"sh", "-c", "exit 3"}, ".", time.Second)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}
}

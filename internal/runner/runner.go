package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
	RunPowerShell(ctx context.Context, script string, dir string) (Result, error)
	DryRun() bool
	Verbose() bool
}

type ExecRunner struct {
	IsDryRun  bool
	IsVerbose bool
	Out       io.Writer
	ErrOut    io.Writer
}

func New(dryRun, verbose bool, out, errOut io.Writer) *ExecRunner {
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	return &ExecRunner{
		IsDryRun:  dryRun,
		IsVerbose: verbose,
		Out:       out,
		ErrOut:    errOut,
	}
}

func (r *ExecRunner) DryRun() bool {
	return r.IsDryRun
}

func (r *ExecRunner) Verbose() bool {
	return r.IsVerbose
}

func (r *ExecRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	if r.IsDryRun {
		fmt.Fprintf(r.Out, "[dry-run] %s %s\n", cmd.Name, strings.Join(cmd.Args, " "))
		return Result{}, nil
	}

	execCmd := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	if cmd.Dir != "" {
		execCmd.Dir = cmd.Dir
	}
	baseEnv := append(os.Environ(), "PYTHONUTF8=1", "LANG=C.UTF-8")
	execCmd.Env = append(baseEnv, cmd.Env...)

	var stdoutBuf, stderrBuf bytes.Buffer
	if r.IsVerbose {
		fmt.Fprintf(r.Out, "+ %s %s\n", cmd.Name, strings.Join(cmd.Args, " "))
		execCmd.Stdout = io.MultiWriter(&stdoutBuf, r.Out)
		execCmd.Stderr = io.MultiWriter(&stderrBuf, r.ErrOut)
	} else {
		execCmd.Stdout = &stdoutBuf
		execCmd.Stderr = &stderrBuf
	}

	err := execCmd.Run()
	res := Result{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = 1
		}
		return res, fmt.Errorf("command %q failed (exit %d): %w", cmd.Name, res.ExitCode, err)
	}
	return res, nil
}

func (r *ExecRunner) RunPowerShell(ctx context.Context, script string, dir string) (Result, error) {
	utf8Prefix := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $OutputEncoding = [System.Text.Encoding]::UTF8; "
	return r.Run(ctx, Command{
		Name: "powershell.exe",
		Args: []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", utf8Prefix + script},
		Dir:  dir,
	})
}

func SplitToolCommand(tool string) (string, []string) {
	fields := strings.Fields(tool)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

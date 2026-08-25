package installplatform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

const (
	maxUIInitializerOutputBytes = 64 << 10
	maxUIInitializerStageBytes  = 256
	uiInitializerLogPath        = ".runtime/install/dependency-install.log"
)

var (
	ErrUIInitializerPortInvalid           = errors.New("UI initializer port is invalid")
	ErrUIInitializerStateDirectoryInvalid = errors.New("UI initializer state directory is invalid")
	ErrUIInitializerCommandFailed         = errors.New("UI initializer command failed")
)

type uiInitializerCommand struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type uiInitializerCommandRunner interface {
	Run(context.Context, uiInitializerCommand, io.Writer, io.Writer) error
}

type CommandUIInitializer struct {
	workspaceRoot   string
	adminRoot       string
	port            int
	runner          uiInitializerCommandRunner
	environment     []string
	operatingSystem string
}

func NewCommandUIInitializer(workspaceRoot, stateDirectory string, port int) (*CommandUIInitializer, error) {
	return newCommandUIInitializer(workspaceRoot, stateDirectory, port, systemUIInitializerCommandRunner{}, os.Environ())
}

func newCommandUIInitializer(workspaceRoot, stateDirectory string, port int, runner uiInitializerCommandRunner, environment []string) (*CommandUIInitializer, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, ErrWorkspaceRootRequired
	}
	if port < 1 || port > 65535 {
		return nil, ErrUIInitializerPortInvalid
	}
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, ErrWorkspaceRootInvalid
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	adminRoot := filepath.Join(canonical, "admin")
	adminInfo, err := os.Lstat(adminRoot)
	if err != nil || !adminInfo.IsDir() || adminInfo.Mode()&os.ModeSymlink != 0 {
		return nil, ErrWorkspaceRootInvalid
	}
	if runner == nil {
		return nil, errors.New("UI initializer command runner is not configured")
	}
	canonicalStateDirectory, err := canonicalizeUIInitializerStateDirectory(stateDirectory)
	if err != nil {
		return nil, err
	}
	filteredEnvironment := filterUIInitializerEnvironment(environment)
	filteredEnvironment = append(filteredEnvironment, "GIN_VBEN_INSTALL_STATE_DIR="+canonicalStateDirectory)
	return &CommandUIInitializer{
		workspaceRoot:   filepath.Clean(canonical),
		adminRoot:       filepath.Clean(adminRoot),
		port:            port,
		runner:          runner,
		environment:     filteredEnvironment,
		operatingSystem: runtime.GOOS,
	}, nil
}

func (i *CommandUIInitializer) Prepare(ctx context.Context, selectedUI installstate.UI, report func(installer.UIPreparationProgress)) error {
	if !validUIInitializerSelection(selectedUI) {
		return installer.ErrUIPreparationInvalid
	}
	return i.run(ctx, installer.UIPreparationActionPrepare, []string{
		"--ui", string(selectedUI), "--confirm-cleanup", "--no-open", "--port", strconv.Itoa(i.port),
	}, report)
}

func (i *CommandUIInitializer) Reset(ctx context.Context, report func(installer.UIPreparationProgress)) error {
	return i.run(ctx, installer.UIPreparationActionReset, []string{
		"--reset", "--confirm-reset", "--no-open", "--port", strconv.Itoa(i.port),
	}, report)
}

func (i *CommandUIInitializer) LogPath() string {
	return uiInitializerLogPath
}

func (i *CommandUIInitializer) run(ctx context.Context, action installer.UIPreparationAction, commandArgs []string, report func(installer.UIPreparationProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if i == nil || i.runner == nil || i.workspaceRoot == "" || i.adminRoot == "" {
		return ErrUIInitializerCommandFailed
	}
	pnpmArgs := append([]string{
		"--dir", i.adminRoot, "run", "init", "--",
	}, commandArgs...)
	invocation, buildErr := buildUIInitializerCommand(i.operatingSystem, i.workspaceRoot, pnpmArgs, i.environment)
	if buildErr != nil {
		return buildErr
	}
	stdout := newUIInitializerOutput(action, report)
	stderr := newUIInitializerOutput("", nil)
	err := i.runner.Run(ctx, invocation, stdout, stderr)
	stdout.finish()
	stderr.finish()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return ErrUIInitializerCommandFailed
	}
	return nil
}

func canonicalizeUIInitializerStateDirectory(stateDirectory string) (string, error) {
	if strings.TrimSpace(stateDirectory) == "" {
		return "", ErrUIInitializerStateDirectoryInvalid
	}
	absolute, err := filepath.Abs(stateDirectory)
	if err != nil {
		return "", ErrUIInitializerStateDirectoryInvalid
	}
	absolute = filepath.Clean(absolute)
	root := string(filepath.Separator)
	if volume := filepath.VolumeName(absolute); volume != "" {
		root = volume + string(filepath.Separator)
	}
	if absolute == root {
		return "", ErrUIInitializerStateDirectoryInvalid
	}

	existing := absolute
	suffix := make([]string, 0, 2)
	for {
		info, inspectErr := os.Lstat(existing)
		if inspectErr == nil {
			if !info.IsDir() {
				return "", ErrUIInitializerStateDirectoryInvalid
			}
			break
		}
		if !errors.Is(inspectErr, os.ErrNotExist) {
			return "", ErrUIInitializerStateDirectoryInvalid
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", ErrUIInitializerStateDirectoryInvalid
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	canonicalParent, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", ErrUIInitializerStateDirectoryInvalid
	}
	for index := len(suffix) - 1; index >= 0; index-- {
		canonicalParent = filepath.Join(canonicalParent, suffix[index])
	}
	canonical, err := filepath.Abs(canonicalParent)
	if err != nil || filepath.Clean(canonical) == root {
		return "", ErrUIInitializerStateDirectoryInvalid
	}
	return filepath.Clean(canonical), nil
}

func buildUIInitializerCommand(operatingSystem, workspaceRoot string, pnpmArgs, environment []string) (uiInitializerCommand, error) {
	invocation := uiInitializerCommand{
		Name: "pnpm",
		Args: append([]string(nil), pnpmArgs...),
		Dir:  workspaceRoot,
		Env:  append([]string(nil), environment...),
	}
	if operatingSystem != "windows" {
		return invocation, nil
	}
	if !safeWindowsUIInitializerArgument(workspaceRoot) {
		return uiInitializerCommand{}, ErrWorkspaceRootInvalid
	}
	var commandLine strings.Builder
	commandLine.WriteString("call pnpm.cmd")
	for _, argument := range pnpmArgs {
		if !safeWindowsUIInitializerArgument(argument) {
			return uiInitializerCommand{}, ErrWorkspaceRootInvalid
		}
		commandLine.WriteByte(' ')
		commandLine.WriteByte('"')
		commandLine.WriteString(argument)
		commandLine.WriteByte('"')
	}
	invocation.Name = "cmd.exe"
	invocation.Args = []string{"/d", "/s", "/c", commandLine.String()}
	return invocation, nil
}

func safeWindowsUIInitializerArgument(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\x00\r\n\"&|<>^%!()")
}

func validUIInitializerSelection(ui installstate.UI) bool {
	switch ui {
	case installstate.UIAntd, installstate.UIEle, installstate.UINaive:
		return true
	default:
		return false
	}
}

type systemUIInitializerCommandRunner struct{}

func (systemUIInitializerCommandRunner) Run(ctx context.Context, invocation uiInitializerCommand, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, invocation.Name, invocation.Args...)
	command.Dir = invocation.Dir
	command.Env = append([]string(nil), invocation.Env...)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func filterUIInitializerEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !allowedUIInitializerEnvironmentKey(key) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func allowedUIInitializerEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "LC_") {
		return true
	}
	switch upper {
	case "PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "TMP", "TEMP",
		"SYSTEMROOT", "COMSPEC", "PATHEXT", "APPDATA", "LOCALAPPDATA",
		"PNPM_HOME", "LANG", "XDG_CACHE_HOME", "NO_COLOR", "FORCE_COLOR":
		return true
	default:
		return false
	}
}

// uiInitializerOutput both retains a fixed-size diagnostic prefix and parses
// progress incrementally. Write always accepts the caller's full buffer so a
// chatty child process cannot block after the retention budget is exhausted.
type uiInitializerOutput struct {
	mu       sync.Mutex
	retained bytes.Buffer
	action   installer.UIPreparationAction
	report   func(installer.UIPreparationProgress)
	line     []byte
	overflow bool
}

func newUIInitializerOutput(action installer.UIPreparationAction, report func(installer.UIPreparationProgress)) *uiInitializerOutput {
	return &uiInitializerOutput{action: action, report: report, line: make([]byte, 0, maxUIInitializerStageBytes)}
}

func (o *uiInitializerOutput) Write(value []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if remaining := maxUIInitializerOutputBytes - o.retained.Len(); remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		_, _ = o.retained.Write(value[:remaining])
	}
	if o.report != nil {
		for _, character := range value {
			if character == '\n' {
				o.emitLineLocked()
				continue
			}
			if o.overflow {
				continue
			}
			if len(o.line) == maxUIInitializerStageBytes {
				o.line = o.line[:0]
				o.overflow = true
				continue
			}
			o.line = append(o.line, character)
		}
	}
	return len(value), nil
}

func (o *uiInitializerOutput) Len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.retained.Len()
}

func (o *uiInitializerOutput) finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.line) > 0 || o.overflow {
		o.emitLineLocked()
	}
}

func (o *uiInitializerOutput) emitLineLocked() {
	if !o.overflow {
		line := strings.TrimSuffix(string(o.line), "\r")
		if progress, ok := parseUIInitializerStage(o.action, line); ok {
			o.report(progress)
		}
	}
	o.line = o.line[:0]
	o.overflow = false
}

func parseUIInitializerStage(action installer.UIPreparationAction, line string) (installer.UIPreparationProgress, bool) {
	prefix := "INIT_STAGE=" + string(action) + ":"
	if !strings.HasPrefix(line, prefix) {
		return installer.UIPreparationProgress{}, false
	}
	stage := strings.TrimPrefix(line, prefix)
	if action == installer.UIPreparationActionPrepare {
		switch stage {
		case "preflight":
			return installer.UIPreparationProgress{CurrentStep: "preflight", Progress: 10}, true
		case "workspace":
			return installer.UIPreparationProgress{CurrentStep: "workspace", Progress: 40}, true
		case "dependencies":
			return installer.UIPreparationProgress{CurrentStep: "dependencies", Progress: 75}, true
		case "complete":
			return installer.UIPreparationProgress{CurrentStep: "complete", Progress: 99}, true
		}
	}
	if action == installer.UIPreparationActionReset {
		switch stage {
		case "preflight":
			return installer.UIPreparationProgress{CurrentStep: "preflight", Progress: 10}, true
		case "workspace":
			return installer.UIPreparationProgress{CurrentStep: "reset", Progress: 60}, true
		case "complete":
			return installer.UIPreparationProgress{CurrentStep: "complete", Progress: 99}, true
		}
	}
	return installer.UIPreparationProgress{}, false
}

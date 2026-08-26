package installplatform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// On Windows, execute the Node entrypoint directly.  Passing a quoted
	// `cmd.exe /c call pnpm.cmd ...` string through os/exec applies two
	// different argument parsers (Go and cmd.exe); paths containing spaces can
	// then be changed before init.mjs starts.  The Node entrypoint itself uses
	// the shared pnpm command builder for dependency installation, so this
	// boundary does not need a shell or a pnpm shim.
	commandLineArgs := commandArgs
	if i.operatingSystem != "windows" {
		commandLineArgs = append([]string{
			"--dir", i.adminRoot, "run", "init", "--",
		}, commandArgs...)
	}
	invocation, buildErr := buildUIInitializerCommand(i.operatingSystem, i.workspaceRoot, commandLineArgs, i.environment)
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
		diagnostic := stdout.diagnosticSnapshot()
		mergeUIInitializerDiagnostics(&diagnostic, stderr.diagnosticSnapshot())
		failure := uiInitializerFailure(action, diagnostic)
		return fmt.Errorf("%w: %w", ErrUIInitializerCommandFailed, failure)
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

func buildUIInitializerCommand(operatingSystem, workspaceRoot string, scriptArgs, environment []string) (uiInitializerCommand, error) {
	invocation := uiInitializerCommand{
		Name: "pnpm",
		Args: append([]string(nil), scriptArgs...),
		Dir:  workspaceRoot,
		Env:  append([]string(nil), environment...),
	}
	if operatingSystem != "windows" {
		return invocation, nil
	}
	// Keep every path and option as a separate argv element.  This avoids the
	// cmd.exe quoting rules entirely and works with pnpm.cmd, pnpm.exe, and
	// other Windows shims because pnpm is only needed by the Node script later.
	invocation.Name = "node"
	invocation.Args = append([]string{
		filepath.Join(workspaceRoot, "admin", "scripts", "init.mjs"),
	}, scriptArgs...)
	return invocation, nil
}

func mergeUIInitializerDiagnostics(target *uiInitializerDiagnostic, source uiInitializerDiagnostic) {
	if target == nil {
		return
	}
	if target.lastStep == "" {
		target.lastStep = source.lastStep
	}
	if target.errorCode == "" {
		target.errorCode = source.errorCode
	}
	if target.scope == "" {
		target.scope = source.scope
	}
	if target.operation == "" {
		target.operation = source.operation
	}
	target.dependencyLog = target.dependencyLog || source.dependencyLog
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
		"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "PUBLIC", "PROGRAMDATA",
		"PNPM_HOME", "COREPACK_HOME", "LANG", "XDG_CACHE_HOME", "XDG_CONFIG_HOME",
		"XDG_DATA_HOME", "NO_COLOR", "FORCE_COLOR":
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
	details  uiInitializerDiagnostic
}

type uiInitializerDiagnostic struct {
	lastStep      string
	errorCode     string
	scope         string
	operation     string
	dependencyLog bool
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

func (o *uiInitializerOutput) diagnosticSnapshot() uiInitializerDiagnostic {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.details
}

func (o *uiInitializerOutput) emitLineLocked() {
	if !o.overflow {
		line := strings.TrimSuffix(string(o.line), "\r")
		if progress, ok := parseUIInitializerStage(o.action, line); ok {
			o.details.lastStep = progress.CurrentStep
			if o.report != nil {
				o.report(progress)
			}
		} else {
			o.parseDiagnosticLineLocked(line)
		}
	}
	o.line = o.line[:0]
	o.overflow = false
}

func (o *uiInitializerOutput) parseDiagnosticLineLocked(line string) {
	switch {
	case strings.HasPrefix(line, "INIT_ERROR="):
		if value := strings.TrimPrefix(line, "INIT_ERROR="); allowedUIInitializerErrorCode(value) {
			o.details.errorCode = value
		}
	case strings.HasPrefix(line, "INIT_FAILURE_SCOPE="):
		if value := strings.TrimPrefix(line, "INIT_FAILURE_SCOPE="); allowedUIInitializerScope(value) {
			o.details.scope = value
		}
	case strings.HasPrefix(line, "INIT_FAILURE_OPERATION="):
		if value := strings.TrimPrefix(line, "INIT_FAILURE_OPERATION="); allowedUIInitializerOperation(value) {
			o.details.operation = value
		}
	case line == "INIT_DEPENDENCY_LOG="+uiInitializerLogPath:
		o.details.dependencyLog = true
	}
}

func allowedUIInitializerErrorCode(value string) bool {
	switch value {
	case "NONE", "PREFLIGHT_FAILED", "TEMPLATE_LAYOUT_INVALID", "API_UNAVAILABLE", "INIT_BUSY",
		"INIT_LEASE_FAILED", "INSTALL_STATE_DIR_INVALID", "NODE_VERSION_UNSUPPORTED",
		"PNPM_VERSION_UNSUPPORTED", "DEPENDENCY_INSTALL_FAILED", "DEPENDENCY_TRANSACTION_INVALID",
		"DEPENDENCY_INSTALL_BUSY", "SOURCE_MOVE_STATE_INVALID", "INITIALIZATION_RESUME_INVALID",
		"RESET_LAYOUT_INVALID", "RESET_RECEIPT_UNAVAILABLE", "RESET_TRANSACTION_INVALID",
		"RESET_UNAVAILABLE", "RESET_UNAVAILABLE_INSTALLED", "LEGACY_MIGRATION_INVALID",
		"RECOVERY_VALIDATION_FAILED", "RUNTIME_ENV_APP_INVALID", "RUNTIME_ENV_PROFILE_INVALID",
		"RUNTIME_ENV_TARGET_INVALID", "RUNTIME_ENV_TEMPLATE_INVALID", "UI_INVALID",
		"UI_PACKAGE_MISMATCH", "UI_PROFILE_INVALID", "UI_PROFILE_MISMATCH", "UI_PROFILE_REQUIRED",
		"RESET_IN_PROGRESS", "STATE_INCONSISTENT", "INITIALIZATION_IN_PROGRESS", "INITIALIZATION_OPERATION_FAILED":
		return true
	default:
		return false
	}
}

func allowedUIInitializerScope(value string) bool {
	switch value {
	case "admin_root", "admin_apps", "selected_ui", "state_root", "ui_backup":
		return true
	default:
		return false
	}
}

func allowedUIInitializerOperation(value string) bool {
	switch value {
	case "read", "create", "write", "sync", "link", "rename", "delete", "cross_directory_rename", "execute", "lock":
		return true
	default:
		return false
	}
}

func uiInitializerFailure(action installer.UIPreparationAction, diagnostic uiInitializerDiagnostic) *installer.UIPreparationFailure {
	failure := &installer.UIPreparationFailure{
		ErrorKey:  "ui_prepare_failed",
		Step:      "launch",
		Reason:    "process_failed",
		Scope:     diagnostic.scope,
		Operation: diagnostic.operation,
	}
	if action == installer.UIPreparationActionReset {
		failure.ErrorKey = "ui_reset_failed"
	}
	hasTrustedStep := diagnostic.lastStep != ""
	if hasTrustedStep {
		failure.Step = diagnostic.lastStep
	}
	if diagnostic.errorCode != "" && diagnostic.errorCode != "NONE" {
		failure.Reason = strings.ToLower(diagnostic.errorCode)
	}
	if action == installer.UIPreparationActionPrepare {
		setClassification := func(errorKey, defaultStep string) {
			failure.ErrorKey = errorKey
			if !hasTrustedStep {
				failure.Step = defaultStep
			}
		}
		switch diagnostic.errorCode {
		case "PREFLIGHT_FAILED":
			setClassification("ui_preflight_failed", "preflight")
		case "TEMPLATE_LAYOUT_INVALID":
			setClassification("ui_template_layout_invalid", "preflight")
		case "API_UNAVAILABLE":
			setClassification("ui_api_unavailable", "preflight")
		case "INIT_BUSY":
			setClassification("ui_initialization_busy", "preflight")
		case "INIT_LEASE_FAILED":
			setClassification("ui_initialization_lease_failed", "preflight")
		case "INSTALL_STATE_DIR_INVALID":
			setClassification("ui_state_directory_invalid", "preflight")
		case "NODE_VERSION_UNSUPPORTED":
			setClassification("ui_node_version_unsupported", "launch")
		case "PNPM_VERSION_UNSUPPORTED":
			setClassification("ui_pnpm_version_unsupported", "launch")
		case "DEPENDENCY_INSTALL_FAILED", "DEPENDENCY_TRANSACTION_INVALID":
			setClassification("ui_dependency_install_failed", "dependencies")
		case "SOURCE_MOVE_STATE_INVALID", "INITIALIZATION_RESUME_INVALID":
			setClassification("ui_workspace_prepare_failed", "workspace")
		}
	}
	failure.DependencyLog = diagnostic.dependencyLog && failure.Step == "dependencies"
	return failure
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

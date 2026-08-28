package installplatform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestCommandUIInitializerRunsShellFreePrepareWithFilteredEnvironmentAndStableStages(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{stdout: strings.Join([]string{
		"arbitrary output must stay private",
		"INIT_STAGE=prepare:preflight",
		"INIT_STAGE=prepare:workspace",
		"INIT_STAGE=other:private-stage",
		"INIT_STAGE=prepare:dependencies",
		"INIT_STAGE=prepare:complete",
	}, "\n") + "\n"}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 9090, runner, []string{
		"PATH=/fixture/bin", "HOME=/fixture/home", "LANG=zh_CN.UTF-8", "LC_ALL=C",
		"USERPROFILE=C:\\Users\\fixture", "HOMEDRIVE=C:", "HOMEPATH=\\Users\\fixture",
		"PNPM_HOME=/fixture/pnpm", "COREPACK_HOME=/fixture/corepack",
		"DATABASE_PASSWORD=private-db", "NPM_TOKEN=private-npm",
		"NODE_OPTIONS=--require=/private/hook.mjs", "GIN_VBEN_INSTALL_STATE_DIR=/private/override",
		"GIN_VBEN_UI_SELECTION_MODE=legacy", "ADMIN_UI=naive", "APP_UI=antd",
	})
	if err != nil {
		t.Fatalf("newCommandUIInitializer() error = %v", err)
	}
	initializer.operatingSystem = "linux"

	var progress []installer.UIPreparationProgress
	if err := initializer.Prepare(context.Background(), installstate.UIEle, func(update installer.UIPreparationProgress) {
		progress = append(progress, update)
	}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	wantArgs := []string{"--dir", filepath.Join(root, "admin"), "run", "init", "--", "--ui", "ele", "--confirm-cleanup", "--no-open", "--port", "9090"}
	if runner.calls != 1 || runner.invocation.Name != "pnpm" || runner.invocation.Dir != root || !reflect.DeepEqual(runner.invocation.Args, wantArgs) {
		t.Fatalf("invocation = %#v, calls=%d, want pnpm %#v in %q", runner.invocation, runner.calls, wantArgs, root)
	}
	joinedEnvironment := strings.Join(runner.invocation.Env, "\n")
	for _, allowed := range []string{"PATH=/fixture/bin", "HOME=/fixture/home", "LANG=zh_CN.UTF-8", "LC_ALL=C", "USERPROFILE=C:\\Users\\fixture", "HOMEDRIVE=C:", "HOMEPATH=\\Users\\fixture", "PNPM_HOME=/fixture/pnpm", "COREPACK_HOME=/fixture/corepack"} {
		if !strings.Contains(joinedEnvironment, allowed) {
			t.Fatalf("filtered environment missing %q: %q", allowed, joinedEnvironment)
		}
	}
	if !strings.Contains(joinedEnvironment, "GIN_VBEN_INSTALL_STATE_DIR="+stateDirectory) {
		t.Fatalf("filtered environment missing fixed state directory %q: %q", stateDirectory, joinedEnvironment)
	}
	for _, blocked := range []string{"DATABASE_PASSWORD", "private-db", "NPM_TOKEN", "private-npm", "NODE_OPTIONS", "/private/hook", "/private/override", "GIN_VBEN_UI_SELECTION_MODE", "ADMIN_UI", "APP_UI"} {
		if strings.Contains(joinedEnvironment, blocked) {
			t.Fatalf("filtered environment contains %q: %q", blocked, joinedEnvironment)
		}
	}
	wantProgress := []installer.UIPreparationProgress{
		{CurrentStep: "preflight", Progress: 10},
		{CurrentStep: "workspace", Progress: 40},
		{CurrentStep: "dependencies", Progress: 75},
		{CurrentStep: "complete", Progress: 99},
	}
	if !reflect.DeepEqual(progress, wantProgress) {
		t.Fatalf("progress = %#v, want %#v", progress, wantProgress)
	}
	if initializer.LogPath() != ".runtime/install/dependency-install.log" {
		t.Fatalf("LogPath() = %q", initializer.LogPath())
	}
}

func TestCommandUIInitializerRunsShellFreeResetWithDedicatedStage(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{stdout: "INIT_STAGE=reset:preflight\r\nINIT_STAGE=reset:workspace\r\nINIT_STAGE=reset:complete\r\n"}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}
	initializer.operatingSystem = "linux"
	var progress []installer.UIPreparationProgress
	if err := initializer.Reset(context.Background(), func(update installer.UIPreparationProgress) {
		progress = append(progress, update)
	}); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	wantArgs := []string{"--dir", filepath.Join(root, "admin"), "run", "init", "--", "--reset", "--confirm-reset", "--no-open", "--port", "8080"}
	if runner.invocation.Name != "pnpm" || runner.invocation.Dir != root || !reflect.DeepEqual(runner.invocation.Args, wantArgs) {
		t.Fatalf("reset invocation = %#v, want args %#v", runner.invocation, wantArgs)
	}
	wantProgress := []installer.UIPreparationProgress{
		{CurrentStep: "preflight", Progress: 10},
		{CurrentStep: "reset", Progress: 60},
		{CurrentStep: "complete", Progress: 99},
	}
	if !reflect.DeepEqual(progress, wantProgress) {
		t.Fatalf("reset progress = %#v, want %#v", progress, wantProgress)
	}
}

func TestCommandUIInitializerRejectsInvalidConfigurationAndSelectionBeforeExecution(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	for _, port := range []int{0, 65536} {
		if _, err := NewCommandUIInitializer(root, stateDirectory, port); !errors.Is(err, ErrUIInitializerPortInvalid) {
			t.Fatalf("NewCommandUIInitializer(port=%d) error = %v, want ErrUIInitializerPortInvalid", port, err)
		}
	}
	if _, err := NewCommandUIInitializer("", stateDirectory, 8080); !errors.Is(err, ErrWorkspaceRootRequired) {
		t.Fatalf("NewCommandUIInitializer(empty) error = %v, want ErrWorkspaceRootRequired", err)
	}
	if _, err := NewCommandUIInitializer(t.TempDir(), stateDirectory, 8080); !errors.Is(err, ErrWorkspaceRootInvalid) {
		t.Fatalf("NewCommandUIInitializer(no admin) error = %v, want ErrWorkspaceRootInvalid", err)
	}

	runner := &uiInitializerCommandRunnerStub{}
	if _, err := NewCommandUIInitializer(root, "", 8080); !errors.Is(err, ErrUIInitializerStateDirectoryInvalid) {
		t.Fatalf("NewCommandUIInitializer(empty state directory) error = %v, want ErrUIInitializerStateDirectoryInvalid", err)
	}

	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := initializer.Prepare(context.Background(), installstate.UI("unknown"), nil); !errors.Is(err, installer.ErrUIPreparationInvalid) {
		t.Fatalf("Prepare(unknown) error = %v, want ErrUIPreparationInvalid", err)
	}
	if runner.calls != 0 {
		t.Fatalf("invalid UI executed %d commands", runner.calls)
	}
}

func TestCommandUIInitializerBoundsOutputAndDoesNotExposeCommandFailure(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	privateOutput := strings.Repeat("private-output", maxUIInitializerOutputBytes)
	runner := &uiInitializerCommandRunnerStub{
		stdout: privateOutput,
		stderr: privateOutput,
		err:    errors.New("exec /private/pnpm with TOKEN=secret"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}
	err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
	if !errors.Is(err, ErrUIInitializerCommandFailed) {
		t.Fatalf("Prepare() error = %v, want ErrUIInitializerCommandFailed", err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("Prepare() leaked command detail: %v", err)
	}
	if runner.stdoutAccepted != len(privateOutput) || runner.stderrAccepted != len(privateOutput) {
		t.Fatalf("runner writes = (%d, %d), want accepted input lengths (%d, %d)", runner.stdoutAccepted, runner.stderrAccepted, len(privateOutput), len(privateOutput))
	}
	if runner.stdoutRetained > maxUIInitializerOutputBytes || runner.stderrRetained > maxUIInitializerOutputBytes {
		t.Fatalf("retained output = (%d, %d), max=%d", runner.stdoutRetained, runner.stderrRetained, maxUIInitializerOutputBytes)
	}
}

func TestCommandUIInitializerStderrOnlyEarlyExitKeepsStableLaunchFailure(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stderr: "INIT_ERROR=PNPM_VERSION_UNSUPPORTED\nnode: executable was not found at C:\\private\\secret\\node.exe\nTOKEN=secret\n",
		err:    errors.New("exit status 1 with private command details"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}
	err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
	var failure *installer.UIPreparationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
	}
	if failure.ErrorKey != "ui_pnpm_version_unsupported" || failure.Step != "launch" || failure.Reason != "pnpm_version_unsupported" {
		t.Fatalf("stderr-only early exit failure = %#v, want stable launch classification", failure)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("stderr-only early exit leaked command detail: %v", err)
	}
}

func TestCommandUIInitializerReturnsAllowlistedStructuredFailure(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stdout: strings.Join([]string{
			"INIT_STAGE=prepare:preflight",
			"INIT_PREFLIGHT=failed",
			"INIT_FAILURE_SCOPE=admin_apps",
			"INIT_FAILURE_OPERATION=cross_directory_rename",
			"INIT_REASON=NONE",
			"INIT_ERROR=PREFLIGHT_FAILED",
		}, "\n") + "\n",
		err: errors.New("exit status 3 with /private/path TOKEN=secret"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}

	err = initializer.Prepare(context.Background(), installstate.UIEle, nil)
	var failure *installer.UIPreparationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
	}
	if failure.ErrorKey != "ui_preflight_failed" || failure.Step != "preflight" ||
		failure.Reason != "preflight_failed" || failure.Scope != "admin_apps" ||
		failure.Operation != "cross_directory_rename" || failure.DependencyLog {
		t.Fatalf("structured failure = %#v", failure)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("structured failure leaked process detail: %v", err)
	}
}

func TestCommandUIInitializerMarksDependencyLogOnlyAfterDependencyStage(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stdout: "INIT_STAGE=prepare:dependencies\nINIT_DEPENDENCY_LOG=.runtime/install/dependency-install.log\nINIT_ERROR=DEPENDENCY_INSTALL_FAILED\n",
		err:    errors.New("exit status 1"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}

	err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
	var failure *installer.UIPreparationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
	}
	if failure.ErrorKey != "ui_dependency_install_failed" || failure.Step != "dependencies" ||
		failure.Reason != "dependency_install_failed" || !failure.DependencyLog {
		t.Fatalf("dependency failure = %#v", failure)
	}
}

func TestCommandUIInitializerKeepsTrustedDependencyStageWhenBusy(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stdout: "INIT_STAGE=prepare:dependencies\nINIT_DEPENDENCY_LOG=.runtime/install/dependency-install.log\nINIT_ERROR=INIT_BUSY\n",
		err:    errors.New("exit status 3"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}

	err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
	var failure *installer.UIPreparationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
	}
	if failure.ErrorKey != "ui_initialization_busy" || failure.Step != "dependencies" ||
		failure.Reason != "init_busy" || !failure.DependencyLog {
		t.Fatalf("busy dependency failure = %#v", failure)
	}
}

func TestCommandUIInitializerDiscardsUnknownMachineDiagnosticIdentifiers(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stdout: "INIT_ERROR=TOKEN_SECRET_PAYLOAD\nINIT_REASON=ENCODED_PRIVATE_VALUE\n",
		err:    errors.New("exit status 1"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}

	err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
	var failure *installer.UIPreparationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
	}
	if failure.ErrorKey != "ui_prepare_failed" || failure.Step != "launch" ||
		failure.Reason != "process_failed" {
		t.Fatalf("unknown diagnostic was not discarded: %#v", failure)
	}
}

func TestCommandUIInitializerKeepsAllowlistedInterruptedAndOperationReasons(t *testing.T) {
	for _, code := range []string{"RESET_IN_PROGRESS", "STATE_INCONSISTENT", "INITIALIZATION_IN_PROGRESS", "INITIALIZATION_OPERATION_FAILED"} {
		t.Run(code, func(t *testing.T) {
			root := uiInitializerWorkspaceFixture(t)
			stateDirectory := uiInitializerStateDirectoryFixture(t)
			runner := &uiInitializerCommandRunnerStub{
				stdout: "INIT_ERROR=" + code + "\n",
				err:    errors.New("exit status 3"),
			}
			initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
			if err != nil {
				t.Fatal(err)
			}

			err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
			var failure *installer.UIPreparationFailure
			if !errors.As(err, &failure) {
				t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
			}
			if failure.Reason != strings.ToLower(code) {
				t.Fatalf("failure reason = %q, want %q", failure.Reason, strings.ToLower(code))
			}
		})
	}
}

func TestCommandUIInitializerClassifiesUnsupportedPNPMBeforePreparation(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stdout: "INIT_REASON=NONE\nINIT_ERROR=PNPM_VERSION_UNSUPPORTED\n",
		err:    errors.New("exit status 1"),
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}

	err = initializer.Prepare(context.Background(), installstate.UIAntd, nil)
	var failure *installer.UIPreparationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepare() error = %T %v, want *UIPreparationFailure", err, err)
	}
	if failure.ErrorKey != "ui_pnpm_version_unsupported" || failure.Step != "launch" ||
		failure.Reason != "pnpm_version_unsupported" || failure.DependencyLog {
		t.Fatalf("pnpm failure = %#v", failure)
	}
}

func TestCommandUIInitializerPreservesContextCancellation(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{err: context.Canceled}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=/fixture/bin"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := initializer.Reset(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reset(cancelled) error = %v, want context.Canceled", err)
	}
}

func TestBuildUIInitializerCommandUsesDirectNodeArgvOnWindows(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Gin Vben Admin")
	args := []string{"--ui", "ele", "--confirm-cleanup", "--no-open", "--port", "8080"}
	invocation, err := buildUIInitializerCommand("windows", root, args, []string{"Path=C:\\bin"})
	if err != nil {
		t.Fatalf("buildUIInitializerCommand(windows) error = %v", err)
	}
	wantScript := filepath.Join(root, "admin", "scripts", "init.mjs")
	wantArgs := append([]string{wantScript}, args...)
	if invocation.Name != "node" || invocation.Dir != root || !reflect.DeepEqual(invocation.Args, wantArgs) {
		t.Fatalf("windows invocation = %#v, want direct node argv %#v in %q", invocation, wantArgs, root)
	}
	joined := strings.Join(invocation.Args, "\x00")
	if strings.Contains(joined, "pnpm.cmd") || strings.Contains(joined, "pnpm.exe") || strings.Contains(joined, "cmd.exe") {
		t.Fatalf("windows invocation unexpectedly depends on a shell or pnpm shim: %#v", invocation)
	}
	specialRoot := filepath.Join(root, "fixture&calc")
	special, err := buildUIInitializerCommand("windows", specialRoot, args, nil)
	if err != nil || special.Name != "node" || !strings.Contains(special.Args[0], "fixture&calc") {
		t.Fatalf("Windows special-character path invocation = %#v, err=%v", special, err)
	}

	unix, err := buildUIInitializerCommand("linux", "/fixture root", args, nil)
	if err != nil || unix.Name != "pnpm" || !reflect.DeepEqual(unix.Args, args) {
		t.Fatalf("unix invocation = (%#v, %v), want direct pnpm argv", unix, err)
	}
}

func TestBuildUIInitializerCommandWindowsShimChoiceDoesNotChangeNodeInvocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Gin Vben Admin")
	args := []string{"--ui", "antd", "--confirm-cleanup", "--no-open", "--port", "8080"}
	var baseline uiInitializerCommand
	for index, shimPath := range []string{`C:\tools\pnpm-cmd-only`, `C:\tools\pnpm-exe-only`} {
		invocation, err := buildUIInitializerCommand("windows", root, args, []string{"PATH=" + shimPath})
		if err != nil {
			t.Fatalf("buildUIInitializerCommand(windows, shim=%q) error = %v", shimPath, err)
		}
		if index == 0 {
			baseline = invocation
			continue
		}
		if invocation.Name != baseline.Name || !reflect.DeepEqual(invocation.Args, baseline.Args) || invocation.Dir != baseline.Dir {
			t.Fatalf("Windows shim choice changed initializer invocation: baseline=%#v got=%#v", baseline, invocation)
		}
	}
}

func TestCommandUIInitializerUsesNodeEntrypointOnWindows(t *testing.T) {
	root := uiInitializerWorkspaceFixture(t)
	stateDirectory := uiInitializerStateDirectoryFixture(t)
	runner := &uiInitializerCommandRunnerStub{
		stdout: "INIT_STAGE=prepare:preflight\nINIT_STAGE=prepare:complete\n",
	}
	initializer, err := newCommandUIInitializer(root, stateDirectory, 8080, runner, []string{"PATH=C:\\tools"})
	if err != nil {
		t.Fatal(err)
	}
	initializer.operatingSystem = "windows"
	if err := initializer.Prepare(context.Background(), installstate.UIEle, nil); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	want := []string{
		filepath.Join(root, "admin", "scripts", "init.mjs"),
		"--ui", "ele", "--confirm-cleanup", "--no-open", "--port", "8080",
	}
	if runner.invocation.Name != "node" || !reflect.DeepEqual(runner.invocation.Args, want) {
		t.Fatalf("Windows prepare invocation = %#v, want node argv %#v", runner.invocation, want)
	}
}

func uiInitializerWorkspaceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "admin"), 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func uiInitializerStateDirectoryFixture(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "configured", "install")
	absolute, err := canonicalizeUIInitializerStateDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

type uiInitializerCommandRunnerStub struct {
	invocation     uiInitializerCommand
	stdout         string
	stderr         string
	err            error
	calls          int
	stdoutAccepted int
	stderrAccepted int
	stdoutRetained int
	stderrRetained int
}

func (r *uiInitializerCommandRunnerStub) Run(_ context.Context, invocation uiInitializerCommand, stdout, stderr io.Writer) error {
	r.calls++
	r.invocation = invocation
	r.stdoutAccepted, _ = stdout.Write([]byte(r.stdout))
	r.stderrAccepted, _ = stderr.Write([]byte(r.stderr))
	if retained, ok := stdout.(interface{ Len() int }); ok {
		r.stdoutRetained = retained.Len()
	}
	if retained, ok := stderr.(interface{ Len() int }); ok {
		r.stderrRetained = retained.Len()
	}
	return r.err
}

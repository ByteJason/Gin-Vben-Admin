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
		"PNPM_HOME=/fixture/pnpm", "DATABASE_PASSWORD=private-db", "NPM_TOKEN=private-npm",
		"NODE_OPTIONS=--require=/private/hook.mjs", "GIN_VBEN_INSTALL_STATE_DIR=/private/override",
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
	for _, allowed := range []string{"PATH=/fixture/bin", "HOME=/fixture/home", "LANG=zh_CN.UTF-8", "LC_ALL=C", "PNPM_HOME=/fixture/pnpm"} {
		if !strings.Contains(joinedEnvironment, allowed) {
			t.Fatalf("filtered environment missing %q: %q", allowed, joinedEnvironment)
		}
	}
	if !strings.Contains(joinedEnvironment, "GIN_VBEN_INSTALL_STATE_DIR="+stateDirectory) {
		t.Fatalf("filtered environment missing fixed state directory %q: %q", stateDirectory, joinedEnvironment)
	}
	for _, blocked := range []string{"DATABASE_PASSWORD", "private-db", "NPM_TOKEN", "private-npm", "NODE_OPTIONS", "/private/hook", "/private/override"} {
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

func TestBuildUIInitializerCommandUsesFixedWindowsShellForPNPMCommandFile(t *testing.T) {
	root := `C:\fixture root`
	args := []string{"--dir", `C:\fixture root\admin`, "run", "init", "--", "--ui", "ele", "--confirm-cleanup", "--no-open", "--port", "8080"}
	invocation, err := buildUIInitializerCommand("windows", root, args, []string{"Path=C:\\bin"})
	if err != nil {
		t.Fatalf("buildUIInitializerCommand(windows) error = %v", err)
	}
	if invocation.Name != "cmd.exe" || invocation.Dir != root || len(invocation.Args) != 4 || !reflect.DeepEqual(invocation.Args[:3], []string{"/d", "/s", "/c"}) {
		t.Fatalf("windows invocation = %#v", invocation)
	}
	commandLine := invocation.Args[3]
	for _, expected := range []string{"call pnpm.cmd", `"C:\fixture root\admin"`, `"--ui"`, `"ele"`} {
		if !strings.Contains(commandLine, expected) {
			t.Fatalf("windows command line missing %q: %q", expected, commandLine)
		}
	}
	if _, err := buildUIInitializerCommand("windows", `C:\fixture&calc`, args, nil); !errors.Is(err, ErrWorkspaceRootInvalid) {
		t.Fatalf("unsafe Windows path error = %v, want ErrWorkspaceRootInvalid", err)
	}

	unix, err := buildUIInitializerCommand("linux", "/fixture root", args, nil)
	if err != nil || unix.Name != "pnpm" || !reflect.DeepEqual(unix.Args, args) {
		t.Fatalf("unix invocation = (%#v, %v), want direct pnpm argv", unix, err)
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

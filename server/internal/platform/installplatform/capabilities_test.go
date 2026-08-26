package installplatform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCapabilityProbeReportsAllowlistedToolsWithoutLeakingPaths(t *testing.T) {
	runner := commandRunnerStub{
		outputs: map[string]string{
			"go":   "go version go1.24.6 darwin/arm64\n",
			"node": "v24.18.0\n",
			"pnpm": "11.16.0\n",
		},
		errors: map[string]error{
			"docker": errors.New("look /private/example/docker: file does not exist"),
		},
	}
	probe := NewCapabilityProbe(runner, "darwin", "arm64")

	got, err := probe.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got.Platform.OS != "darwin" || got.Platform.Arch != "arm64" {
		t.Fatalf("platform = %#v", got.Platform)
	}
	if len(got.Tools) != 4 {
		t.Fatalf("tools = %#v, want four allowlisted tools", got.Tools)
	}
	for index, id := range []string{"go", "node", "pnpm", "docker"} {
		if got.Tools[index].ID != id {
			t.Fatalf("tools[%d].ID = %q, want %q", index, got.Tools[index].ID, id)
		}
	}
	if !got.Tools[0].Available || got.Tools[0].Version != "go1.24.6" {
		t.Fatalf("Go capability = %#v", got.Tools[0])
	}
	if got.Tools[3].Available || got.Tools[3].Reason != "not_available" {
		t.Fatalf("Docker capability = %#v", got.Tools[3])
	}
	encoded := got.String()
	if strings.Contains(encoded, "/private/example") || strings.Contains(encoded, "file does not exist") {
		t.Fatalf("capabilities leaked runner error: %s", encoded)
	}
}

func TestCapabilityProbeHonorsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewCapabilityProbe(commandRunnerStub{}, "linux", "amd64").Probe(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe() error = %v, want context.Canceled", err)
	}
}

func TestCapabilityProbeMarksUnsupportedRequiredToolVersions(t *testing.T) {
	runner := commandRunnerStub{outputs: map[string]string{
		"go": "go version go1.24.6 linux/amd64\n", "node": "v24.11.0\n",
		"pnpm": "10.14.0\n", "docker": "Docker version 29.0.0, build fixture\n",
	}}

	got, err := NewCapabilityProbe(runner, "windows", "amd64").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	node := got.Tools[1]
	pnpm := got.Tools[2]
	if !node.Available || node.Compatible || node.RequiredVersion != "^22.18.0 || ^24.12.0" || node.Reason != "version_unsupported" {
		t.Fatalf("Node capability = %#v", node)
	}
	if !pnpm.Available || pnpm.Compatible || pnpm.RequiredVersion != ">=11.0.0" || pnpm.Reason != "version_unsupported" {
		t.Fatalf("pnpm capability = %#v", pnpm)
	}
}

func TestCapabilityProbeRejectsUnboundedOrPathLikeVersionTokens(t *testing.T) {
	runner := commandRunnerStub{outputs: map[string]string{
		"go":     "go version go1.24.6/Users/example/private linux/amd64\n",
		"node":   "v" + strings.Repeat("2", 80) + "\n",
		"pnpm":   "11.16.0\\private\\wrapper\n",
		"docker": "Docker version 29.0.0/var/private, build fixture\n",
	}}

	got, err := NewCapabilityProbe(runner, "linux", "amd64").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range got.Tools {
		if tool.Available || tool.Version != "" || tool.Reason != "version_unreadable" {
			t.Fatalf("capability %q = %#v, want bounded credential-free unreadable result", tool.ID, tool)
		}
	}
}

func TestCapabilityProbeRejectsNonVersionSingleTokens(t *testing.T) {
	runner := commandRunnerStub{outputs: map[string]string{
		"go":     "go version goPASSWORD linux/amd64\n",
		"node":   "password\n",
		"pnpm":   "token_secret\n",
		"docker": "Docker version credential, build fixture\n",
	}}

	got, err := NewCapabilityProbe(runner, "linux", "amd64").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range got.Tools {
		if tool.Available || tool.Version != "" || tool.Reason != "version_unreadable" {
			t.Fatalf("capability %q = %#v, want a rejected non-version token", tool.ID, tool)
		}
	}
	if strings.Contains(got.String(), "password") || strings.Contains(got.String(), "secret") || strings.Contains(got.String(), "credential") {
		t.Fatalf("capabilities leaked non-version runner output: %s", got.String())
	}
}

func TestCapabilityCommandInvocationUsesWindowsShellForPNPMShims(t *testing.T) {
	command, args := capabilityCommandInvocation("windows", "pnpm", []string{"--version"})
	if command == "pnpm" || len(args) != 5 || args[0] != "/d" || args[1] != "/s" || args[2] != "/c" || args[3] != "pnpm" || args[4] != "--version" {
		t.Fatalf("Windows pnpm invocation = (%q, %#v), want cmd.exe /d /s /c pnpm --version", command, args)
	}

	command, args = capabilityCommandInvocation("darwin", "pnpm", []string{"--version"})
	if command != "pnpm" || len(args) != 1 || args[0] != "--version" {
		t.Fatalf("POSIX pnpm invocation = (%q, %#v), want direct argv", command, args)
	}
}

type commandRunnerStub struct {
	outputs map[string]string
	errors  map[string]error
}

func (s commandRunnerStub) Version(_ context.Context, command string, _ ...string) (string, error) {
	if err := s.errors[command]; err != nil {
		return "", err
	}
	return s.outputs[command], nil
}

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

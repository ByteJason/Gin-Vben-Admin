package main

import (
	"path/filepath"
	"testing"
)

func TestParseCommandUsesLoopbackAndValidatedPort(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"--assets", "../admin/apps/install/dist", "--port", "9191", "--config", "configs/server.example.yaml"})
	if err != nil {
		t.Fatalf("parseCommand() error = %v", err)
	}
	if command.addr != "127.0.0.1:9191" || command.assets != filepath.Clean("../admin/apps/install/dist") || command.configPath != "configs/server.example.yaml" {
		t.Fatalf("parseCommand() = %#v", command)
	}
}

func TestParseCommandDefaultsToLoopback8080(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"--assets", "../admin/apps/install/dist"})
	if err != nil {
		t.Fatalf("parseCommand() error = %v", err)
	}
	if command.addr != "127.0.0.1:8080" {
		t.Fatalf("addr = %q, want loopback default", command.addr)
	}
}

func TestParseCommandRejectsMissingAssetsInvalidPortsAndUnexpectedArguments(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{},
		{"--assets", ""},
		{"--assets", "fixture", "--port", "0"},
		{"--assets", "fixture", "--port", "65536"},
		{"--assets", "fixture", "--host", "0.0.0.0"},
		{"--assets", "fixture", "extra"},
	} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%q) error = nil, want rejection", args)
		}
	}
}

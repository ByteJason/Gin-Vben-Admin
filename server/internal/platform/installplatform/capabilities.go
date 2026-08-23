package installplatform

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
)

type CommandRunner interface {
	Version(context.Context, string, ...string) (string, error)
}

type CapabilityProbe struct {
	runner CommandRunner
	os     string
	arch   string
}

func NewCapabilityProbe(runner CommandRunner, operatingSystem, architecture string) *CapabilityProbe {
	return &CapabilityProbe{runner: runner, os: operatingSystem, arch: architecture}
}

func NewSystemCapabilityProbe() *CapabilityProbe {
	return NewCapabilityProbe(systemCommandRunner{}, runtime.GOOS, runtime.GOARCH)
}

func (p *CapabilityProbe) Probe(ctx context.Context) (installer.Capabilities, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.Capabilities{}, err
	}
	if p == nil || p.runner == nil {
		return installer.Capabilities{}, errors.New("capability command runner is not configured")
	}

	result := installer.Capabilities{
		Platform: installer.PlatformCapability{OS: p.os, Arch: p.arch},
		Tools:    make([]installer.ToolCapability, 0, 4),
	}
	for _, tool := range []struct {
		id   string
		args []string
	}{
		{id: "go", args: []string{"version"}},
		{id: "node", args: []string{"--version"}},
		{id: "pnpm", args: []string{"--version"}},
		{id: "docker", args: []string{"--version"}},
	} {
		output, err := p.runner.Version(ctx, tool.id, tool.args...)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return installer.Capabilities{}, ctxErr
		}
		capability := installer.ToolCapability{ID: tool.id}
		if err != nil {
			capability.Reason = "not_available"
		} else {
			capability.Available = true
			capability.Version = parseToolVersion(tool.id, output)
			if capability.Version == "" {
				capability.Available = false
				capability.Reason = "version_unreadable"
			}
		}
		result.Tools = append(result.Tools, capability)
	}
	return result, nil
}

type systemCommandRunner struct{}

func (systemCommandRunner) Version(ctx context.Context, command string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, command, args...).Output()
	if err != nil {
		return "", err
	}
	if len(output) > 512 {
		output = output[:512]
	}
	return string(output), nil
}

func parseToolVersion(tool, output string) string {
	fields := strings.FieldsFunc(strings.TrimSpace(output), func(r rune) bool {
		return unicode.IsSpace(r) || r == ','
	})
	if len(fields) == 0 {
		return ""
	}
	switch tool {
	case "go":
		if len(fields) >= 3 && strings.HasPrefix(fields[2], "go") {
			return fields[2]
		}
	case "docker":
		for index, field := range fields {
			if field == "version" && index+1 < len(fields) {
				return fields[index+1]
			}
		}
	default:
		return fields[0]
	}
	return ""
}

package installplatform

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
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
		capability := installer.ToolCapability{ID: tool.id, RequiredVersion: requiredToolVersion(tool.id)}
		if err != nil {
			capability.Reason = "not_available"
		} else {
			capability.Available = true
			capability.Version = parseToolVersion(tool.id, output)
			if capability.Version == "" {
				capability.Available = false
				capability.Reason = "version_unreadable"
			} else if !compatibleToolVersion(tool.id, capability.Version) {
				capability.Reason = "version_unsupported"
			} else {
				capability.Compatible = true
			}
		}
		result.Tools = append(result.Tools, capability)
	}
	return result, nil
}

func requiredToolVersion(tool string) string {
	switch tool {
	case "node":
		return "^22.18.0 || ^24.12.0"
	case "pnpm":
		return ">=11.0.0"
	default:
		return ""
	}
}

func compatibleToolVersion(tool, version string) bool {
	if tool != "node" && tool != "pnpm" {
		return true
	}
	major, minor, patch, ok := numericToolVersion(version)
	if !ok {
		return false
	}
	if tool == "pnpm" {
		return major >= 11
	}
	return major == 22 && versionAtLeast(minor, patch, 18, 0) ||
		major == 24 && versionAtLeast(minor, patch, 12, 0)
}

func numericToolVersion(version string) (int, int, int, bool) {
	value := strings.TrimPrefix(strings.TrimPrefix(version, "go"), "v")
	parts := strings.SplitN(value, ".", 4)
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	numbers := [3]int{}
	for index := 0; index < len(numbers); index++ {
		if index >= len(parts) {
			break
		}
		digits := parts[index]
		if suffix := strings.IndexFunc(digits, func(character rune) bool { return character < '0' || character > '9' }); suffix >= 0 {
			digits = digits[:suffix]
		}
		if digits == "" {
			return 0, 0, 0, false
		}
		parsed, err := strconv.Atoi(digits)
		if err != nil {
			return 0, 0, 0, false
		}
		numbers[index] = parsed
	}
	return numbers[0], numbers[1], numbers[2], true
}

func versionAtLeast(minor, patch, requiredMinor, requiredPatch int) bool {
	return minor > requiredMinor || minor == requiredMinor && patch >= requiredPatch
}

type systemCommandRunner struct{}

var (
	goVersionPattern     = regexp.MustCompile(`^go[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[a-z]+[0-9]*)?$`)
	semverVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

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
	var candidate string
	switch tool {
	case "go":
		if len(fields) >= 3 && strings.HasPrefix(fields[2], "go") {
			candidate = fields[2]
		}
	case "docker":
		for index, field := range fields {
			if field == "version" && index+1 < len(fields) {
				candidate = fields[index+1]
				break
			}
		}
	default:
		candidate = fields[0]
	}
	return safeToolVersion(tool, candidate)
}

// safeToolVersion keeps capability responses inside the public OpenAPI bound
// and rejects separators/control data that could expose wrapper paths.
func safeToolVersion(tool, version string) string {
	if version == "" || len(version) > 64 {
		return ""
	}
	for _, character := range version {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' {
			continue
		}
		switch character {
		case '.', '+', '-', '_':
			continue
		default:
			return ""
		}
	}
	if tool == "go" {
		if !goVersionPattern.MatchString(version) {
			return ""
		}
	} else if !semverVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

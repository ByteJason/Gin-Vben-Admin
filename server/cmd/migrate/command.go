package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type action string

const (
	actionUp                  action = "up"
	actionDown                action = "down"
	actionStatus              action = "status"
	actionSettingsMailCleanup action = "settings-mail-cleanup"
)

type command struct {
	action     action
	configPath string
	steps      uint
}

func parseCommand(args []string) (command, error) {
	actionValue, flagArgs, err := splitAction(args)
	if err != nil {
		return command{}, err
	}

	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "path to server YAML configuration")
	steps := flags.Uint("steps", 1, "number of migration steps to revert")
	if err := flags.Parse(flagArgs); err != nil {
		return command{}, fmt.Errorf("parse migrate options: %w", err)
	}
	if flags.NArg() != 0 {
		return command{}, fmt.Errorf("unexpected migrate arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *steps == 0 {
		return command{}, errors.New("migration steps must be positive")
	}
	if actionValue != actionDown && containsStepsFlag(flagArgs) {
		return command{}, errors.New("--steps is only valid for down migrations")
	}

	return command{action: actionValue, configPath: *configPath, steps: *steps}, nil
}

func splitAction(args []string) (action, []string, error) {
	for index := 0; index < len(args); index++ {
		value := args[index]
		if value == "--config" || value == "--steps" {
			index++
			continue
		}
		if strings.HasPrefix(value, "--config=") || strings.HasPrefix(value, "--steps=") {
			continue
		}
		if parsed, ok := parseAction(value); ok {
			remaining := make([]string, 0, len(args)-1)
			remaining = append(remaining, args[:index]...)
			remaining = append(remaining, args[index+1:]...)
			return parsed, remaining, nil
		}
	}
	if len(args) == 0 {
		return "", nil, errors.New("migration action is required: up, down, status, or settings-mail-cleanup")
	}
	return "", nil, fmt.Errorf("invalid migration action %q: use up, down, status, or settings-mail-cleanup", args[0])
}

// parseAction accepts the canonical cleanup command plus short aliases used by
// deployment scripts. Returning one canonical action keeps output and dispatch
// stable while still exposing the immutable v003 migration name.
func parseAction(value string) (action, bool) {
	switch action(strings.ToLower(strings.TrimSpace(value))) {
	case actionUp:
		return actionUp, true
	case actionDown:
		return actionDown, true
	case actionStatus:
		return actionStatus, true
	case actionSettingsMailCleanup, "cleanup-settings-mail", "v003", "up-v003", "v003-settings-mail-cleanup":
		return actionSettingsMailCleanup, true
	default:
		return "", false
	}
}

func containsStepsFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--steps" || strings.HasPrefix(arg, "--steps=") {
			return true
		}
	}
	return false
}

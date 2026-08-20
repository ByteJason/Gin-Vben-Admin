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
	actionUp     action = "up"
	actionDown   action = "down"
	actionStatus action = "status"
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
		switch action(value) {
		case actionUp, actionDown, actionStatus:
			remaining := make([]string, 0, len(args)-1)
			remaining = append(remaining, args[:index]...)
			remaining = append(remaining, args[index+1:]...)
			return action(value), remaining, nil
		}
	}
	if len(args) == 0 {
		return "", nil, errors.New("migration action is required: up, down, or status")
	}
	return "", nil, fmt.Errorf("invalid migration action %q: use up, down, or status", args[0])
}

func containsStepsFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--steps" || strings.HasPrefix(arg, "--steps=") {
			return true
		}
	}
	return false
}

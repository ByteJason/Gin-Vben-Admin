package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
)

type action string

const actionReset action = "reset"

type command struct {
	action     action
	configPath string
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return command{}, errors.New("admin password action is required: reset")
	}
	if action(args[0]) != actionReset {
		return command{}, fmt.Errorf("invalid admin password action %q: use reset", args[0])
	}

	flags := flag.NewFlagSet("admin-password reset", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "path to server YAML configuration")
	if err := flags.Parse(args[1:]); err != nil {
		return command{}, fmt.Errorf("parse admin password options: %w", err)
	}
	if flags.NArg() != 0 {
		return command{}, errors.New("unexpected admin password arguments")
	}

	return command{action: actionReset, configPath: *configPath}, nil
}

func readConfirmedPassword(input io.Reader) (string, error) {
	if input == nil {
		return "", errors.New("password input is required")
	}

	// The installer policy is bounded below bcrypt's 72-byte ceiling. Keep the
	// input buffer small as well, so an accidental stream cannot allocate an
	// unbounded amount of memory before application validation runs.
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 128), 256)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", errors.New("read password input")
		}
		return "", errors.New("password input is required")
	}
	password := scanner.Text()
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", errors.New("read password confirmation")
		}
		return "", errors.New("password confirmation is required")
	}
	if password != scanner.Text() {
		return "", errors.New("password confirmation does not match")
	}
	return password, nil
}

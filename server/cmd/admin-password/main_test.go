package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseCommandAcceptsResetWithOptionalConfig(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		want command
	}{
		{name: "defaults", args: []string{"reset"}, want: command{action: actionReset}},
		{name: "separate config", args: []string{"reset", "--config", "test.yaml"}, want: command{action: actionReset, configPath: "test.yaml"}},
		{name: "inline config", args: []string{"reset", "--config=test.yaml"}, want: command{action: actionReset, configPath: "test.yaml"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCommand(test.args)
			if err != nil {
				t.Fatalf("parseCommand(%q) error = %v", test.args, err)
			}
			if got != test.want {
				t.Fatalf("parseCommand(%q) = %#v, want %#v", test.args, got, test.want)
			}
		})
	}
}

func TestParseCommandRejectsMissingUnknownAndPlaintextArguments(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{},
		{"change"},
		{"reset", "Secret1"},
		{"reset", "--password", "Secret1"},
		{"reset", "--config"},
	} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%q) error = nil, want validation error", args)
		}
	}
}

func TestReadConfirmedPasswordRequiresTwoMatchingLines(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "unix lines", input: "Abcd12\nAbcd12\n", want: "Abcd12"},
		{name: "windows lines", input: "Abcd12\r\nAbcd12\r\n", want: "Abcd12"},
		{name: "final line without newline", input: "Abcd12\nAbcd12", want: "Abcd12"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := readConfirmedPassword(strings.NewReader(test.input))
			if err != nil {
				t.Fatalf("readConfirmedPassword() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("readConfirmedPassword() = %q, want %q", got, test.want)
			}
		})
	}

	for _, input := range []string{"", "Abcd12\n", "Abcd12\nAbcd13\n"} {
		if _, err := readConfirmedPassword(strings.NewReader(input)); err == nil {
			t.Fatalf("readConfirmedPassword(%q) error = nil, want confirmation error", input)
		}
	}
}

func TestRunResetsUsingConfirmedStdinWithoutEchoingSensitiveValues(t *testing.T) {
	t.Parallel()

	const password = "Sensitive9"
	const account = "private-admin"
	runtime := &stubResetRuntime{account: account}
	var requestedConfig string
	factory := func(path string) (resetRuntime, error) {
		requestedConfig = path
		return runtime, nil
	}
	var output bytes.Buffer

	exitCode := run(context.Background(), []string{"reset", "--config", "private.yaml"}, strings.NewReader(password+"\n"+password+"\n"), &output, factory)
	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, output = %q", exitCode, output.String())
	}
	if requestedConfig != "private.yaml" {
		t.Fatalf("factory config path = %q", requestedConfig)
	}
	if runtime.password != password || runtime.resetCalls != 1 || runtime.closeCalls != 1 {
		t.Fatalf("runtime = %#v", runtime)
	}
	wantOutput := "ADMIN_PASSWORD_RESET=OK\nLOGIN_FAILURE_STATE_RESET=OK\n"
	if output.String() != wantOutput {
		t.Fatalf("output = %q, want %q", output.String(), wantOutput)
	}
	for _, secret := range []string{password, account, "private.yaml"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("output leaked sensitive value %q: %q", secret, output.String())
		}
	}
}

func TestRunMapsFailuresToBoundedMachineReadableOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		input      string
		factoryErr error
		runtime    *stubResetRuntime
		wantCode   int
		wantError  string
	}{
		{name: "command", args: []string{"reset", "plaintext"}, input: "Abcd12\nAbcd12\n", wantCode: 2, wantError: "invalid_command"},
		{name: "input", args: []string{"reset"}, input: "Abcd12\nDifferent1\n", wantCode: 2, wantError: "input"},
		{name: "initialize", args: []string{"reset"}, input: "Abcd12\nAbcd12\n", factoryErr: errors.New("dsn=secret"), wantCode: 1, wantError: "initialize"},
		{name: "operation", args: []string{"reset"}, input: "Abcd12\nAbcd12\n", runtime: &stubResetRuntime{resetErr: errors.New("account private-admin")}, wantCode: 1, wantError: "operation"},
		{name: "close", args: []string{"reset"}, input: "Abcd12\nAbcd12\n", runtime: &stubResetRuntime{closeErr: errors.New("password Sensitive9")}, wantCode: 1, wantError: "close"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			factory := func(string) (resetRuntime, error) {
				if test.factoryErr != nil {
					return nil, test.factoryErr
				}
				if test.runtime != nil {
					return test.runtime, nil
				}
				return &stubResetRuntime{}, nil
			}
			var output bytes.Buffer
			gotCode := run(context.Background(), test.args, strings.NewReader(test.input), &output, factory)
			if gotCode != test.wantCode {
				t.Fatalf("run() exit code = %d, want %d", gotCode, test.wantCode)
			}
			want := "ADMIN_PASSWORD_RESET=ERROR\nADMIN_PASSWORD_RESET_ERROR=" + test.wantError + "\n"
			if output.String() != want {
				t.Fatalf("output = %q, want %q", output.String(), want)
			}
			for _, secret := range []string{"Sensitive9", "private-admin", "dsn=secret", "plaintext"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("output leaked %q: %q", secret, output.String())
				}
			}
		})
	}
}

type stubResetRuntime struct {
	account    string
	password   string
	resetErr   error
	closeErr   error
	resetCalls int
	closeCalls int
}

func (s *stubResetRuntime) Reset(_ context.Context, password string) error {
	s.password = password
	s.resetCalls++
	return s.resetErr
}

func (s *stubResetRuntime) Close() error {
	s.closeCalls++
	return s.closeErr
}

func TestSuccessOutputIsStable(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writeSuccess(&output)
	want := []string{"ADMIN_PASSWORD_RESET=OK", "LOGIN_FAILURE_STATE_RESET=OK", ""}
	if got := strings.Split(output.String(), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("writeSuccess() lines = %#v, want %#v", got, want)
	}
}

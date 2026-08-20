package main

import "testing"

func TestParseCommandAcceptsEachMigrationAction(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		want command
	}{
		{name: "up after flags", args: []string{"--config", "test.yaml", "up"}, want: command{action: actionUp, configPath: "test.yaml", steps: 1}},
		{name: "config value matching action", args: []string{"--config", "status", "up"}, want: command{action: actionUp, configPath: "status", steps: 1}},
		{name: "status before flags", args: []string{"status", "--config=test.yaml"}, want: command{action: actionStatus, configPath: "test.yaml", steps: 1}},
		{name: "down defaults to one", args: []string{"down"}, want: command{action: actionDown, steps: 1}},
		{name: "down uses requested steps", args: []string{"down", "--steps", "2"}, want: command{action: actionDown, steps: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
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

func TestParseCommandRejectsInvalidMigrationAction(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{}, {"reset"}, {"down", "--steps", "0"}, {"up", "--steps", "2"}} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%q) error = nil, want validation error", args)
		}
	}
}

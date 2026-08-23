package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

type command struct {
	addr       string
	assets     string
	configPath string
}

func parseCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	assets := flags.String("assets", "", "installer asset directory")
	port := flags.Int("port", 8080, "loopback installer port")
	configPath := flags.String("config", "", "path to server YAML configuration")
	if err := flags.Parse(args); err != nil {
		return command{}, fmt.Errorf("parse init options: %w", err)
	}
	if flags.NArg() != 0 {
		return command{}, fmt.Errorf("unexpected init arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*assets) == "" {
		return command{}, errors.New("installer assets are required")
	}
	if *port < 1 || *port > 65535 {
		return command{}, errors.New("installer port must be between 1 and 65535")
	}

	return command{
		addr:       net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)),
		assets:     filepath.Clean(*assets),
		configPath: strings.TrimSpace(*configPath),
	}, nil
}

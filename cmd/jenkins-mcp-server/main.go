package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/david/jenkins-mcp/internal/app"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "jenkins-mcp-server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			printHelp()
			return nil
		case "--version", "version":
			fmt.Println(app.Version)
			return nil
		}
	}

	cfg, err := app.LoadConfigFromProcess(os.Args[1:], os.Environ())
	if err != nil {
		return err
	}

	server, err := app.New(cfg)
	if err != nil {
		return err
	}
	if err := server.RunStdio(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func printHelp() {
	fmt.Fprintf(os.Stdout, `jenkins-mcp-server %s

Runs a Jenkins MCP server over stdio.

Configuration precedence: flags > environment > config file > defaults.

Flags:
  --config PATH     JSON config file path
  --version         Print version
  --help            Print help

Environment quick start:
  JENKINS_URL       Jenkins controller URL
  JENKINS_USER      Jenkins username
  JENKINS_TOKEN     Jenkins API token
  JENKINS_ID        Optional controller id, defaults to "default"
  JENKINS_MUTATIONS Set to "true" to enable trigger/cancel tools
`, app.Version)
}

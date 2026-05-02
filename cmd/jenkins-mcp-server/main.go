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

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			printHelp()
			return nil
		case "--version", "version":
			fmt.Println(app.Version)
			return nil
		}
	}
	if hasInitFlag(args) {
		path, err := app.InitConfigFromProcess(args, os.Environ())
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "Created starter config at %s\n", path)
		return nil
	}

	cfg, err := app.LoadConfigFromProcess(args, os.Environ())
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

func hasInitFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--init" || arg == "-init" {
			return true
		}
	}
	return false
}

func printHelp() {
	_, _ = fmt.Fprintf(os.Stdout, `jenkins-mcp-server %s

Runs a Jenkins MCP server over stdio.

Configuration precedence: flags > environment > config file > defaults.
The first existing default config file is loaded from $XDG_CONFIG_HOME/jenkins-mcp/config.json
or ~/.config/jenkins-mcp/config.json on Unix-like systems, and %%APPDATA%%\jenkins-mcp\config.json
or %%USERPROFILE%%\AppData\Roaming\jenkins-mcp\config.json on Windows.

Flags:
  --config PATH     JSON config file path
  --init            Create a starter config file and exit
  --version         Print version
  --help            Print help

Environment quick start:
  JENKINS_URL       Jenkins controller URL
  JENKINS_USER      Jenkins username
  JENKINS_TOKEN     Jenkins API token
  JENKINS_ID        Optional controller id, defaults to "default"
  JENKINS_MUTATIONS Set to "true" to enable trigger/cancel tools
  JENKINS_MCP_UPDATE_CHECK Set to "false" to disable GitHub release checks
`, app.Version)
}

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/david/jenkins-mcp/internal/app"
	"github.com/david/jenkins-mcp/internal/selfupdate"
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
	if hasSelfUpdateFlag(args) {
		cfg, selfUpdate, force, err := app.LoadSelfUpdateConfigFromProcess(args, os.Environ())
		if err != nil {
			return err
		}
		if selfUpdate {
			result, err := app.SelfUpdate(ctx, cfg, force)
			if err != nil {
				return err
			}
			printSelfUpdateResult(result)
			return nil
		}
		args = stripSelfUpdateFlags(args)
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

func hasSelfUpdateFlag(args []string) bool {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if name != "--self-update" && name != "-self-update" {
			continue
		}
		return true
	}
	return false
}

func stripSelfUpdateFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if name == "--self-update" || name == "-self-update" || name == "--force" || name == "-force" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func printSelfUpdateResult(result selfupdate.Result) {
	_, _ = fmt.Fprintf(os.Stdout, "Current version: %s\n", result.CurrentVersion)
	_, _ = fmt.Fprintf(os.Stdout, "Latest version: %s\n", result.LatestVersion)
	if result.InstalledPath != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Installed path: %s\n", result.InstalledPath)
	}
	if result.StagedPath != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Staged path: %s\n", result.StagedPath)
	}
	if result.ManifestPath != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Manifest path: %s\n", result.ManifestPath)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Restart required: %t\n", result.RestartRequired)
	if result.Message != "" {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", result.Message)
	}
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
  --self-update     Download and install or stage the latest released server binary
  --force           With --self-update, reinstall the latest release even when versions match
  --version         Print version
  --help            Print help

Environment quick start:
  JENKINS_URL       Jenkins controller URL
  JENKINS_USER      Jenkins username
  JENKINS_TOKEN     Jenkins API token
  JENKINS_ID        Optional controller id, defaults to "default"
  JENKINS_MUTATIONS Set to "true" to enable trigger/cancel tools
  JENKINS_MCP_LOG_LEVEL Set to "debug" to log Jenkins request URLs
  JENKINS_MCP_LOG_FILE Write server logs to a file instead of stderr
  JENKINS_MCP_LOG_TOOL_CALLS Set to "true" to log MCP tool call start/finish events
  JENKINS_MCP_LOG_TOOL_PAYLOADS Set to "true" to also log full tool arguments and responses
  JENKINS_MCP_UPDATE_CHECK Set to "false" to disable GitHub release checks
  JENKINS_MCP_SELF_UPDATE Set to "true" to enable the MCP self-update tool
`, app.Version)
}

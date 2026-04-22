package main

import (
	"fmt"
	"io"
	"os"

	"github.com/caorenmei/autoproxy3/src/internal/buildinfo"
)

type appArgs struct {
	mode string
}

var (
	serveCommand             = runServe
	reloadWebRulesCommand    = runReloadWebRules
	reloadCustomRulesCommand = runReloadCustomRules
	reloadRulesCommand       = runReloadRules
)

func parseArgs(args []string) (appArgs, error) {
	if len(args) <= 1 {
		return appArgs{mode: "serve"}, nil
	}

	switch args[1] {
	case "serve", "version", "help", "reload_web_rules", "reload_custom_rules", "reload_rules":
		return appArgs{mode: args[1]}, nil
	default:
		return appArgs{}, fmt.Errorf("unknown command: %s", args[1])
	}
}

func main() {
	if code := run(os.Args, os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printHelp(stderr)
		return 1
	}

	switch parsed.mode {
	case "serve":
		if err := serveCommand(parsed); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "version":
		fmt.Fprintln(stdout, buildinfo.Version)
	case "help":
		printHelp(stdout)
	case "reload_web_rules":
		if err := reloadWebRulesCommand(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "reload_custom_rules":
		if err := reloadCustomRulesCommand(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "reload_rules":
		if err := reloadRulesCommand(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	return 0
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]")
}

func runServe(args appArgs) error {
	_ = args
	return nil
}

func runReloadWebRules() error {
	return nil
}

func runReloadCustomRules() error {
	return nil
}

func runReloadRules() error {
	return nil
}

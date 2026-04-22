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

func normalizeArgs(args []string) appArgs {
	if len(args) > 1 {
		switch args[1] {
		case "serve", "version", "help", "reload_web_rules", "reload_custom_rules", "reload_rules":
			return appArgs{mode: args[1]}
		}
	}

	return appArgs{mode: "serve"}
}

func main() {
	args := normalizeArgs(os.Args)

	switch args.mode {
	case "serve":
		if err := runServe(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(buildinfo.Version)
	case "help":
		printHelp(os.Stdout)
	case "reload_web_rules":
		if err := runReloadWebRules(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "reload_custom_rules":
		if err := runReloadCustomRules(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "reload_rules":
		if err := runReloadRules(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		if err := runServe(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
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

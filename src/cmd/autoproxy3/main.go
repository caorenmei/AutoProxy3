package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/caorenmei/autoproxy3/src/internal/buildinfo"
	"github.com/caorenmei/autoproxy3/src/internal/config"
)

type appArgs struct {
	mode       string
	configPath string
}

const defaultConfigPath = "config.json"

type serveHandler func(appArgs, config.Config) error

type configLoader func(string) (config.Config, error)

type reloadRulesHandler func() error

type commandHandlers struct {
	serve             serveHandler
	reloadWebRules    reloadRulesHandler
	reloadCustomRules reloadRulesHandler
}

type app struct {
	handlers   commandHandlers
	loadConfig configLoader
}

func newApp(handlers commandHandlers) app {
	return newAppWithConfigLoader(handlers, loadConfigFromPath)
}

func newAppWithConfigLoader(handlers commandHandlers, loader configLoader) app {
	if handlers.serve == nil {
		handlers.serve = func(appArgs, config.Config) error {
			return nil
		}
	}
	if handlers.reloadWebRules == nil {
		handlers.reloadWebRules = func() error {
			return nil
		}
	}
	if handlers.reloadCustomRules == nil {
		handlers.reloadCustomRules = func() error {
			return nil
		}
	}
	if loader == nil {
		loader = loadConfigFromPath
	}
	return app{handlers: handlers, loadConfig: loader}
}

func parseArgs(args []string) (appArgs, error) {
	parsed := appArgs{mode: "serve", configPath: defaultConfigPath}
	commandSet := false
	if len(args) <= 1 {
		return parsed, nil
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			i++
			if i >= len(args) {
				return appArgs{}, errors.New("missing value for --config")
			}
			parsed.configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			parsed.configPath = strings.TrimPrefix(arg, "--config=")
		case isCommand(arg):
			if commandSet {
				return appArgs{}, fmt.Errorf("unexpected argument: %s", arg)
			}
			parsed.mode = arg
			commandSet = true
		default:
			return appArgs{}, fmt.Errorf("unknown command: %s", arg)
		}
	}

	return parsed, nil
}

func isCommand(arg string) bool {
	switch arg {
	case "serve", "version", "help", "reload_web_rules", "reload_custom_rules", "reload_rules":
		return true
	default:
		return false
	}
}

func main() {
	runMain(os.Args, os.Stdout, os.Stderr, os.Exit)
}

func runMain(args []string, stdout, stderr io.Writer, exit func(int)) {
	if code := run(args, stdout, stderr); code != 0 {
		exit(code)
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	return newApp(commandHandlers{}).run(args, stdout, stderr)
}

func (a app) run(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printHelp(stderr)
		return 1
	}

	switch parsed.mode {
	case "serve":
		cfg, err := a.loadConfig(parsed.configPath)
		if err != nil {
			fmt.Fprintln(stderr, fmt.Errorf("load config: %w", err))
			return 1
		}
		if err := runServe(parsed, cfg, a.handlers.serve); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "version":
		fmt.Fprintln(stdout, buildinfo.Version)
	case "help":
		printHelp(stdout)
	case "reload_web_rules":
		if err := runReloadWebRules(a.handlers.reloadWebRules); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "reload_custom_rules":
		if err := runReloadCustomRules(a.handlers.reloadCustomRules); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "reload_rules":
		if err := runReloadRules(a.handlers.reloadWebRules, a.handlers.reloadCustomRules); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	return 0
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: autoproxy3 [--config <path>] [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]")
	fmt.Fprintln(w, "Default config path: "+defaultConfigPath)
}

func loadConfigFromPath(path string) (config.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.Load(strings.NewReader("{}"), path)
		}
		return config.Config{}, fmt.Errorf("open config file: %w", err)
	}
	defer file.Close()

	return config.Load(file, path)
}

func runServe(args appArgs, cfg config.Config, handler serveHandler) error {
	if handler == nil {
		return errors.New("serve handler is not configured")
	}
	return handler(args, cfg)
}

func runReloadWebRules(handler reloadRulesHandler) error {
	if handler == nil {
		return errors.New("reload web rules handler is not configured")
	}
	return handler()
}

func runReloadCustomRules(handler reloadRulesHandler) error {
	if handler == nil {
		return errors.New("reload custom rules handler is not configured")
	}
	return handler()
}

func runReloadRules(reloadWebRules reloadRulesHandler, reloadCustomRules reloadRulesHandler) error {
	if err := runReloadWebRules(reloadWebRules); err != nil {
		return fmt.Errorf("reload web rules: %w", err)
	}
	if err := runReloadCustomRules(reloadCustomRules); err != nil {
		return fmt.Errorf("reload custom rules: %w", err)
	}
	return nil
}

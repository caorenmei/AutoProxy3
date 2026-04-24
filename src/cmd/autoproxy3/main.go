package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/caorenmei/autoproxy3/src/internal/buildinfo"
	"github.com/caorenmei/autoproxy3/src/internal/cli"
	"github.com/caorenmei/autoproxy3/src/internal/config"
	"github.com/caorenmei/autoproxy3/src/internal/runtime"
)

type appArgs struct {
	mode       string
	configPath string
}

const defaultConfigPath = "config.json"

type serveHandler func(appArgs, config.Config) error

type configLoader func(string) (config.Config, error)

type reloadRulesHandler func(config.Config) error

type commandHandlers struct {
	serve             serveHandler
	reloadWebRules    reloadRulesHandler
	reloadCustomRules reloadRulesHandler
	reloadRules       reloadRulesHandler
}

type app struct {
	handlers   commandHandlers
	loadConfig configLoader
}

var newRuntime = runtime.New
var newServeContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func newApp(handlers commandHandlers) app {
	return newAppWithConfigLoader(handlers, loadConfigFromPath)
}

func newAppWithConfigLoader(handlers commandHandlers, loader configLoader) app {
	if handlers.serve == nil {
		handlers.serve = defaultServe
	}
	if handlers.reloadWebRules == nil {
		handlers.reloadWebRules = defaultReloadWebRules
	}
	if handlers.reloadCustomRules == nil {
		handlers.reloadCustomRules = defaultReloadCustomRules
	}
	if handlers.reloadRules == nil {
		handlers.reloadRules = defaultReloadRules
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
		cfg, err := a.loadConfig(parsed.configPath)
		if err != nil {
			fmt.Fprintln(stderr, fmt.Errorf("load config: %w", err))
			return 1
		}
		if err := runReloadWebRules(cfg, a.handlers.reloadWebRules); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "reload_custom_rules":
		cfg, err := a.loadConfig(parsed.configPath)
		if err != nil {
			fmt.Fprintln(stderr, fmt.Errorf("load config: %w", err))
			return 1
		}
		if err := runReloadCustomRules(cfg, a.handlers.reloadCustomRules); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	case "reload_rules":
		cfg, err := a.loadConfig(parsed.configPath)
		if err != nil {
			fmt.Fprintln(stderr, fmt.Errorf("load config: %w", err))
			return 1
		}
		if err := runReloadRulesCommand(cfg, a.handlers.reloadRules); err != nil {
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

func runReloadWebRules(cfg config.Config, handler reloadRulesHandler) error {
	if handler == nil {
		return errors.New("reload web rules handler is not configured")
	}
	return handler(cfg)
}

func runReloadCustomRules(cfg config.Config, handler reloadRulesHandler) error {
	if handler == nil {
		return errors.New("reload custom rules handler is not configured")
	}
	return handler(cfg)
}

func runReloadRulesCommand(cfg config.Config, handler reloadRulesHandler) error {
	if handler == nil {
		return errors.New("reload rules handler is not configured")
	}
	return handler(cfg)
}

func defaultServe(_ appArgs, cfg config.Config) error {
	runner, err := newRuntime(cfg)
	if err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	ctx, cancel := newServeContext()
	defer cancel()

	return runner.Run(ctx)
}

func defaultReloadWebRules(cfg config.Config) error {
	return newReloadClient(cfg).ReloadWebRules(context.Background())
}

func defaultReloadCustomRules(cfg config.Config) error {
	return newReloadClient(cfg).ReloadCustomRules(context.Background())
}

func defaultReloadRules(cfg config.Config) error {
	return newReloadClient(cfg).ReloadRules(context.Background())
}

func newReloadClient(cfg config.Config) *cli.Client {
	return cli.NewClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.Management.ListenPort), http.DefaultClient)
}

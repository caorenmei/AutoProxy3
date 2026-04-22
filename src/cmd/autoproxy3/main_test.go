package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/caorenmei/autoproxy3/src/internal/buildinfo"
	"github.com/caorenmei/autoproxy3/src/internal/config"
)

type testCounters struct {
	serve        int
	loadConfig   int
	reloadWeb    int
	reloadCustom int
}

const expectedHelpText = "Usage: autoproxy3 [--config <path>] [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\nDefault config path: config.json\n"

func TestRunUsesDefaultCommandHandlers(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:     "default serve",
			args:     []string{"autoproxy3"},
			wantCode: 0,
		},
		{
			name:     "explicit serve",
			args:     []string{"autoproxy3", "serve"},
			wantCode: 0,
		},
		{
			name:       "help",
			args:       []string{"autoproxy3", "help"},
			wantCode:   0,
			wantStdout: expectedHelpText,
		},
		{
			name:       "version",
			args:       []string{"autoproxy3", "version"},
			wantCode:   0,
			wantStdout: buildinfo.Version + "\n",
		},
		{
			name:     "reload web rules",
			args:     []string{"autoproxy3", "reload_web_rules"},
			wantCode: 0,
		},
		{
			name:     "reload custom rules",
			args:     []string{"autoproxy3", "reload_custom_rules"},
			wantCode: 0,
		},
		{
			name:     "reload all rules",
			args:     []string{"autoproxy3", "reload_rules"},
			wantCode: 0,
		},
		{
			name:       "unknown command",
			args:       []string{"autoproxy3", "bogus"},
			wantCode:   1,
			wantStderr: "unknown command: bogus\n" + expectedHelpText,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			gotCode := run(tc.args, &stdout, &stderr)

			if gotCode != tc.wantCode {
				t.Fatalf("expected exit code %d, got %d", tc.wantCode, gotCode)
			}
			if gotStdout := stdout.String(); gotStdout != tc.wantStdout {
				t.Fatalf("unexpected stdout: %q", gotStdout)
			}
			if gotStderr := stderr.String(); gotStderr != tc.wantStderr {
				t.Fatalf("unexpected stderr: %q", gotStderr)
			}
		})
	}
}

func TestAppRunDispatchesCommands(t *testing.T) {
	serveErr := errors.New("serve failed")
	configErr := errors.New("load config failed")
	reloadWebErr := errors.New("reload web failed")
	reloadCustomErr := errors.New("reload custom failed")
	wantConfig := config.Config{ListenAddr: "127.0.0.1:8080"}

	tests := []struct {
		name       string
		args       []string
		handlers   commandHandlers
		loadConfig configLoader
		wantCode   int
		wantStdout string
		wantStderr string
		wantCounts testCounters
	}{
		{
			name: "serve command",
			args: []string{"autoproxy3", "--config", "custom.json", "serve"},
			handlers: commandHandlers{
				serve: func(args appArgs, cfg config.Config) error {
					if args.mode != "serve" {
						t.Fatalf("unexpected serve mode %q", args.mode)
					}
					if args.configPath != "custom.json" {
						t.Fatalf("unexpected config path %q", args.configPath)
					}
					if cfg != wantConfig {
						t.Fatalf("unexpected config: %+v", cfg)
					}
					return nil
				},
			},
			loadConfig: func(path string) (config.Config, error) {
				if path != "custom.json" {
					t.Fatalf("unexpected config load path %q", path)
				}
				return wantConfig, nil
			},
			wantCode:   0,
			wantCounts: testCounters{serve: 1, loadConfig: 1},
		},
		{
			name: "serve command error",
			args: []string{"autoproxy3", "serve"},
			handlers: commandHandlers{
				serve: func(appArgs, config.Config) error { return serveErr },
			},
			loadConfig: func(string) (config.Config, error) { return config.Config{}, nil },
			wantCode:   1,
			wantStderr: "serve failed\n",
			wantCounts: testCounters{serve: 1, loadConfig: 1},
		},
		{
			name: "serve config load error",
			args: []string{"autoproxy3", "serve"},
			handlers: commandHandlers{
				serve: func(appArgs, config.Config) error {
					t.Fatal("serve handler should not run on load failure")
					return nil
				},
			},
			loadConfig: func(string) (config.Config, error) { return config.Config{}, configErr },
			wantCode:   1,
			wantStderr: "load config: load config failed\n",
			wantCounts: testCounters{loadConfig: 1},
		},
		{
			name: "reload web rules",
			args: []string{"autoproxy3", "reload_web_rules"},
			handlers: commandHandlers{
				reloadWebRules: func() error { return nil },
			},
			wantCode:   0,
			wantCounts: testCounters{reloadWeb: 1},
		},
		{
			name: "reload web rules error",
			args: []string{"autoproxy3", "reload_web_rules"},
			handlers: commandHandlers{
				reloadWebRules: func() error { return reloadWebErr },
			},
			wantCode:   1,
			wantStderr: "reload web failed\n",
			wantCounts: testCounters{reloadWeb: 1},
		},
		{
			name: "reload custom rules",
			args: []string{"autoproxy3", "reload_custom_rules"},
			handlers: commandHandlers{
				reloadCustomRules: func() error { return nil },
			},
			wantCode:   0,
			wantCounts: testCounters{reloadCustom: 1},
		},
		{
			name: "reload custom rules error",
			args: []string{"autoproxy3", "reload_custom_rules"},
			handlers: commandHandlers{
				reloadCustomRules: func() error { return reloadCustomErr },
			},
			wantCode:   1,
			wantStderr: "reload custom failed\n",
			wantCounts: testCounters{reloadCustom: 1},
		},
		{
			name: "reload all rules",
			args: []string{"autoproxy3", "reload_rules"},
			handlers: commandHandlers{
				reloadWebRules:    func() error { return nil },
				reloadCustomRules: func() error { return nil },
			},
			wantCode:   0,
			wantCounts: testCounters{reloadWeb: 1, reloadCustom: 1},
		},
		{
			name: "reload all rules web error",
			args: []string{"autoproxy3", "reload_rules"},
			handlers: commandHandlers{
				reloadWebRules:    func() error { return reloadWebErr },
				reloadCustomRules: func() error { return nil },
			},
			wantCode:   1,
			wantStderr: "reload web rules: reload web failed\n",
			wantCounts: testCounters{reloadWeb: 1},
		},
		{
			name: "reload all rules custom error",
			args: []string{"autoproxy3", "reload_rules"},
			handlers: commandHandlers{
				reloadWebRules:    func() error { return nil },
				reloadCustomRules: func() error { return reloadCustomErr },
			},
			wantCode:   1,
			wantStderr: "reload custom rules: reload custom failed\n",
			wantCounts: testCounters{reloadWeb: 1, reloadCustom: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts := testCounters{}
			handlers := tc.handlers
			if handlers.serve != nil {
				original := handlers.serve
				handlers.serve = func(args appArgs, cfg config.Config) error {
					counts.serve++
					return original(args, cfg)
				}
			}
			if handlers.reloadWebRules != nil {
				original := handlers.reloadWebRules
				handlers.reloadWebRules = func() error {
					counts.reloadWeb++
					return original()
				}
			}
			if handlers.reloadCustomRules != nil {
				original := handlers.reloadCustomRules
				handlers.reloadCustomRules = func() error {
					counts.reloadCustom++
					return original()
				}
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			loadConfig := tc.loadConfig
			if loadConfig != nil {
				original := loadConfig
				loadConfig = func(path string) (config.Config, error) {
					counts.loadConfig++
					return original(path)
				}
			}
			gotCode := newAppWithConfigLoader(handlers, loadConfig).run(tc.args, &stdout, &stderr)

			if gotCode != tc.wantCode {
				t.Fatalf("expected exit code %d, got %d", tc.wantCode, gotCode)
			}
			if gotStdout := stdout.String(); gotStdout != tc.wantStdout {
				t.Fatalf("unexpected stdout: %q", gotStdout)
			}
			if gotStderr := stderr.String(); gotStderr != tc.wantStderr {
				t.Fatalf("unexpected stderr: %q", gotStderr)
			}
			if counts != tc.wantCounts {
				t.Fatalf("unexpected counters: got %+v want %+v", counts, tc.wantCounts)
			}
		})
	}
}

func TestRunMain(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantExit   bool
		wantStdout string
		wantStderr string
	}{
		{
			name:       "success without exit",
			args:       []string{"autoproxy3", "help"},
			wantStdout: expectedHelpText,
		},
		{
			name:       "error triggers exit",
			args:       []string{"autoproxy3", "bogus"},
			wantCode:   1,
			wantExit:   true,
			wantStderr: "unknown command: bogus\n" + expectedHelpText,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exited := false
			exitCode := 0

			runMain(tc.args, &stdout, &stderr, func(code int) {
				exited = true
				exitCode = code
			})

			if exited != tc.wantExit {
				t.Fatalf("unexpected exit state: got %v want %v", exited, tc.wantExit)
			}
			if exitCode != tc.wantCode {
				t.Fatalf("unexpected exit code: got %d want %d", exitCode, tc.wantCode)
			}
			if gotStdout := stdout.String(); gotStdout != tc.wantStdout {
				t.Fatalf("unexpected stdout: %q", gotStdout)
			}
			if gotStderr := stderr.String(); gotStderr != tc.wantStderr {
				t.Fatalf("unexpected stderr: %q", gotStderr)
			}
		})
	}
}

func TestRunServe(t *testing.T) {
	serveErr := errors.New("serve failed")
	args := appArgs{mode: "serve"}
	cfg := config.Config{ListenAddr: "127.0.0.1:8080"}

	tests := []struct {
		name    string
		handler func(appArgs, config.Config) error
		wantErr string
	}{
		{
			name: "success",
			handler: func(got appArgs, gotCfg config.Config) error {
				if got != args {
					t.Fatalf("unexpected args: %+v", got)
				}
				if gotCfg != cfg {
					t.Fatalf("unexpected config: %+v", gotCfg)
				}
				return nil
			},
		},
		{
			name:    "missing handler",
			wantErr: "serve handler is not configured",
		},
		{
			name:    "handler error",
			handler: func(appArgs, config.Config) error { return serveErr },
			wantErr: "serve failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runServe(args, cfg, tc.handler)
			assertErrorString(t, err, tc.wantErr)
		})
	}
}

func TestRunReloadWebRules(t *testing.T) {
	reloadErr := errors.New("reload web failed")

	tests := []struct {
		name    string
		handler func() error
		wantErr string
	}{
		{
			name:    "success",
			handler: func() error { return nil },
		},
		{
			name:    "missing handler",
			wantErr: "reload web rules handler is not configured",
		},
		{
			name:    "handler error",
			handler: func() error { return reloadErr },
			wantErr: "reload web failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runReloadWebRules(tc.handler)
			assertErrorString(t, err, tc.wantErr)
		})
	}
}

func TestRunReloadCustomRules(t *testing.T) {
	reloadErr := errors.New("reload custom failed")

	tests := []struct {
		name    string
		handler func() error
		wantErr string
	}{
		{
			name:    "success",
			handler: func() error { return nil },
		},
		{
			name:    "missing handler",
			wantErr: "reload custom rules handler is not configured",
		},
		{
			name:    "handler error",
			handler: func() error { return reloadErr },
			wantErr: "reload custom failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runReloadCustomRules(tc.handler)
			assertErrorString(t, err, tc.wantErr)
		})
	}
}

func TestRunReloadRules(t *testing.T) {
	reloadWebErr := errors.New("reload web failed")
	reloadCustomErr := errors.New("reload custom failed")

	tests := []struct {
		name          string
		webHandler    func() error
		customHandler func() error
		wantErr       string
		wantCounts    testCounters
	}{
		{
			name:          "success",
			webHandler:    func() error { return nil },
			customHandler: func() error { return nil },
			wantCounts:    testCounters{reloadWeb: 1, reloadCustom: 1},
		},
		{
			name:       "missing web handler",
			wantErr:    "reload web rules: reload web rules handler is not configured",
			wantCounts: testCounters{},
		},
		{
			name:       "missing custom handler",
			webHandler: func() error { return nil },
			wantErr:    "reload custom rules: reload custom rules handler is not configured",
			wantCounts: testCounters{reloadWeb: 1},
		},
		{
			name:          "web handler error",
			webHandler:    func() error { return reloadWebErr },
			customHandler: func() error { return nil },
			wantErr:       "reload web rules: reload web failed",
			wantCounts:    testCounters{reloadWeb: 1},
		},
		{
			name:          "custom handler error",
			webHandler:    func() error { return nil },
			customHandler: func() error { return reloadCustomErr },
			wantErr:       "reload custom rules: reload custom failed",
			wantCounts:    testCounters{reloadWeb: 1, reloadCustom: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts := testCounters{}
			webHandler := tc.webHandler
			customHandler := tc.customHandler
			if webHandler != nil {
				original := webHandler
				webHandler = func() error {
					counts.reloadWeb++
					return original()
				}
			}
			if customHandler != nil {
				original := customHandler
				customHandler = func() error {
					counts.reloadCustom++
					return original()
				}
			}

			err := runReloadRules(webHandler, customHandler)
			assertErrorString(t, err, tc.wantErr)
			if counts != tc.wantCounts {
				t.Fatalf("unexpected counters: got %+v want %+v", counts, tc.wantCounts)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    appArgs
		wantErr string
	}{
		{
			name: "default config",
			args: []string{"autoproxy3"},
			want: appArgs{mode: "serve", configPath: "config.json"},
		},
		{
			name: "config path flag",
			args: []string{"autoproxy3", "--config", "custom.json"},
			want: appArgs{mode: "serve", configPath: "custom.json"},
		},
		{
			name: "config equals flag",
			args: []string{"autoproxy3", "--config=custom.json"},
			want: appArgs{mode: "serve", configPath: "custom.json"},
		},
		{
			name:    "unknown command",
			args:    []string{"autoproxy3", "bogus"},
			wantErr: "unknown command: bogus",
		},
		{
			name:    "config flag missing value",
			args:    []string{"autoproxy3", "--config"},
			wantErr: "missing value for --config",
		},
		{
			name:    "duplicate command",
			args:    []string{"autoproxy3", "serve", "serve"},
			wantErr: "unexpected argument: serve",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgs(tc.args)
			assertErrorString(t, err, tc.wantErr)
			if err != nil {
				return
			}
			if got != tc.want {
				t.Fatalf("unexpected args: got %+v want %+v", got, tc.want)
			}
		})
	}
}

// 此测试会修改进程级状态（os.Args、os.Stdout），因此不能并行执行。
func TestMainEntryPoint(t *testing.T) {
	originalArgs := os.Args
	originalStdout := os.Stdout
	defer func() {
		os.Args = originalArgs
		os.Stdout = originalStdout
	}()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	defer reader.Close()

	os.Args = []string{"autoproxy3", "help"}
	os.Stdout = writer

	main()

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close stdout writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	if string(output) != expectedHelpText {
		t.Fatalf("unexpected stdout: %q", string(output))
	}
}

func assertErrorString(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error %q, got nil", want)
	}
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}

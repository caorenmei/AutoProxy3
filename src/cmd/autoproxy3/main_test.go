package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/caorenmei/autoproxy3/src/internal/buildinfo"
)

type testCounters struct {
	serve        int
	reloadWeb    int
	reloadCustom int
}

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
			wantStdout: "Usage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n",
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
			wantStderr: "unknown command: bogus\nUsage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n",
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
	reloadWebErr := errors.New("reload web failed")
	reloadCustomErr := errors.New("reload custom failed")

	tests := []struct {
		name       string
		args       []string
		handlers   commandHandlers
		wantCode   int
		wantStdout string
		wantStderr string
		wantCounts testCounters
	}{
		{
			name: "serve command",
			args: []string{"autoproxy3", "serve"},
			handlers: commandHandlers{
				serve: func(args appArgs) error {
					if args.mode != "serve" {
						t.Fatalf("unexpected serve mode %q", args.mode)
					}
					return nil
				},
			},
			wantCode:   0,
			wantCounts: testCounters{serve: 1},
		},
		{
			name: "serve command error",
			args: []string{"autoproxy3", "serve"},
			handlers: commandHandlers{
				serve: func(appArgs) error { return serveErr },
			},
			wantCode:   1,
			wantStderr: "serve failed\n",
			wantCounts: testCounters{serve: 1},
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
				handlers.serve = func(args appArgs) error {
					counts.serve++
					return original(args)
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
			gotCode := newApp(handlers).run(tc.args, &stdout, &stderr)

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
			wantStdout: "Usage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n",
		},
		{
			name:       "error triggers exit",
			args:       []string{"autoproxy3", "bogus"},
			wantCode:   1,
			wantExit:   true,
			wantStderr: "unknown command: bogus\nUsage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n",
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

	tests := []struct {
		name    string
		handler func(appArgs) error
		wantErr string
	}{
		{
			name: "success",
			handler: func(got appArgs) error {
				if got != args {
					t.Fatalf("unexpected args: %+v", got)
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
			handler: func(appArgs) error { return serveErr },
			wantErr: "serve failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runServe(args, tc.handler)
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

func TestMain(t *testing.T) {
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
	if string(output) != "Usage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n" {
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

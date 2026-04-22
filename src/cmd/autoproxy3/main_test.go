package main

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRunDispatchesCommands(t *testing.T) {
	type counters struct {
		serve  int
		web    int
		custom int
		rules  int
	}

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
		wantCounts counters
	}{
		{
			name:       "no-arg default behavior",
			args:       []string{"autoproxy3"},
			wantCode:   0,
			wantCounts: counters{serve: 1},
		},
		{
			name:       "serve command",
			args:       []string{"autoproxy3", "serve"},
			wantCode:   0,
			wantCounts: counters{serve: 1},
		},
		{
			name:       "help command",
			args:       []string{"autoproxy3", "help"},
			wantCode:   0,
			wantStdout: "Usage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n",
		},
		{
			name:       "version command",
			args:       []string{"autoproxy3", "version"},
			wantCode:   0,
			wantStdout: "0.1.0\n",
		},
		{
			name:       "reload web rules",
			args:       []string{"autoproxy3", "reload_web_rules"},
			wantCode:   0,
			wantCounts: counters{web: 1},
		},
		{
			name:       "reload custom rules",
			args:       []string{"autoproxy3", "reload_custom_rules"},
			wantCode:   0,
			wantCounts: counters{custom: 1},
		},
		{
			name:       "reload all rules",
			args:       []string{"autoproxy3", "reload_rules"},
			wantCode:   0,
			wantCounts: counters{rules: 1},
		},
		{
			name:       "unknown command handling",
			args:       []string{"autoproxy3", "bogus"},
			wantCode:   1,
			wantStderr: "unknown command: bogus\nUsage: autoproxy3 [serve|version|help|reload_web_rules|reload_custom_rules|reload_rules]\n",
		},
	}

	originalServe := serveCommand
	originalWeb := reloadWebRulesCommand
	originalCustom := reloadCustomRulesCommand
	originalRules := reloadRulesCommand
	t.Cleanup(func() {
		serveCommand = originalServe
		reloadWebRulesCommand = originalWeb
		reloadCustomRulesCommand = originalCustom
		reloadRulesCommand = originalRules
	})

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			counts := counters{}
			serveCommand = func(args appArgs) error {
				counts.serve++
				if args.mode != "serve" {
					return fmt.Errorf("unexpected serve mode %q", args.mode)
				}
				return nil
			}
			reloadWebRulesCommand = func() error {
				counts.web++
				return nil
			}
			reloadCustomRulesCommand = func() error {
				counts.custom++
				return nil
			}
			reloadRulesCommand = func() error {
				counts.rules++
				return nil
			}

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
			if counts != tc.wantCounts {
				t.Fatalf("unexpected counters: got %+v want %+v", counts, tc.wantCounts)
			}
		})
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigAppliesDefaults(t *testing.T) {
	cfg, err := Load(strings.NewReader(`{"listen_addr":"localhost:1080"}`), "/workspace/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "listen addr", got: cfg.ListenAddr, want: "localhost:1080"},
		{name: "web rules enabled", got: cfg.WebRules.Enabled, want: true},
		{name: "web rules download on start", got: cfg.WebRules.DownloadOnStart, want: true},
		{name: "custom rules enabled", got: cfg.CustomRules.Enabled, want: true},
		{name: "auto detect enabled", got: cfg.AutoDetect.Enabled, want: false},
		{name: "auto detect max attempts", got: cfg.AutoDetect.MaxAttempts, want: 3},
		{name: "management enabled", got: cfg.Management.Enabled, want: true},
		{name: "management listen port", got: cfg.Management.ListenPort, want: 9091},
		{name: "logging level", got: cfg.Logging.Level, want: "info"},
		{name: "logging format", got: cfg.Logging.Format, want: "text"},
		{name: "logging max size", got: cfg.Logging.MaxSize, want: 10},
		{name: "logging max backups", got: cfg.Logging.MaxBackups, want: 5},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Fatalf("unexpected %s: got %v want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestLoadConfigDefaultsListenAddrWhenOmitted(t *testing.T) {
	cfg, err := Load(strings.NewReader(`{}`), "/workspace/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != "localhost:1080" {
		t.Fatalf("unexpected listen addr: got %q want %q", cfg.ListenAddr, "localhost:1080")
	}
}

func TestLoadConfigResolvesRelativePaths(t *testing.T) {
	cfg, err := Load(strings.NewReader(`{
		"web_rules":{"cache_path":"web_rules.txt"},
		"custom_rules":{"path":"custom.txt"},
		"auto_detect":{"rules_path":"auto_rules.txt"},
		"logging":{"file_path":"logs/proxy.log"}
	}`), "/opt/autoproxy/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "web rules cache", got: cfg.WebRules.CachePath, want: "/opt/autoproxy/web_rules.txt"},
		{name: "custom rules path", got: cfg.CustomRules.Path, want: "/opt/autoproxy/custom.txt"},
		{name: "auto detect path", got: cfg.AutoDetect.RulesPath, want: "/opt/autoproxy/auto_rules.txt"},
		{name: "logging file path", got: cfg.Logging.FilePath, want: "/opt/autoproxy/logs/proxy.log"},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Fatalf("unexpected %s: got %q want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestLoadConfigKeepsAbsolutePaths(t *testing.T) {
	cfg, err := Load(strings.NewReader(`{
		"web_rules":{"cache_path":"/var/lib/autoproxy/web_rules.txt"},
		"custom_rules":{"path":"/etc/autoproxy/custom.txt"},
		"auto_detect":{"rules_path":"/srv/autoproxy/auto_rules.txt"},
		"logging":{"file_path":"/var/log/autoproxy/proxy.log"}
	}`), "/opt/autoproxy/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "web rules cache", got: cfg.WebRules.CachePath, want: "/var/lib/autoproxy/web_rules.txt"},
		{name: "custom rules path", got: cfg.CustomRules.Path, want: "/etc/autoproxy/custom.txt"},
		{name: "auto detect path", got: cfg.AutoDetect.RulesPath, want: "/srv/autoproxy/auto_rules.txt"},
		{name: "logging file path", got: cfg.Logging.FilePath, want: "/var/log/autoproxy/proxy.log"},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Fatalf("unexpected %s: got %q want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestLoadConfigResolvesRelativePathsFromRelativeSourcePath(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	tests := []struct {
		name       string
		sourcePath string
		wantPath   string
	}{
		{
			name:       "config in current directory",
			sourcePath: "config.json",
			wantPath:   filepath.Join(workingDir, "custom.txt"),
		},
		{
			name:       "config in nested directory",
			sourcePath: filepath.Join("configs", "dev.json"),
			wantPath:   filepath.Join(workingDir, "configs", "custom.txt"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(strings.NewReader(`{
				"custom_rules":{"path":"custom.txt"}
			}`), tc.sourcePath)
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			if cfg.CustomRules.Path != tc.wantPath {
				t.Fatalf("unexpected custom rules path: got %q want %q", cfg.CustomRules.Path, tc.wantPath)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidJSON(t *testing.T) {
	_, err := Load(strings.NewReader(`{"listen_addr":`), "/workspace/config.json")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode config:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigAllowsEmptyOptionalFields(t *testing.T) {
	cfg, err := Load(strings.NewReader(`{
		"upstream_proxy":"",
		"logging":{"file_path":""}
	}`), "/workspace/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.UpstreamProxy != "" {
		t.Fatalf("expected empty upstream proxy, got %q", cfg.UpstreamProxy)
	}
	if cfg.Logging.FilePath != "" {
		t.Fatalf("expected empty logging file path, got %q", cfg.Logging.FilePath)
	}
}

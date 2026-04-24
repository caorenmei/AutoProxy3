package rulesources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoDetectStoreAppendHostDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto.txt")
	store := AutoDetectStore{Path: path}
	if err := store.AppendHost(" Example.COM. "); err != nil {
		t.Fatalf("first AppendHost returned error: %v", err)
	}
	if err := store.AppendHost("example.com"); err != nil {
		t.Fatalf("second AppendHost returned error: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(body) != "example.com\n" {
		t.Fatalf("expected normalized single host entry, got %q", string(body))
	}
}

func TestAutoDetectStoreAppendHostRoundTripsHostPortThroughFileSource(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.txt")
	autoPath := filepath.Join(dir, "auto.txt")
	if err := os.WriteFile(customPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	store := AutoDetectStore{Path: autoPath}
	if err := store.AppendHost(" Example.com:443 "); err != nil {
		t.Fatalf("AppendHost returned error: %v", err)
	}

	source := FileSource{}
	_, autoSet, err := source.LoadCustomAndAutoDetect(customPath, autoPath)
	if err != nil {
		t.Fatalf("LoadCustomAndAutoDetect returned error: %v", err)
	}
	if !autoSet.Match("example.com") {
		t.Fatal("expected loaded auto-detect rules to match normalized host without port")
	}
}

func TestAutoDetectStoreAppendHostIgnoresEmptyHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto.txt")
	store := AutoDetectStore{Path: path}
	if err := store.AppendHost("  \n\t "); err != nil {
		t.Fatalf("AppendHost returned error: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadFile returned unexpected error: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("expected no content for empty host, got %q", string(body))
	}
}

func TestAutoDetectStoreAppendHostReturnsOpenError(t *testing.T) {
	store := AutoDetectStore{Path: t.TempDir()}
	err := store.AppendHost("example.com")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "open auto-detect rules file") {
		t.Fatalf("expected open file error, got %v", err)
	}
}

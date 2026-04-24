package rulesources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSourceLoadCustomAndAutoDetect(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.txt")
	autoPath := filepath.Join(dir, "auto.txt")
	if err := os.WriteFile(customPath, []byte("*.google.com\n"), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}
	if err := os.WriteFile(autoPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write auto-detect: %v", err)
	}

	source := FileSource{}
	customSet, autoSet, err := source.LoadCustomAndAutoDetect(customPath, autoPath)
	if err != nil {
		t.Fatalf("LoadCustomAndAutoDetect returned error: %v", err)
	}
	if !customSet.Match("mail.google.com") {
		t.Fatal("expected custom rules to match wildcard host")
	}
	if !autoSet.Match("example.com") {
		t.Fatal("expected auto-detect rules to match exact host")
	}
}

func TestFileSourceLoadCustomAndAutoDetectReturnsCustomOpenError(t *testing.T) {
	source := FileSource{}
	_, _, err := source.LoadCustomAndAutoDetect(filepath.Join(t.TempDir(), "missing-custom.txt"), filepath.Join(t.TempDir(), "missing-auto.txt"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "open custom rules") {
		t.Fatalf("expected custom open error, got %v", err)
	}
}

func TestFileSourceLoadCustomAndAutoDetectTreatsMissingAutoDetectFileAsEmptySet(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.txt")
	if err := os.WriteFile(customPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	source := FileSource{}
	customSet, autoSet, err := source.LoadCustomAndAutoDetect(customPath, filepath.Join(dir, "missing-auto.txt"))
	if err != nil {
		t.Fatalf("LoadCustomAndAutoDetect returned error: %v", err)
	}
	if !customSet.Match("example.com") {
		t.Fatal("expected custom rules to remain available")
	}
	if autoSet.Match("example.com") {
		t.Fatal("expected missing auto-detect file to load as empty set")
	}
}

func TestFileSourceLoadCustomAndAutoDetectReturnsAutoDetectOpenError(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.txt")
	if err := os.WriteFile(customPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	source := FileSource{}
	_, _, err := source.LoadCustomAndAutoDetect(customPath, filepath.Join(customPath, "auto.txt"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "open auto-detect rules") {
		t.Fatalf("expected auto-detect open error, got %v", err)
	}
}

func TestFileSourceLoadCustomAndAutoDetectReturnsCustomParseError(t *testing.T) {
	dir := t.TempDir()
	autoPath := filepath.Join(dir, "auto.txt")
	if err := os.WriteFile(autoPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write auto-detect: %v", err)
	}

	source := FileSource{}
	_, _, err := source.LoadCustomAndAutoDetect(dir, autoPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse custom rules") {
		t.Fatalf("expected custom parse error, got %v", err)
	}
}

func TestFileSourceLoadCustomAndAutoDetectReturnsAutoDetectParseError(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.txt")
	if err := os.WriteFile(customPath, []byte("example.com\n"), 0o644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	source := FileSource{}
	_, _, err := source.LoadCustomAndAutoDetect(customPath, dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse auto-detect rules") {
		t.Fatalf("expected auto-detect parse error, got %v", err)
	}
}

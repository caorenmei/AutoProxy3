package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLoggerCreatesJSONHandler(t *testing.T) {
	logger, err := New(Options{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	if logger == nil {
		t.Fatal("expected logger")
	}
}

func TestNewHandlerCreatesJSONHandler(t *testing.T) {
	handler, err := newHandler("json", io.Discard, slog.LevelDebug)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	if _, ok := handler.(*slog.JSONHandler); !ok {
		t.Fatalf("expected JSON handler, got %T", handler)
	}
}

func TestNewHandlerCreatesTextHandler(t *testing.T) {
	handler, err := newHandler("text", io.Discard, slog.LevelInfo)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	if _, ok := handler.(*slog.TextHandler); !ok {
		t.Fatalf("expected Text handler, got %T", handler)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	_, err := New(Options{Level: "trace", Format: "json"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := `invalid level "trace"`; err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestNewRejectsInvalidFormat(t *testing.T) {
	_, err := New(Options{Level: "info", Format: "xml"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := `invalid format "xml"`; err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestNewWriterUsesStdoutWhenFilePathEmpty(t *testing.T) {
	writer, err := newWriter(Options{})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if writer != os.Stdout {
		t.Fatalf("expected stdout writer, got %T", writer)
	}
}

func TestNewWriterUsesLumberjackWhenFilePathSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autoproxy.log")
	writer, err := newWriter(Options{FilePath: path, MaxSize: 12, MaxBackups: 7})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	if got := fmt.Sprintf("%T", writer); got != "*lumberjack.Logger" {
		t.Fatalf("expected lumberjack logger, got %s", got)
	}
	logger, err := New(Options{Level: "info", Format: "text", FilePath: path, MaxSize: 12, MaxBackups: 7})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger.Info("file output")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected log file content")
	}
}

func TestNewCreatesMissingParentDirectoryForLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "nested", "autoproxy.log")

	logger, err := New(Options{Level: "info", Format: "text", FilePath: path, MaxSize: 12, MaxBackups: 7})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Info("create parent directory")

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("stat parent directory: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected log file content")
	}
}

func TestNewRejectsInvalidLogFilePath(t *testing.T) {
	path := t.TempDir()

	_, err := New(Options{Level: "info", Format: "text", FilePath: path, MaxSize: 12, MaxBackups: 7})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := "prepare log file"; !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareLogFileRejectsDirectoryCreationFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(parent, []byte("busy"), 0o644); err != nil {
		t.Fatalf("write occupied file: %v", err)
	}

	err := prepareLogFile(filepath.Join(parent, "autoproxy.log"))
	if err == nil {
		t.Fatal("expected directory preparation error")
	}
	if want := "prepare log directory"; !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareLogFileReturnsCloseError(t *testing.T) {
	originalOpenLogFile := openLogFile
	t.Cleanup(func() {
		openLogFile = originalOpenLogFile
	})

	openLogFile = func(string, int, os.FileMode) (io.WriteCloser, error) {
		return failingWriteCloser{}, nil
	}

	err := prepareLogFile("autoproxy.log")
	if err == nil {
		t.Fatal("expected close error")
	}
	if want := "close log file"; !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLevel(tc.input)
			if err != nil {
				t.Fatalf("parse level: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected level: got %v want %v", got, tc.want)
			}
		})
	}
}

type failingWriteCloser struct{}

func (failingWriteCloser) Write([]byte) (int, error) {
	return 0, nil
}

func (failingWriteCloser) Close() error {
	return errors.New("close failed")
}

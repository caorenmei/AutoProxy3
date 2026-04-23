package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Options 定义日志初始化参数。
//
// 该结构描述日志级别、输出格式、目标文件与轮转策略，
// 调用方可直接从配置层转换得到该值后传入 New。
// 当 Level 或 Format 非法时，New 会返回明确错误。
type Options struct {
	// Level 表示日志级别，仅支持 debug、info、warn、error。
	Level string
	// Format 表示日志格式，仅支持 text 或 json。
	Format string
	// FilePath 表示日志文件路径；为空时输出到标准输出。
	FilePath string
	// MaxSize 表示单个日志文件最大大小，单位为 MB。
	MaxSize int
	// MaxBackups 表示日志文件最大保留数量。
	MaxBackups int
}

// New 创建并返回结构化日志记录器。
//
// 参数 opts 指定日志级别、输出格式以及可选的文件轮转配置。
// 当 opts.FilePath 为空时日志输出到标准输出；否则输出到启用 lumberjack 轮转的文件。
// 当级别或格式不受支持时，函数返回对应错误。
func New(opts Options) (*slog.Logger, error) {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	handler, err := newHandler(opts.Format, newWriter(opts), level)
	if err != nil {
		return nil, err
	}

	return slog.New(handler), nil
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid level %q", level)
	}
}

func newWriter(opts Options) io.Writer {
	if opts.FilePath == "" {
		return os.Stdout
	}

	return &lumberjack.Logger{
		Filename:   opts.FilePath,
		MaxSize:    opts.MaxSize,
		MaxBackups: opts.MaxBackups,
	}
}

func newHandler(format string, writer io.Writer, level slog.Level) (slog.Handler, error) {
	options := &slog.HandlerOptions{Level: level}

	switch format {
	case "json":
		return slog.NewJSONHandler(writer, options), nil
	case "text":
		return slog.NewTextHandler(writer, options), nil
	default:
		return nil, fmt.Errorf("invalid format %q", format)
	}
}

// Package logger provides the hub's shared logging setup, ported from the
// legacy go-devops-gin service so both control planes follow one operational
// convention: klog for structured, leveled output with optional lumberjack
// rotation to a file.
//
// Configuration comes from the environment:
//   - LOG_FILE: if set, logs rotate to this file via lumberjack; otherwise they
//     go to stderr (suitable for container stdout capture).
//   - LOG_LEVEL: silent | error | warn | info | debug → klog verbosity (-v).
package logger

import (
	"flag"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
	"k8s.io/klog/v2"
)

// Init wires klog's output and verbosity from the environment. It must be
// called once at process start, before any log line is emitted.
func Init() {
	// Use a private flagset so klog's flags don't collide with gin's or the
	// global CommandLine flagset (which may already be initialized).
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)

	if lf := os.Getenv("LOG_FILE"); lf != "" {
		klog.SetOutput(&lumberjack.Logger{
			Filename:   lf,
			MaxSize:    50, // MB per file
			MaxBackups: 20,
			MaxAge:     28, // days
			Compress:   true,
		})
	}

	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		_ = fs.Set("v", levelToV(lvl))
	}
}

// levelToV maps the hub's human log level onto klog's numeric -v verbosity.
func levelToV(level string) string {
	switch level {
	case "debug", "info":
		return "4"
	case "warn", "warning":
		return "2"
	case "error":
		return "1"
	default:
		return "0"
	}
}

// Info logs at info level.
func Info(args ...any) { klog.Info(args...) }

// Infof logs a formatted info line.
func Infof(format string, args ...any) { klog.Infof(format, args...) }

// Warn logs at warning level.
func Warn(args ...any) { klog.Warning(args...) }

// Warnf logs a formatted warning line.
func Warnf(format string, args ...any) { klog.Warningf(format, args...) }

// Error logs at error level.
func Error(args ...any) { klog.Error(args...) }

// Errorf logs a formatted error line.
func Errorf(format string, args ...any) { klog.Errorf(format, args...) }

// Fatal logs at error level and exits the process.
func Fatal(args ...any) { klog.Fatal(args...) }

// Fatalf logs a formatted error line and exits the process.
func Fatalf(format string, args ...any) { klog.Fatalf(format, args...) }

// Flush flushes any buffered log lines. Call before process exit.
func Flush() { klog.Flush() }

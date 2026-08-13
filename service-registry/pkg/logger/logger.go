package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

type Logger struct {
	mu       sync.Mutex
	output   io.Writer
	level    Level
	prefix   string
	stdLogger *log.Logger
}

var (
	defaultLogger *Logger
	once          sync.Once
)

func init() {
	defaultLogger = New(os.Stdout, INFO, "[registry]")
}

func New(w io.Writer, level Level, prefix string) *Logger {
	l := &Logger{
		output:   w,
		level:    level,
		prefix:   prefix,
		stdLogger: log.New(w, "", 0),
	}
	return l
}

func Default() *Logger {
	return defaultLogger
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
	l.stdLogger = log.New(w, "", 0)
}

func (l *Logger) logMsg(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	levelName := levelNames[level]
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s %s %s\n", timestamp, l.prefix, levelName, msg)
	l.stdLogger.Print(line)
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.logMsg(DEBUG, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.logMsg(INFO, format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.logMsg(WARN, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.logMsg(ERROR, format, args...)
}

func (l *Logger) Fatal(format string, args ...interface{}) {
	l.logMsg(FATAL, format, args...)
	os.Exit(1)
}

func Debug(format string, args ...interface{}) { defaultLogger.Debug(format, args...) }
func Info(format string, args ...interface{})  { defaultLogger.Info(format, args...) }
func Warn(format string, args ...interface{})  { defaultLogger.Warn(format, args...) }
func Error(format string, args ...interface{}) { defaultLogger.Error(format, args...) }
func Fatal(format string, args ...interface{}) { defaultLogger.Fatal(format, args...) }

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

func (l *Logger) ParseEntry(line string) *LogEntry {
	t, err := time.Parse("2006-01-02 15:04:05.000", line[:23])
	if err != nil {
		return nil
	}
	return &LogEntry{Time: t, Level: "INFO", Message: line}
}

type MultiWriter struct {
	writers []io.Writer
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, w := range mw.writers {
		n, err = w.Write(p)
		if err != nil {
			return n, err
		}
	}
	return len(p), nil
}

func NewMultiWriter(writers ...io.Writer) *MultiWriter {
	return &MultiWriter{writers: writers}
}

func (l *Logger) WithField(key, value string) *Logger {
	newPrefix := fmt.Sprintf("%s [%s=%s]", l.prefix, key, value)
	return New(l.output, l.level, newPrefix)
}

func (l *Logger) Sync() error {
	if f, ok := l.output.(*os.File); ok {
		return f.Sync()
	}
	return nil
}

func SetGlobalLevel(level Level) {
	defaultLogger.SetLevel(level)
}

func SetGlobalOutput(w io.Writer) {
	defaultLogger.SetOutput(w)
}

func ValidateLevel(s string) Level {
	switch s {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}
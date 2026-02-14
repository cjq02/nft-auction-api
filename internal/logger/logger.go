package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	baseDir     string
	currentFile *os.File
	currentDate string
	mu          sync.Mutex
}

func NewLogger(baseDir string) (*Logger, error) {
	logger := &Logger{
		baseDir: baseDir,
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	if err := logger.ensureFile(); err != nil {
		return nil, err
	}

	return logger, nil
}

func (l *Logger) ensureFile() error {
	today := time.Now().Format("2006/01/02")

	if l.currentFile != nil && l.currentDate == today {
		return nil
	}

	if l.currentFile != nil {
		l.currentFile.Close()
		l.currentFile = nil
	}

	dateDir := filepath.Join(l.baseDir, time.Now().Format("2006/01"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return fmt.Errorf("failed to create date directory: %w", err)
	}

	fileName := time.Now().Format("02") + ".log"
	filePath := filepath.Join(dateDir, fileName)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	l.currentFile = file
	l.currentDate = today

	return nil
}

func (l *Logger) write(level, message string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureFile(); err != nil {
		return err
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMsg := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)

	if _, err := l.currentFile.WriteString(logMsg); err != nil {
		return err
	}

	fmt.Print(logMsg)
	return nil
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.write("INFO", fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.write("WARN", fmt.Sprintf(format, args...))
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.write("ERROR", fmt.Sprintf(format, args...))
}

func (l *Logger) GetWriter() io.Writer {
	return l
}

func (l *Logger) Write(p []byte) (n int, err error) {
	return l.currentFile.Write(p)
}

func (l *Logger) Close() error {
	if l.currentFile != nil {
		return l.currentFile.Close()
	}
	return nil
}

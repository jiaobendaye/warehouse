// Package logging sets up dual-output logging: stderr + rotating daily file.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

var logFile *os.File

// Init opens the log file under dataDir and configures the standard library
// log package to write to both stderr and the file. Call once at startup.
// Returns the full path of the log file.
func Init(dataDir string) (string, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	logPath := filepath.Join(dataDir, "warehouse.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}

	// Rotate if the file was created on a different day.
	if info, err := f.Stat(); err == nil && info.ModTime().Day() != time.Now().Day() {
		rotated := logPath + "." + info.ModTime().Format("2006-01-02")
		_ = f.Close()
		_ = os.Rename(logPath, rotated)
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return "", fmt.Errorf("reopen log file after rotate: %w", err)
		}
	}

	logFile = f
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags) // date + time prefix

	log.Printf("logging initialized — %s", logPath)
	return logPath, nil
}

// Close flushes and closes the log file. Safe to call multiple times.
func Close() {
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}
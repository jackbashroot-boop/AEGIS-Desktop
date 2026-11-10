package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Log is the qFlipper-style session log: a timestamped ring in memory for
// the Activity Log page plus a session file on disk that can be exported
// when something goes wrong.
type Log struct {
	mu      sync.Mutex
	lines   []string
	maxKeep int
	file    *os.File
	path    string
}

func NewLog() *Log {
	l := &Log{maxKeep: 500}
	dir := filepath.Join(exeDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		name := "companion-" + time.Now().Format("20060102-150405") + ".log"
		l.path = filepath.Join(dir, name)
		if f, err := os.Create(l.path); err == nil {
			l.file = f
		}
	}
	l.add("INFO", "AEGIS Companion "+appVersion+" started (protocol AEGIS/1)")
	return l
}

func (l *Log) add(level, msg string) {
	line := fmt.Sprintf("%s %-5s %s", time.Now().Format("15:04:05"), level, msg)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.maxKeep {
		l.lines = append([]string{}, l.lines[len(l.lines)-l.maxKeep:]...)
	}
	if l.file != nil {
		fmt.Fprintln(l.file, line)
	}
	l.mu.Unlock()
	// stdout too, so --headless runs are useful in a terminal.
	fmt.Println(line)
}

func (l *Log) Infof(format string, args ...any)  { l.add("INFO", fmt.Sprintf(format, args...)) }
func (l *Log) Warnf(format string, args ...any)  { l.add("WARN", fmt.Sprintf(format, args...)) }
func (l *Log) Errorf(format string, args ...any) { l.add("ERROR", fmt.Sprintf(format, args...)) }

func (l *Log) Snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.lines...)
}

func (l *Log) Path() string { return l.path }

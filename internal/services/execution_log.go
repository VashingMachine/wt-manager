package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type executionLog struct {
	mu     sync.Mutex
	path   string
	err    error
	nextID int
}

type executionLogContextKey struct{}

func (s *Service) commandContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, executionLogContextKey{}, &s.sessionLog)
}

// ReadSessionLog returns a consistent snapshot and the temporary file to inspect outside the TUI.
func (s *Service) ReadSessionLog() (string, string, error) {
	l := &s.sessionLog
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureFileLocked()
	if l.err != nil {
		return l.path, "", l.err
	}
	data, err := os.ReadFile(l.path)
	return l.path, string(data), err
}

func (s *Service) RecordSessionEvent(message string, failed bool) {
	level := "INFO"
	if failed {
		level = "ERROR"
	}
	l := &s.sessionLog
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appendLocked(fmt.Sprintf("[%s] %s %s\n", time.Now().Format(time.RFC3339Nano), level, message))
}

func (l *executionLog) ensureFileLocked() {
	if l.path != "" || l.err != nil {
		return
	}
	file, err := os.CreateTemp("", "wt-manager-*.log")
	if err != nil {
		l.err = fmt.Errorf("create session log: %w", err)
		return
	}
	l.path = file.Name()
	_, l.err = fmt.Fprintf(file, "wt-manager session started %s (pid %d)\n", time.Now().Format(time.RFC3339Nano), os.Getpid())
	if err := file.Close(); l.err == nil {
		l.err = err
	}
}

func (l *executionLog) appendLocked(record string) {
	l.ensureFileLocked()
	if l.err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.err = err
		return
	}
	_, l.err = file.WriteString(record)
	if err := file.Close(); l.err == nil {
		l.err = err
	}
}

func (l *executionLog) begin(cmd *exec.Cmd) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	args := make([]string, len(cmd.Args))
	for i, arg := range cmd.Args {
		args[i] = shellQuote(arg)
	}
	dir := cmd.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	l.appendLocked(fmt.Sprintf("\n[%s] START #%d\nDirectory: %s\nCommand: %s\n", time.Now().Format(time.RFC3339Nano), l.nextID, dir, strings.Join(args, " ")))
	return l.nextID
}

func (l *executionLog) finish(id int, started time.Time, output []byte, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := "SUCCESS"
	if err != nil {
		result = "ERROR"
	}
	record := fmt.Sprintf("[%s] %s #%d (%s)\n", time.Now().Format(time.RFC3339Nano), result, id, time.Since(started).Round(time.Millisecond))
	if len(output) > 0 {
		record += "Output (stdout + stderr):\n" + string(output)
		if output[len(output)-1] != '\n' {
			record += "\n"
		}
	}
	if err != nil {
		record += "Error: " + err.Error() + "\n"
	}
	l.appendLocked(record)
}

func loggedCombinedOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	l, _ := ctx.Value(executionLogContextKey{}).(*executionLog)
	if l == nil {
		return cmd.CombinedOutput()
	}
	started := time.Now()
	id := l.begin(cmd)
	output, err := cmd.CombinedOutput()
	logErr := err
	if err != nil && ctx.Err() != nil {
		logErr = fmt.Errorf("%w: %w", err, ctx.Err())
	}
	l.finish(id, started, output, logErr)
	return output, err
}

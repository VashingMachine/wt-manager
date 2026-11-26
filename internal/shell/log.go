package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Log is the record of one wt-manager session: every external command run
// with a context from Context, with its arguments, directory, output and
// result, plus the events passed to Record. It is kept in a private temporary
// file created on first use. The zero value is ready to use.
type Log struct {
	mu     sync.Mutex
	path   string
	err    error
	nextID int
}

type logContextKey struct{}

// Context returns a context whose commands are recorded in the log.
func (l *Log) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, logContextKey{}, l)
}

// Read returns the path of the log file and a consistent snapshot of it.
func (l *Log) Read() (string, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureFileLocked()
	if l.err != nil {
		return l.path, "", l.err
	}
	data, err := os.ReadFile(l.path)
	return l.path, string(data), err
}

// Record notes something that happened outside a command, such as a status
// message shown to the user.
func (l *Log) Record(message string, failed bool) {
	level := "INFO"
	if failed {
		level = "ERROR"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appendLocked(fmt.Sprintf("[%s] %s %s\n", time.Now().Format(time.RFC3339Nano), level, message))
}

func (l *Log) ensureFileLocked() {
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

func (l *Log) appendLocked(record string) {
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

func (l *Log) begin(cmd *exec.Cmd) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	args := make([]string, len(cmd.Args))
	for i, arg := range cmd.Args {
		args[i] = Quote(arg)
	}
	dir := cmd.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	l.appendLocked(fmt.Sprintf("\n[%s] START #%d\nDirectory: %s\nCommand: %s\n", time.Now().Format(time.RFC3339Nano), l.nextID, dir, strings.Join(args, " ")))
	return l.nextID
}

func (l *Log) finish(id int, started time.Time, output []byte, err error) {
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

// CombinedOutput runs cmd and returns its stdout and stderr together, recording
// the run in the session log carried by ctx, if any.
func CombinedOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	l, _ := ctx.Value(logContextKey{}).(*Log)
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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type logEntry struct {
	Time  time.Time  `json:"time"`
	Level slog.Level `json:"level"`
	Msg   string     `json:"msg"`
	Attrs string     `json:"attrs"`
}

type logMsg struct{ entry logEntry }

const logRingBufCap = 200

var (
	logRingMu  sync.RWMutex
	logRingBuf []logEntry
)

func appendLogEntry(e logEntry) {
	logRingMu.Lock()
	defer logRingMu.Unlock()
	logRingBuf = append(logRingBuf, e)
	if len(logRingBuf) > logRingBufCap {
		logRingBuf = logRingBuf[len(logRingBuf)-logRingBufCap:]
	}
}

func getLogEntries() []logEntry {
	logRingMu.RLock()
	defer logRingMu.RUnlock()
	out := make([]logEntry, len(logRingBuf))
	copy(out, logRingBuf)
	return out
}

type tuiHandler struct {
	program *tea.Program
	level   slog.Level
}

func newTuiHandler(p *tea.Program, level slog.Level) *tuiHandler {
	return &tuiHandler{program: p, level: level}
}

func (h *tuiHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *tuiHandler) Handle(_ context.Context, r slog.Record) error {
	e := logEntry{
		Time:  r.Time,
		Level: r.Level,
		Msg:   r.Message,
		Attrs: attrsFromRecord(r),
	}
	appendLogEntry(e)
	if h.program != nil {
		h.program.Send(logMsg{entry: e})
	} else {
		// Write to stderr when running in webui-only mode (program == nil).
		var sb strings.Builder
		fmt.Fprintf(&sb, "%s %s %s", e.Time.Format("15:04:05"), e.Level, e.Msg)
		if e.Attrs != "" {
			fmt.Fprintf(&sb, " %s", e.Attrs)
		}
		sb.WriteByte('\n')
		os.Stderr.WriteString(sb.String())
	}
	return nil
}

func (h *tuiHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *tuiHandler) WithGroup(_ string) slog.Handler {
	return h
}

func attrsFromRecord(r slog.Record) string {
	var parts []string
	r.Attrs(func(a slog.Attr) bool {
		parts = append(parts, a.String())
		return true
	})
	return strings.Join(parts, " ")
}

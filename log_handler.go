package main

import (
	"context"
	"log/slog"
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
}

func newTuiHandler(p *tea.Program) *tuiHandler {
	return &tuiHandler{program: p}
}

func (h *tuiHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
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

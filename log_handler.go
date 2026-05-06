package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type logEntry struct {
	t     time.Time
	level slog.Level
	msg   string
	attrs string
}

type logMsg struct{ entry logEntry }

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
	if h.program == nil {
		return nil
	}
	h.program.Send(logMsg{entry: logEntry{
		t:     r.Time,
		level: r.Level,
		msg:   r.Message,
		attrs: attrsFromRecord(r),
	}})
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

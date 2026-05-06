package main

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestTuiHandlerEnabledAllLevels(t *testing.T) {
	h := &tuiHandler{}
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if !h.Enabled(context.Background(), level) {
			t.Errorf("expected Enabled=true for level %v", level)
		}
	}
}

func TestTuiHandlerHandleNilProgram(t *testing.T) {
	h := &tuiHandler{program: nil}
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	r.AddAttrs(slog.String("key", "value"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Errorf("unexpected error with nil program: %v", err)
	}
}

func TestTuiHandlerWithAttrsReturnsHandler(t *testing.T) {
	h := &tuiHandler{}
	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if h2 == nil {
		t.Error("WithAttrs returned nil")
	}
}

func TestTuiHandlerWithGroupReturnsHandler(t *testing.T) {
	h := &tuiHandler{}
	h2 := h.WithGroup("mygroup")
	if h2 == nil {
		t.Error("WithGroup returned nil")
	}
}

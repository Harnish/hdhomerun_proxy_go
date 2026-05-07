package main

import (
	"context"
	"fmt"
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
	resetLogRingBuf()
	h := &tuiHandler{program: nil}
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	r.AddAttrs(slog.String("key", "value"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Errorf("unexpected error with nil program: %v", err)
	}
	entries := getLogEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry in ring buffer, got %d", len(entries))
	}
	if entries[0].Msg != "test message" {
		t.Errorf("expected msg 'test message', got %q", entries[0].Msg)
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

func TestAppendAndGetLogEntries(t *testing.T) {
	resetLogRingBuf()
	appendLogEntry(logEntry{Msg: "first"})
	appendLogEntry(logEntry{Msg: "second"})
	entries := getLogEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Msg != "first" || entries[1].Msg != "second" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestLogRingBufCap(t *testing.T) {
	resetLogRingBuf()
	for i := 0; i < logRingBufCap+10; i++ {
		appendLogEntry(logEntry{Msg: fmt.Sprintf("msg%d", i)})
	}
	entries := getLogEntries()
	if len(entries) != logRingBufCap {
		t.Fatalf("expected %d entries, got %d", logRingBufCap, len(entries))
	}
	if entries[0].Msg != fmt.Sprintf("msg%d", 10) {
		t.Errorf("expected msg10, got %s", entries[0].Msg)
	}
}

func TestGetLogEntriesReturnsCopy(t *testing.T) {
	resetLogRingBuf()
	appendLogEntry(logEntry{Msg: "original"})
	entries := getLogEntries()
	entries[0].Msg = "mutated"
	entries2 := getLogEntries()
	if entries2[0].Msg != "original" {
		t.Error("getLogEntries should return a copy, not a reference")
	}
}

// resetLogRingBuf clears the package-level ring buffer for test isolation.
func resetLogRingBuf() {
	logRingMu.Lock()
	logRingBuf = nil
	logRingMu.Unlock()
}

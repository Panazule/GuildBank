package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(&buf, "debug", "json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hello", "request_id", "abc")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output is not JSON: %v (%s)", err, buf.String())
	}
	if entry["msg"] != "hello" || entry["request_id"] != "abc" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry["level"] != "INFO" {
		t.Fatalf("level = %v", entry["level"])
	}
}

func TestNewRejectsUnknownValues(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "verbose", "json"); err == nil {
		t.Fatal("expected invalid level error")
	}
	if _, err := New(&bytes.Buffer{}, "info", "xml"); err == nil {
		t.Fatal("expected invalid format error")
	}
}

func TestParseLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestRequestIDContext(t *testing.T) {
	ctx := WithRequestID(t.Context(), "abc123")
	if got := RequestID(ctx); got != "abc123" {
		t.Fatalf("RequestID = %q", got)
	}
	if got := RequestID(t.Context()); got != "" {
		t.Fatalf("empty context returned %q", got)
	}
}

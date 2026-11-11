package middleware

import (
	"encoding/json"
	"log/slog"
	"testing"
)

func decodeErrorBody(t *testing.T, raw []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("body is not JSON: %s", raw)
	}
	return envelope.Error.Code
}

func testLimiterLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(discardBuffer(), nil))
}

func discardBuffer() *discardWriter {
	return &discardWriter{}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

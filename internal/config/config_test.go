package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseEnvFile(t *testing.T) {
	path := writeEnvFile(t, `
# a comment
LISTEN_ADDR=:9090
QUOTED="hello world"
SINGLE='v1'
EMPTY=
NOEQUALS
=
SPACED  =  padded value
`)
	got := parseEnvFile(path)
	want := map[string]string{
		"LISTEN_ADDR": ":9090",
		"QUOTED":      "hello world",
		"SINGLE":      "v1",
		"EMPTY":       "",
		"SPACED":      "padded value",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %v, want %v", got, want)
	}
}

func TestParseEnvFileMissing(t *testing.T) {
	if got := parseEnvFile(""); len(got) != 0 {
		t.Fatalf("empty path = %v, want nil map", got)
	}
	if got := parseEnvFile("/nonexistent/.env"); len(got) != 0 {
		t.Fatalf("missing file = %v, want empty", got)
	}
}

func TestLookupPrecedence(t *testing.T) {
	file := map[string]string{"FOO": "from-file"}
	t.Setenv("FOO", "from-env")
	if got := lookup("FOO", "default", file); got != "from-env" {
		t.Fatalf("env should win, got %q", got)
	}
	os.Unsetenv("FOO")
	if got := lookup("FOO", "default", file); got != "from-file" {
		t.Fatalf("file should win over default, got %q", got)
	}
	if got := lookup("MISSING", "default", file); got != "default" {
		t.Fatalf("default expected, got %q", got)
	}
	if got := lookup("EMPTY_ENV", "default", file); got != "default" {
		t.Fatalf("empty env should fall through, got %q", got)
	}
}

func TestForAPIValidation(t *testing.T) {
	s := &Settings{}
	if err := s.ForAPI(); err == nil {
		t.Fatal("empty settings should fail ForAPI")
	}
	ok := &Settings{
		LLMBaseURL:     "https://api.groq.com/openai/v1",
		LLMAPIKey:      "k",
		LLMModelSQL:    "m1",
		LLMModelAnswer: "m2",
		DatabaseURLRO:  "postgres://x",
		RedisURL:       "redis://x",
	}
	if err := ok.ForAPI(); err != nil {
		t.Fatalf("full settings should pass ForAPI: %v", err)
	}
}

func TestForIngestValidation(t *testing.T) {
	if err := (&Settings{}).ForIngest(); err == nil {
		t.Fatal("empty settings should fail ForIngest")
	}
	if err := (&Settings{DatabaseURLRW: "postgres://x"}).ForIngest(); err != nil {
		t.Fatalf("valid settings should pass ForIngest: %v", err)
	}
}

func TestFindEnvFileWalksUp(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("A=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if got := findEnvFile(); got != envPath {
		t.Fatalf("findEnvFile = %q, want %q", got, envPath)
	}
}

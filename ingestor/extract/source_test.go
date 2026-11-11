package extract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sources.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

type stubSource struct {
	def SourceDef
}

func (s stubSource) Def() SourceDef { return s.def }
func (s stubSource) Fetch(ctx context.Context, stream string, cursor map[string]any) (Batch, error) {
	return Batch{}, nil
}

func TestLoadSourcesBuildsFromRegistry(t *testing.T) {
	path := writeFile(t, "sources:\n  - id: synthetic\n    type: synthetic\n    streams: [snapshot]\n    params:\n      league_id: \"4328\"\n      code: EPL\n")
	var received map[string]string
	factories := map[string]Factory{
		"synthetic": func(def SourceDef) (Source, error) {
			received = def.Params
			return stubSource{def: def}, nil
		},
	}
	sources, err := LoadSources(path, factories)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(sources))
	}
	if sources[0].Def().ID != "synthetic" {
		t.Errorf("id = %q", sources[0].Def().ID)
	}
	if received["league_id"] != "4328" || received["code"] != "EPL" {
		t.Errorf("params not passed through: %v", received)
	}
}

func TestLoadSourcesRejectsBadConfigs(t *testing.T) {
	factories := map[string]Factory{"ok": func(def SourceDef) (Source, error) { return stubSource{def: def}, nil }}
	cases := []struct {
		name    string
		content string
	}{
		{"empty file", "sources: []\n"},
		{"unknown type", "sources:\n  - id: a\n    type: mystery\n    streams: [s]\n"},
		{"missing id", "sources:\n  - type: ok\n    streams: [s]\n"},
		{"missing streams", "sources:\n  - id: a\n    type: ok\n"},
		{"duplicate id", "sources:\n  - id: a\n    type: ok\n    streams: [s]\n  - id: a\n    type: ok\n    streams: [s]\n"},
		{"broken yaml", "sources: [ {,}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFile(t, testCase.content)
			if _, err := LoadSources(path, factories); err == nil {
				t.Errorf("expected failure for %q", testCase.name)
			}
		})
	}
}

func TestLoadSourcesMissingFile(t *testing.T) {
	if _, err := LoadSources(filepath.Join(t.TempDir(), "absent.yaml"), nil); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want not-exist", err)
	}
}

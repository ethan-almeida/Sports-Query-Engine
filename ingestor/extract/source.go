package extract

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"sports-analytics/ingestor/model"
)

type Batch struct {
	Stream string
	Cursor map[string]any
	Rows   []model.Row
	Done   bool
}

type SourceDef struct {
	ID      string            `yaml:"id"`
	Type    string            `yaml:"type"`
	Streams []string          `yaml:"streams"`
	Params  map[string]string `yaml:"params"`
}

type Source interface {
	Def() SourceDef
	Fetch(ctx context.Context, stream string, cursor map[string]any) (Batch, error)
}

type Factory func(def SourceDef) (Source, error)

type fileFormat struct {
	Sources []SourceDef `yaml:"sources"`
}

func LoadSources(path string, factories map[string]Factory) ([]Source, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("extract: load %q: %w", path, err)
	}
	var parsed fileFormat
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("extract: parse %q: %w", path, err)
	}
	if len(parsed.Sources) == 0 {
		return nil, fmt.Errorf("extract: %q declares no sources", path)
	}

	sources := make([]Source, 0, len(parsed.Sources))
	seen := map[string]bool{}
	for _, def := range parsed.Sources {
		if def.ID == "" || def.Type == "" || len(def.Streams) == 0 {
			return nil, fmt.Errorf("extract: source %q must define id, type and streams", def.ID)
		}
		if seen[def.ID] {
			return nil, fmt.Errorf("extract: duplicate source id %q", def.ID)
		}
		seen[def.ID] = true
		factory, ok := factories[def.Type]
		if !ok {
			return nil, fmt.Errorf("extract: unknown source type %q for id %q", def.Type, def.ID)
		}
		source, err := factory(def)
		if err != nil {
			return nil, fmt.Errorf("extract: building %q: %w", def.ID, err)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

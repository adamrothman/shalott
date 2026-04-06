package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/adamrothman/shalott/internal/config"
)

func TestParse_MinimalValid(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: music
    source: /mnt/music
    target: /opt/roon/library
    paths:
      - prefix: Artists
        depth: 2
`)
	cfg, err := config.Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Mirrors) != 1 {
		t.Fatalf("expected 1 mirror, got %d", len(cfg.Mirrors))
	}
	m := cfg.Mirrors[0]
	if m.Name != "music" {
		t.Errorf("expected name 'music', got %q", m.Name)
	}
	if m.Source != "/mnt/music" {
		t.Errorf("expected source '/mnt/music', got %q", m.Source)
	}
	if m.Target != "/opt/roon/library" {
		t.Errorf("expected target '/opt/roon/library', got %q", m.Target)
	}
}

func TestParse_Defaults(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    source: /src
    target: /tgt
`)
	cfg, err := config.Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := cfg.Mirrors[0]
	if m.PollInterval.Duration != 30*time.Second {
		t.Errorf("expected poll_interval 30s, got %v", m.PollInterval.Duration)
	}
	if m.Debounce.Duration != 2*time.Minute {
		t.Errorf("expected debounce 2m, got %v", m.Debounce.Duration)
	}
	if len(m.Paths) != 1 || m.Paths[0].Prefix != "" || m.Paths[0].Depth != 1 {
		t.Errorf("expected default paths [{prefix:'', depth:1}], got %+v", m.Paths)
	}
}

func TestParse_DurationParsing(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    source: /src
    target: /tgt
    poll_interval: 5m
    debounce: 30s
    paths:
      - prefix: data
        depth: 1
`)
	cfg, err := config.Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := cfg.Mirrors[0]
	if m.PollInterval.Duration != 5*time.Minute {
		t.Errorf("expected poll_interval 5m, got %v", m.PollInterval.Duration)
	}
	if m.Debounce.Duration != 30*time.Second {
		t.Errorf("expected debounce 30s, got %v", m.Debounce.Duration)
	}
}

func TestParse_MissingName(t *testing.T) {
	yaml := []byte(`
mirrors:
  - source: /src
    target: /tgt
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParse_MissingSource(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    target: /tgt
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestParse_MissingTarget(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    source: /src
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
}

func TestParse_DepthZero(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    source: /src
    target: /tgt
    paths:
      - prefix: data
        depth: 0
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for depth 0")
	}
}

func TestParse_DepthNegative(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    source: /src
    target: /tgt
    paths:
      - prefix: data
        depth: -1
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for negative depth")
	}
}

func TestParse_NoMirrors(t *testing.T) {
	yaml := []byte(`mirrors: []`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for empty mirrors")
	}
}

func TestParse_DuplicateNames(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: music
    source: /src1
    target: /tgt1
  - name: music
    source: /src2
    target: /tgt2
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestParse_OverlappingSources(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: a
    source: /mnt/data
    target: /tgt1
  - name: b
    source: /mnt/data/sub
    target: /tgt2
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for overlapping sources")
	}
}

func TestParse_OverlappingTargets(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: a
    source: /src1
    target: /opt/lib
  - name: b
    source: /src2
    target: /opt/lib/sub
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for overlapping targets")
	}
}

func TestParse_IdenticalSources(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: a
    source: /mnt/data
    target: /tgt1
  - name: b
    source: /mnt/data
    target: /tgt2
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for identical sources")
	}
}

func TestParse_EmptyPrefixExplicit(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: test
    source: /src
    target: /tgt
    paths:
      - prefix: ""
        depth: 1
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatal("expected error for empty prefix when explicitly listed")
	}
}

func TestParse_NonOverlappingMirrors(t *testing.T) {
	yaml := []byte(`
mirrors:
  - name: music
    source: /mnt/music
    target: /opt/roon
    paths:
      - prefix: Artists
        depth: 2
  - name: photos
    source: /mnt/photos
    target: /opt/immich
    paths:
      - prefix: "2026"
        depth: 1
`)
	_, err := config.Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	yaml := `
mirrors:
  - name: test
    source: /src
    target: /tgt
    paths:
      - prefix: data
        depth: 1
`
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mirrors[0].Name != "test" {
		t.Errorf("expected name 'test', got %q", cfg.Mirrors[0].Name)
	}
}

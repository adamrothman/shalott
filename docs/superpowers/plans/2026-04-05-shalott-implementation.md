# shalott Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a polling daemon that maintains a local symlink tree mirroring a remote NFS source, generating inotify events for watching applications.

**Architecture:** Single binary daemon with per-mirror goroutines. Config is loaded from YAML at startup. Each mirror goroutine polls its source on a ticker, diffs against the local target tree, and creates/prunes symlinks. Context threading enables clean shutdown. No database — the symlink tree is the state.

**Tech Stack:** Go 1.23+, `gopkg.in/yaml.v3`, `github.com/go-playground/validator/v10`, `log/slog`, standard library for everything else.

**Spec:** `docs/superpowers/specs/2026-04-05-shalott-design.md`

---

## File Structure

```
shalott/
├── cmd/shalott/
│   └── main.go              # CLI flags, signal handling, mirror orchestration
├── internal/
│   ├── config/
│   │   ├── config.go        # Config/Mirror/Path types, YAML loading, defaults, validation
│   │   └── config_test.go   # Config loading, defaults, validation (all cases)
│   └── mirror/
│       ├── scan.go           # walkToDepth helper, ScanSource function
│       ├── scan_test.go      # Depth walking, debounce, symlink skipping, context cancellation
│       ├── sync.go           # DiffResult/Entry types, Diff, Create, Prune functions
│       ├── sync_test.go      # Diff classification, symlink creation, orphan pruning, parent cleanup
│       ├── mirror.go         # Mirror struct, RunOnce, Run (daemon loop)
│       └── mirror_test.go    # Health check, full scan cycle, daemon loop, force scan
├── deploy/
│   └── shalott.service       # systemd unit file
├── go.mod
└── go.sum
```

---

### Task 1: Project Skeleton

**Files:**
- Create: `go.mod`
- Create: `cmd/shalott/main.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/adam/src/shalott
go mod init shalott
```

- [ ] **Step 2: Create minimal main.go**

Create `cmd/shalott/main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("shalott")
}
```

- [ ] **Step 3: Verify it builds**

```bash
go build -o shalott ./cmd/shalott
./shalott
```

Expected: prints `shalott`

- [ ] **Step 4: Clean up and commit**

```bash
rm ./shalott
git add go.mod cmd/shalott/main.go
git commit -m "Initialize Go module and project skeleton"
```

---

### Task 2: Config Types, Loading, and Validation

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for config loading**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"testing"
	"time"

	"shalott/internal/config"
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
	if err := writeFile(path, []byte("not: [valid: yaml")); err != nil {
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
	if err := writeFile(path, []byte(yaml)); err != nil {
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

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
```

Wait — I can't use `__import__os` obviously. Let me fix that test helper. Actually, I'll just use `os.WriteFile` directly since we import `os` in the test file.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/ -v
```

Expected: compilation errors (package doesn't exist yet)

- [ ] **Step 3: Install dependencies**

```bash
go get gopkg.in/yaml.v3
go get github.com/go-playground/validator/v10
```

- [ ] **Step 4: Implement config package**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// Config is the top-level shalott configuration.
type Config struct {
	Mirrors []Mirror `yaml:"mirrors" validate:"required,min=1,dive"`
}

// Mirror defines a single source-to-target mirroring configuration.
type Mirror struct {
	Name         string   `yaml:"name" validate:"required"`
	Source       string   `yaml:"source" validate:"required"`
	Target       string   `yaml:"target" validate:"required"`
	PollInterval Duration `yaml:"poll_interval"`
	Debounce     Duration `yaml:"debounce"`
	Paths        []Path   `yaml:"paths" validate:"dive"`
}

// Path defines a prefix and depth within a mirror.
type Path struct {
	Prefix string `yaml:"prefix"`
	Depth  int    `yaml:"depth" validate:"gt=0"`
}

// Duration wraps time.Duration for YAML unmarshaling from strings like "30s".
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

// Load reads and parses a config file from disk.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse parses config from YAML bytes.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Check empty prefixes before applying defaults
	// (default paths use empty prefix which is valid)
	if err := checkEmptyPrefixes(&cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(c *Config) {
	for i := range c.Mirrors {
		m := &c.Mirrors[i]
		if m.PollInterval.Duration == 0 {
			m.PollInterval.Duration = 30 * time.Second
		}
		if m.Debounce.Duration == 0 {
			m.Debounce.Duration = 2 * time.Minute
		}
		if m.Paths == nil {
			m.Paths = []Path{{Prefix: "", Depth: 1}}
		}
	}
}

func validate(c *Config) error {
	v := validator.New()
	if err := v.Struct(c); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	if err := checkDuplicateNames(c); err != nil {
		return err
	}
	if err := checkOverlaps(c); err != nil {
		return err
	}
	return nil
}

func checkEmptyPrefixes(c *Config) error {
	for _, m := range c.Mirrors {
		if m.Paths == nil {
			continue
		}
		for _, p := range m.Paths {
			if p.Prefix == "" {
				return fmt.Errorf("mirror %q: path prefix cannot be empty when explicitly listed", m.Name)
			}
		}
	}
	return nil
}

func checkDuplicateNames(c *Config) error {
	seen := make(map[string]bool)
	for _, m := range c.Mirrors {
		if seen[m.Name] {
			return fmt.Errorf("duplicate mirror name: %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}

func checkOverlaps(c *Config) error {
	for i := 0; i < len(c.Mirrors); i++ {
		for j := i + 1; j < len(c.Mirrors); j++ {
			mi, mj := c.Mirrors[i], c.Mirrors[j]
			if pathsOverlap(mi.Source, mj.Source) {
				return fmt.Errorf("mirrors %q and %q have overlapping sources: %s, %s",
					mi.Name, mj.Name, mi.Source, mj.Source)
			}
			if pathsOverlap(mi.Target, mj.Target) {
				return fmt.Errorf("mirrors %q and %q have overlapping targets: %s, %s",
					mi.Name, mj.Name, mi.Target, mj.Target)
			}
		}
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	aSep := a + string(filepath.Separator)
	bSep := b + string(filepath.Separator)
	return strings.HasPrefix(bSep, aSep) || strings.HasPrefix(aSep, bSep)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/config/ -v
```

Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "Add config loading with YAML parsing, defaults, and validation"
```

---

### Task 3: Source Scanner

**Files:**
- Create: `internal/mirror/scan.go`
- Create: `internal/mirror/scan_test.go`

- [ ] **Step 1: Write failing tests for source scanning**

Create `internal/mirror/scan_test.go`:

```go
package mirror_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"shalott/internal/mirror"
)

// mkdirs creates directories under root from a list of relative paths.
func mkdirs(t *testing.T, root string, dirs []string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanSource_Depth1(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{
		"Soundtracks/TRON Legacy",
		"Soundtracks/Interstellar",
	})

	got, err := mirror.ScanSource(context.Background(), root, 1, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join(root, "Soundtracks")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_Depth2(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{
		"Robyn/Sexistential",
		"Robyn/Honey",
		"Charlie Puth/Whatever's Clever",
	})

	got, err := mirror.ScanSource(context.Background(), root, 2, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		filepath.Join(root, "Charlie Puth", "Whatever's Clever"),
		filepath.Join(root, "Robyn", "Honey"),
		filepath.Join(root, "Robyn", "Sexistential"),
	}
	assertPathsEqual(t, got, want)
}

func TestScanSource_SkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"real-dir"})

	// Create a symlink at depth 1
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}

	got, err := mirror.ScanSource(context.Background(), root, 1, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join(root, "real-dir")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_SkipsSymlinksAtIntermediateDepth(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"real/album"})

	// Create a symlink at depth 1 that contains a depth-2 dir
	linkTarget := t.TempDir()
	mkdirs(t, linkTarget, []string{"album"})
	if err := os.Symlink(linkTarget, filepath.Join(root, "linked-artist")); err != nil {
		t.Fatal(err)
	}

	got, err := mirror.ScanSource(context.Background(), root, 2, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join(root, "real", "album")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_Debounce(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"old-dir", "new-dir"})

	now := time.Now()

	// Set old-dir mtime to 10 minutes ago
	oldTime := now.Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(root, "old-dir"), oldTime, oldTime)

	// new-dir mtime is ~now (just created)

	got, err := mirror.ScanSource(context.Background(), root, 1, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only old-dir should pass debounce (mtime > 5 min ago)
	want := []string{filepath.Join(root, "old-dir")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"a", "b", "c", "d", "e"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := mirror.ScanSource(ctx, root, 1, 0, time.Now())
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestScanSource_EmptyDir(t *testing.T) {
	root := t.TempDir()

	got, err := mirror.ScanSource(context.Background(), root, 1, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestScanSource_NonexistentDir(t *testing.T) {
	_, err := mirror.ScanSource(context.Background(), "/nonexistent", 1, 0, time.Now())
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func assertPathsEqual(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("expected %d paths, got %d:\n  want: %v\n  got:  %v", len(want), len(got), want, got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("path %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/mirror/ -v
```

Expected: compilation errors (package doesn't exist yet)

- [ ] **Step 3: Implement walkToDepth and ScanSource**

Create `internal/mirror/scan.go`:

```go
package mirror

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ScanSource walks sourceDir to exactly depth levels deep, returning absolute
// paths of real directories found at that depth. Skips symlinks and directories
// with mtime newer than now-debounce.
func ScanSource(ctx context.Context, sourceDir string, depth int, debounce time.Duration, now time.Time) ([]string, error) {
	var results []string
	err := walkToDepth(ctx, sourceDir, depth, func(path string, d fs.DirEntry) error {
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if debounce > 0 {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if now.Sub(info.ModTime()) < debounce {
				return nil
			}
		}
		results = append(results, path)
		return nil
	})
	return results, err
}

// walkToDepth walks dir to exactly depth levels, calling fn for each entry at
// that depth. At intermediate levels, only real directories are descended into
// (symlinks are never followed). Checks for context cancellation between entries.
func walkToDepth(ctx context.Context, dir string, depth int, fn func(path string, d fs.DirEntry) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		path := filepath.Join(dir, e.Name())

		if depth == 1 {
			if err := fn(path, e); err != nil {
				return err
			}
			continue
		}

		// Intermediate level: only descend into real directories
		if e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if !e.IsDir() {
			continue
		}

		if err := walkToDepth(ctx, path, depth-1, fn); err != nil {
			return err
		}
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/mirror/ -v
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/mirror/scan.go internal/mirror/scan_test.go
git commit -m "Add source scanner with depth walking, debounce, and symlink skipping"
```

---

### Task 4: Diff

**Files:**
- Create: `internal/mirror/sync.go`
- Create: `internal/mirror/sync_test.go`

- [ ] **Step 1: Write failing tests for Diff**

Create `internal/mirror/sync_test.go`:

```go
package mirror_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"shalott/internal/mirror"
)

func TestDiff_AllNew(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey", "Robyn/Sexistential"})

	scanned := []string{
		filepath.Join(sourceDir, "Robyn", "Honey"),
		filepath.Join(sourceDir, "Robyn", "Sexistential"),
	}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 2 {
		t.Fatalf("expected 2 new entries, got %d", len(result.New))
	}
	if len(result.Orphan) != 0 {
		t.Errorf("expected 0 orphans, got %d", len(result.Orphan))
	}
	if len(result.Valid) != 0 {
		t.Errorf("expected 0 valid, got %d", len(result.Valid))
	}
}

func TestDiff_AllValid(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey"})

	// Create matching symlink in target
	mkdirs(t, targetDir, []string{"Robyn"})
	os.Symlink(
		filepath.Join(sourceDir, "Robyn", "Honey"),
		filepath.Join(targetDir, "Robyn", "Honey"),
	)

	scanned := []string{filepath.Join(sourceDir, "Robyn", "Honey")}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 0 {
		t.Errorf("expected 0 new, got %d", len(result.New))
	}
	if len(result.Valid) != 1 {
		t.Errorf("expected 1 valid, got %d", len(result.Valid))
	}
}

func TestDiff_Orphans(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Symlink in target pointing to a source that no longer exists
	mkdirs(t, targetDir, []string{"Robyn"})
	os.Symlink("/nonexistent/source", filepath.Join(targetDir, "Robyn", "Deleted"))

	// Empty scan result — nothing in source
	var scanned []string

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Orphan) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(result.Orphan))
	}
	if result.Orphan[0].TargetPath != filepath.Join(targetDir, "Robyn", "Deleted") {
		t.Errorf("unexpected orphan path: %s", result.Orphan[0].TargetPath)
	}
}

func TestDiff_ForeignEntry(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"real"})

	// Create a real directory in target where a symlink should be
	mkdirs(t, targetDir, []string{"foreign"})

	scanned := []string{filepath.Join(sourceDir, "real")}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Foreign) != 1 {
		t.Errorf("expected 1 foreign entry, got %d", len(result.Foreign))
	}
	if len(result.New) != 1 {
		t.Errorf("expected 1 new entry, got %d", len(result.New))
	}
}

func TestDiff_MixedState(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"new-album", "existing-album"})

	// existing-album has a valid symlink
	os.Symlink(
		filepath.Join(sourceDir, "existing-album"),
		filepath.Join(targetDir, "existing-album"),
	)
	// orphan has a stale symlink
	os.Symlink("/gone", filepath.Join(targetDir, "orphan-album"))

	scanned := []string{
		filepath.Join(sourceDir, "new-album"),
		filepath.Join(sourceDir, "existing-album"),
	}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 1 {
		t.Errorf("expected 1 new, got %d", len(result.New))
	}
	if len(result.Valid) != 1 {
		t.Errorf("expected 1 valid, got %d", len(result.Valid))
	}
	if len(result.Orphan) != 1 {
		t.Errorf("expected 1 orphan, got %d", len(result.Orphan))
	}
}

func TestDiff_TargetDirNotExist(t *testing.T) {
	sourceDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"album"})
	targetDir := filepath.Join(t.TempDir(), "nonexistent")

	scanned := []string{filepath.Join(sourceDir, "album")}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 1 {
		t.Errorf("expected 1 new, got %d", len(result.New))
	}
}

func sortEntries(entries []mirror.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TargetPath < entries[j].TargetPath
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/mirror/ -v -run TestDiff
```

Expected: compilation errors (Diff, DiffResult, Entry not defined yet)

- [ ] **Step 3: Implement Diff**

Create `internal/mirror/sync.go`:

```go
package mirror

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// Entry represents a source-target path pair for a mirrored directory.
type Entry struct {
	SourcePath string
	TargetPath string
}

// DiffResult classifies entries after comparing source scan to target state.
type DiffResult struct {
	New     []Entry // exist in source, no symlink in target
	Orphan  []Entry // symlink in target, not in source
	Valid   []Entry // symlink in target, exists in source
	Foreign []Entry // non-symlink entry in target
}

// Diff compares scanned source directories against existing entries in targetDir.
// Both sourceDir and targetDir should be at the prefix level.
func Diff(ctx context.Context, sourceDir, targetDir string, scanned []string, depth int) (*DiffResult, error) {
	sourceSet := make(map[string]string, len(scanned))
	for _, abs := range scanned {
		rel, err := filepath.Rel(sourceDir, abs)
		if err != nil {
			return nil, fmt.Errorf("computing relative path for %s: %w", abs, err)
		}
		sourceSet[rel] = abs
	}

	var result DiffResult

	// Check if target exists
	if _, err := os.Lstat(targetDir); os.IsNotExist(err) {
		for rel, sourcePath := range sourceSet {
			result.New = append(result.New, Entry{
				SourcePath: sourcePath,
				TargetPath: filepath.Join(targetDir, rel),
			})
		}
		return &result, nil
	}

	// Walk target at the same depth
	seen := make(map[string]bool)
	err := walkToDepth(ctx, targetDir, depth, func(path string, d fs.DirEntry) error {
		rel, err := filepath.Rel(targetDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}
		seen[rel] = true

		isSymlink := d.Type()&fs.ModeSymlink != 0
		if !isSymlink {
			result.Foreign = append(result.Foreign, Entry{TargetPath: path})
			return nil
		}

		if sourcePath, inSource := sourceSet[rel]; inSource {
			result.Valid = append(result.Valid, Entry{SourcePath: sourcePath, TargetPath: path})
		} else {
			result.Orphan = append(result.Orphan, Entry{TargetPath: path})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking target: %w", err)
	}

	// Source entries not in target are new
	for rel, sourcePath := range sourceSet {
		if seen[rel] {
			continue
		}
		result.New = append(result.New, Entry{
			SourcePath: sourcePath,
			TargetPath: filepath.Join(targetDir, rel),
		})
	}

	return &result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/mirror/ -v -run TestDiff
```

Expected: all Diff tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/mirror/sync.go internal/mirror/sync_test.go
git commit -m "Add Diff function to classify source/target entries"
```

---

### Task 5: Create and Prune

**Files:**
- Modify: `internal/mirror/sync.go`
- Modify: `internal/mirror/sync_test.go`

- [ ] **Step 1: Write failing tests for Create and Prune**

Add to `internal/mirror/sync_test.go`:

```go
func TestCreate_NewSymlink(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey"})

	entry := mirror.Entry{
		SourcePath: filepath.Join(sourceDir, "Robyn", "Honey"),
		TargetPath: filepath.Join(targetDir, "Robyn", "Honey"),
	}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Create(entry, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify symlink exists and points to correct source
	link, err := os.Readlink(entry.TargetPath)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if link != entry.SourcePath {
		t.Errorf("symlink points to %q, want %q", link, entry.SourcePath)
	}
}

func TestCreate_CreatesParentDirs(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"deep/nested/album"})

	entry := mirror.Entry{
		SourcePath: filepath.Join(sourceDir, "deep", "nested", "album"),
		TargetPath: filepath.Join(targetDir, "deep", "nested", "album"),
	}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Create(entry, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify parent dirs are real directories, not symlinks
	parentInfo, err := os.Lstat(filepath.Join(targetDir, "deep", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		t.Error("parent directory should be a real dir, not a symlink")
	}
}

func TestCreate_DryRun(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	entry := mirror.Entry{
		SourcePath: filepath.Join(sourceDir, "album"),
		TargetPath: filepath.Join(targetDir, "album"),
	}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Create(entry, true, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Symlink should NOT exist in dry-run mode
	if _, err := os.Lstat(entry.TargetPath); !os.IsNotExist(err) {
		t.Error("symlink should not be created in dry-run mode")
	}
}

func TestPrune_RemovesSymlink(t *testing.T) {
	targetDir := t.TempDir()

	// Create a symlink to prune
	symlinkPath := filepath.Join(targetDir, "orphan")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Prune(entry, targetDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
		t.Error("symlink should have been removed")
	}
}

func TestPrune_CleansEmptyParents(t *testing.T) {
	targetDir := t.TempDir()
	prefixDir := filepath.Join(targetDir, "Artists")

	// Create nested structure: Artists/Robyn/Album (symlink)
	mkdirs(t, targetDir, []string{"Artists/Robyn"})
	symlinkPath := filepath.Join(prefixDir, "Robyn", "Album")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Prune(entry, prefixDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Robyn/ should be removed (now empty)
	if _, err := os.Lstat(filepath.Join(prefixDir, "Robyn")); !os.IsNotExist(err) {
		t.Error("empty parent 'Robyn' should have been removed")
	}

	// Artists/ (prefix dir) should NOT be removed
	if _, err := os.Lstat(prefixDir); os.IsNotExist(err) {
		t.Error("prefix directory should not be removed")
	}
}

func TestPrune_StopsAtPrefixBoundary(t *testing.T) {
	targetDir := t.TempDir()
	prefixDir := filepath.Join(targetDir, "Artists")

	// depth 1: symlink directly in prefix dir
	mkdirs(t, targetDir, []string{"Artists"})
	symlinkPath := filepath.Join(prefixDir, "Album")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Prune(entry, prefixDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prefix dir should still exist
	if _, err := os.Lstat(prefixDir); os.IsNotExist(err) {
		t.Error("prefix directory should not be removed")
	}
}

func TestPrune_KeepsNonEmptyParents(t *testing.T) {
	targetDir := t.TempDir()
	prefixDir := filepath.Join(targetDir, "Artists")

	// Artists/Robyn has two symlinks, we prune one
	mkdirs(t, targetDir, []string{"Artists/Robyn"})
	os.Symlink("/src/a", filepath.Join(prefixDir, "Robyn", "Album1"))
	os.Symlink("/src/b", filepath.Join(prefixDir, "Robyn", "Album2"))

	entry := mirror.Entry{TargetPath: filepath.Join(prefixDir, "Robyn", "Album1")}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Prune(entry, prefixDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Robyn/ should still exist (Album2 is still there)
	if _, err := os.Lstat(filepath.Join(prefixDir, "Robyn")); os.IsNotExist(err) {
		t.Error("non-empty parent should not be removed")
	}
}

func TestPrune_DryRun(t *testing.T) {
	targetDir := t.TempDir()
	symlinkPath := filepath.Join(targetDir, "orphan")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(os.Discard, nil))
	err := mirror.Prune(entry, targetDir, true, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Symlink should still exist in dry-run mode
	if _, err := os.Lstat(symlinkPath); os.IsNotExist(err) {
		t.Error("symlink should not be removed in dry-run mode")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/mirror/ -v -run "TestCreate|TestPrune"
```

Expected: compilation errors (Create, Prune not defined yet)

- [ ] **Step 3: Implement Create and Prune**

Add to `internal/mirror/sync.go`:

```go
// Create creates a symlink at entry.TargetPath pointing to entry.SourcePath.
// Parent directories are created as needed.
func Create(entry Entry, dryRun bool, logger *slog.Logger) error {
	logger.Info("creating symlink",
		"target", entry.TargetPath,
		"source", entry.SourcePath,
		"dry_run", dryRun,
	)
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(entry.TargetPath), 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	return os.Symlink(entry.SourcePath, entry.TargetPath)
}

// Prune removes the symlink at entry.TargetPath and cleans up empty parent
// directories up to (but not including) prefixDir.
func Prune(entry Entry, prefixDir string, dryRun bool, logger *slog.Logger) error {
	logger.Info("pruning symlink",
		"target", entry.TargetPath,
		"dry_run", dryRun,
	)
	if dryRun {
		return nil
	}
	if err := os.Remove(entry.TargetPath); err != nil {
		return fmt.Errorf("removing symlink: %w", err)
	}
	cleanupEmptyParents(entry.TargetPath, prefixDir)
	return nil
}

// cleanupEmptyParents removes empty directories between the parent of path
// and prefixDir (exclusive). Stops at the first non-empty directory.
func cleanupEmptyParents(path, prefixDir string) {
	prefixDir = filepath.Clean(prefixDir)
	dir := filepath.Dir(path)
	for {
		dir = filepath.Clean(dir)
		if dir == prefixDir || len(dir) <= len(prefixDir) {
			break
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/mirror/ -v -run "TestCreate|TestPrune"
```

Expected: all Create and Prune tests pass

- [ ] **Step 5: Run all mirror tests**

```bash
go test ./internal/mirror/ -v
```

Expected: all tests pass (scan + diff + create + prune)

- [ ] **Step 6: Commit**

```bash
git add internal/mirror/sync.go internal/mirror/sync_test.go
git commit -m "Add Create and Prune with parent directory cleanup"
```

---

### Task 6: Mirror Orchestration

**Files:**
- Create: `internal/mirror/mirror.go`
- Create: `internal/mirror/mirror_test.go`

- [ ] **Step 1: Write failing tests for RunOnce**

Create `internal/mirror/mirror_test.go`:

```go
package mirror_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"shalott/internal/config"
	"shalott/internal/mirror"
)

func TestRunOnce_CreatesSymlinks(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey", "Robyn/Sexistential"})

	// Set mtime in the past to pass debounce
	past := time.Now().Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(sourceDir, "Robyn", "Honey"), past, past)
	os.Chtimes(filepath.Join(sourceDir, "Robyn", "Sexistential"), past, past)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:   "test",
			Source: sourceDir,
			Target: targetDir,
			Debounce: config.Duration{Duration: time.Minute},
			Paths: []config.Path{
				{Prefix: "Robyn", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(os.Discard, nil)),
	}

	result := m.RunOnce(context.Background())

	if result.Created != 2 {
		t.Errorf("expected 2 created, got %d", result.Created)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", result.Errors)
	}

	// Verify symlinks exist
	for _, album := range []string{"Honey", "Sexistential"} {
		link := filepath.Join(targetDir, "Robyn", album)
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("symlink %s not created: %v", link, err)
			continue
		}
		expected := filepath.Join(sourceDir, "Robyn", album)
		if target != expected {
			t.Errorf("symlink %s points to %q, want %q", link, target, expected)
		}
	}
}

func TestRunOnce_PrunesOrphans(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Create orphan symlink in target
	mkdirs(t, targetDir, []string{"prefix"})
	orphan := filepath.Join(targetDir, "prefix", "gone")
	os.Symlink("/nonexistent", orphan)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:   "test",
			Source: sourceDir,
			Target: targetDir,
			Paths: []config.Path{
				{Prefix: "prefix", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(os.Discard, nil)),
	}

	// Create the prefix dir in source so health check passes
	mkdirs(t, sourceDir, []string{"prefix"})

	result := m.RunOnce(context.Background())

	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}

	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Error("orphan symlink should have been removed")
	}
}

func TestRunOnce_SourceUnreachable(t *testing.T) {
	targetDir := t.TempDir()

	// Create existing symlink that should NOT be pruned
	mkdirs(t, targetDir, []string{"prefix"})
	existing := filepath.Join(targetDir, "prefix", "keep")
	os.Symlink("/whatever", existing)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:   "test",
			Source: "/nonexistent/source",
			Target: targetDir,
			Paths: []config.Path{
				{Prefix: "prefix", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(os.Discard, nil)),
	}

	result := m.RunOnce(context.Background())

	if result.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Errors)
	}

	// Existing symlink should be preserved (no pruning when source is down)
	if _, err := os.Lstat(existing); os.IsNotExist(err) {
		t.Error("existing symlink should not be pruned when source is unreachable")
	}
}

func TestRunOnce_DryRun(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"album"})

	past := time.Now().Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(sourceDir, "album"), past, past)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:   "test",
			Source: sourceDir,
			Target: targetDir,
			Debounce: config.Duration{Duration: time.Minute},
			Paths: []config.Path{
				{Prefix: "", Depth: 1},
			},
		},
		DryRun: true,
		Logger: slog.New(slog.NewTextHandler(os.Discard, nil)),
	}

	result := m.RunOnce(context.Background())

	if result.Created != 1 {
		t.Errorf("expected 1 created (dry run), got %d", result.Created)
	}

	// Symlink should NOT actually exist
	if _, err := os.Lstat(filepath.Join(targetDir, "album")); !os.IsNotExist(err) {
		t.Error("symlink should not be created in dry-run mode")
	}
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:         "test",
			Source:       sourceDir,
			Target:       targetDir,
			PollInterval: config.Duration{Duration: time.Hour},
			Paths: []config.Path{
				{Prefix: "", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(os.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx, nil)
		close(done)
	}()

	// Give Run time to start and do initial scan
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestRun_ForceScan(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:         "test",
			Source:       sourceDir,
			Target:       targetDir,
			PollInterval: config.Duration{Duration: time.Hour},
			Paths: []config.Path{
				{Prefix: "", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(os.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forceScan := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		m.Run(ctx, forceScan)
		close(done)
	}()

	// Wait for initial scan to complete
	time.Sleep(50 * time.Millisecond)

	// Add a new directory and force scan
	mkdirs(t, sourceDir, []string{"new-album"})
	forceScan <- struct{}{}

	// Wait for forced scan to complete
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Verify the new symlink was created
	if _, err := os.Lstat(filepath.Join(targetDir, "new-album")); os.IsNotExist(err) {
		t.Error("forced scan should have created the new symlink")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/mirror/ -v -run "TestRunOnce|TestRun_"
```

Expected: compilation errors (Mirror struct, RunOnce, Run not defined)

- [ ] **Step 3: Implement Mirror, RunOnce, and Run**

Create `internal/mirror/mirror.go`:

```go
package mirror

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"shalott/internal/config"
)

// ScanResult summarizes a single scan cycle.
type ScanResult struct {
	Created int
	Pruned  int
	Errors  int
}

// Mirror orchestrates scanning and syncing for a single mirror configuration.
type Mirror struct {
	Config config.Mirror
	DryRun bool
	Logger *slog.Logger
}

// RunOnce performs a single scan cycle across all path configs.
func (m *Mirror) RunOnce(ctx context.Context) ScanResult {
	var result ScanResult

	// Health check: verify source is reachable
	if _, err := os.Stat(m.Config.Source); err != nil {
		m.Logger.Error("source unreachable, skipping scan",
			"mirror", m.Config.Name,
			"source", m.Config.Source,
			"error", err,
		)
		result.Errors++
		return result
	}

	now := time.Now()

	for _, pathCfg := range m.Config.Paths {
		if ctx.Err() != nil {
			return result
		}

		sourceDir := filepath.Join(m.Config.Source, pathCfg.Prefix)
		targetDir := filepath.Join(m.Config.Target, pathCfg.Prefix)

		r := m.syncPath(ctx, sourceDir, targetDir, pathCfg, now)
		result.Created += r.Created
		result.Pruned += r.Pruned
		result.Errors += r.Errors
	}

	return result
}

func (m *Mirror) syncPath(ctx context.Context, sourceDir, targetDir string, pathCfg config.Path, now time.Time) ScanResult {
	var result ScanResult

	scanned, err := ScanSource(ctx, sourceDir, pathCfg.Depth, m.Config.Debounce.Duration, now)
	if err != nil {
		m.Logger.Error("scan failed, skipping path",
			"mirror", m.Config.Name,
			"prefix", pathCfg.Prefix,
			"error", err,
		)
		result.Errors++
		return result
	}

	m.Logger.Debug("scan complete",
		"mirror", m.Config.Name,
		"prefix", pathCfg.Prefix,
		"found", len(scanned),
	)

	diff, err := Diff(ctx, sourceDir, targetDir, scanned, pathCfg.Depth)
	if err != nil {
		m.Logger.Error("diff failed",
			"mirror", m.Config.Name,
			"prefix", pathCfg.Prefix,
			"error", err,
		)
		result.Errors++
		return result
	}

	for _, e := range diff.Foreign {
		m.Logger.Warn("foreign entry in target",
			"mirror", m.Config.Name,
			"path", e.TargetPath,
		)
	}

	prefixDir := filepath.Join(m.Config.Target, pathCfg.Prefix)

	for _, e := range diff.New {
		if err := Create(e, m.DryRun, m.Logger); err != nil {
			m.Logger.Error("failed to create symlink",
				"mirror", m.Config.Name,
				"target", e.TargetPath,
				"error", err,
			)
			result.Errors++
			continue
		}
		result.Created++
	}

	for _, e := range diff.Orphan {
		if err := Prune(e, prefixDir, m.DryRun, m.Logger); err != nil {
			m.Logger.Error("failed to prune symlink",
				"mirror", m.Config.Name,
				"target", e.TargetPath,
				"error", err,
			)
			result.Errors++
			continue
		}
		result.Pruned++
	}

	return result
}

// Run starts the daemon loop, scanning on the configured interval.
// Runs an initial scan immediately, then on each ticker tick.
// Returns when ctx is cancelled. Send on forceScan to trigger an immediate scan.
func (m *Mirror) Run(ctx context.Context, forceScan <-chan struct{}) {
	m.Logger.Info("starting mirror",
		"mirror", m.Config.Name,
		"poll_interval", m.Config.PollInterval.Duration,
	)

	result := m.RunOnce(ctx)
	m.Logger.Debug("initial scan complete",
		"mirror", m.Config.Name,
		"created", result.Created,
		"pruned", result.Pruned,
		"errors", result.Errors,
	)

	ticker := time.NewTicker(m.Config.PollInterval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Logger.Info("stopping mirror", "mirror", m.Config.Name)
			return
		case <-ticker.C:
			result := m.RunOnce(ctx)
			m.Logger.Debug("scan complete",
				"mirror", m.Config.Name,
				"created", result.Created,
				"pruned", result.Pruned,
				"errors", result.Errors,
			)
		case <-forceScan:
			m.Logger.Info("forced scan triggered", "mirror", m.Config.Name)
			result := m.RunOnce(ctx)
			m.Logger.Debug("forced scan complete",
				"mirror", m.Config.Name,
				"created", result.Created,
				"pruned", result.Pruned,
				"errors", result.Errors,
			)
			ticker.Reset(m.Config.PollInterval.Duration)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/mirror/ -v
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/mirror/mirror.go internal/mirror/mirror_test.go
git commit -m "Add Mirror orchestration with RunOnce and daemon loop"
```

---

### Task 7: CLI, Signals, and Main

**Files:**
- Modify: `cmd/shalott/main.go`

- [ ] **Step 1: Implement the full main.go**

Replace `cmd/shalott/main.go` with:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"shalott/internal/config"
	"shalott/internal/mirror"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "/etc/shalott/config.yaml", "path to config file")
	once := flag.Bool("once", false, "run a single scan and exit")
	dryRun := flag.Bool("dry-run", false, "log intended changes without executing")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("shalott", version)
		return 0
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		return 2
	}

	// Create target directories
	for _, m := range cfg.Mirrors {
		if err := os.MkdirAll(m.Target, 0o755); err != nil {
			logger.Error("failed to create target directory",
				"target", m.Target,
				"error", err,
			)
			return 2
		}
	}

	// Warn about unreachable sources
	for _, m := range cfg.Mirrors {
		if _, err := os.Stat(m.Source); err != nil {
			logger.Warn("source not reachable at startup",
				"mirror", m.Name,
				"source", m.Source,
				"error", err,
			)
		}
	}

	if *once {
		return runOnce(cfg, *dryRun, logger)
	}
	return runDaemon(cfg, *dryRun, logger)
}

func runOnce(cfg *config.Config, dryRun bool, logger *slog.Logger) int {
	ctx := context.Background()
	hasErrors := false

	for _, mcfg := range cfg.Mirrors {
		m := &mirror.Mirror{
			Config: mcfg,
			DryRun: dryRun,
			Logger: logger,
		}
		result := m.RunOnce(ctx)
		if result.Errors > 0 {
			hasErrors = true
		}
	}

	if hasErrors {
		return 1
	}
	return 0
}

func runDaemon(cfg *config.Config, dryRun bool, logger *slog.Logger) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling: SIGTERM/SIGINT for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Signal handling: SIGHUP for forced scan
	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)

	// Start mirror goroutines
	var wg sync.WaitGroup
	forceScanChs := make([]chan struct{}, len(cfg.Mirrors))

	for i, mcfg := range cfg.Mirrors {
		forceScanChs[i] = make(chan struct{}, 1)
		m := &mirror.Mirror{
			Config: mcfg,
			DryRun: dryRun,
			Logger: logger,
		}
		wg.Add(1)
		go func(forceScan chan struct{}) {
			defer wg.Done()
			m.Run(ctx, forceScan)
		}(forceScanChs[i])
	}

	logger.Info("shalott started", "mirrors", len(cfg.Mirrors))

	for {
		select {
		case sig := <-sigCh:
			logger.Info("received signal, shutting down", "signal", sig)
			cancel()
			wg.Wait()
			logger.Info("shalott stopped")
			return 0
		case <-sighupCh:
			logger.Info("received SIGHUP, forcing scan on all mirrors")
			for _, ch := range forceScanChs {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}
}
```

- [ ] **Step 2: Verify it builds**

```bash
go build -o shalott ./cmd/shalott
```

Expected: builds successfully

- [ ] **Step 3: Test --version flag**

```bash
./shalott --version
```

Expected: `shalott dev`

- [ ] **Step 4: Test with invalid config path**

```bash
./shalott --config /nonexistent ; echo "exit code: $?"
```

Expected: error message and `exit code: 2`

- [ ] **Step 5: Clean up and commit**

```bash
rm ./shalott
git add cmd/shalott/main.go
git commit -m "Add CLI with flags, signal handling, and daemon/once modes"
```

---

### Task 8: Integration Test and Deployment Files

**Files:**
- Create: `integration_test.go`
- Create: `deploy/shalott.service`

- [ ] **Step 1: Write integration test**

Create `integration_test.go`:

```go
//go:build integration

package main_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"shalott/internal/config"
	"shalott/internal/mirror"
)

func TestIntegration_FullCycle(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up source tree: Artists with two artists, each with albums
	dirs := []string{
		"Artists/Robyn/Honey",
		"Artists/Robyn/Sexistential",
		"Artists/Charlie Puth/Whatever's Clever",
		"Soundtracks/TRON Legacy",
		"Soundtracks/Interstellar",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(sourceDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Set all mtimes to past to pass debounce
	past := time.Now().Add(-10 * time.Minute)
	setMtimeRecursive(t, sourceDir, past)

	cfg := config.Mirror{
		Name:     "music",
		Source:   sourceDir,
		Target:   targetDir,
		Debounce: config.Duration{Duration: time.Minute},
		Paths: []config.Path{
			{Prefix: "Artists", Depth: 2},
			{Prefix: "Soundtracks", Depth: 1},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := &mirror.Mirror{Config: cfg, Logger: logger}

	// --- First scan: create all symlinks ---
	result := m.RunOnce(context.Background())
	if result.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", result.Errors)
	}
	if result.Created != 5 {
		t.Fatalf("expected 5 created, got %d", result.Created)
	}

	// Verify symlinks
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Robyn", "Honey"),
		filepath.Join(sourceDir, "Artists", "Robyn", "Honey"))
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Robyn", "Sexistential"),
		filepath.Join(sourceDir, "Artists", "Robyn", "Sexistential"))
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Charlie Puth", "Whatever's Clever"),
		filepath.Join(sourceDir, "Artists", "Charlie Puth", "Whatever's Clever"))
	assertSymlink(t, filepath.Join(targetDir, "Soundtracks", "TRON Legacy"),
		filepath.Join(sourceDir, "Soundtracks", "TRON Legacy"))
	assertSymlink(t, filepath.Join(targetDir, "Soundtracks", "Interstellar"),
		filepath.Join(sourceDir, "Soundtracks", "Interstellar"))

	// Verify intermediate dirs are real directories, not symlinks
	assertRealDir(t, filepath.Join(targetDir, "Artists"))
	assertRealDir(t, filepath.Join(targetDir, "Artists", "Robyn"))
	assertRealDir(t, filepath.Join(targetDir, "Soundtracks"))

	// --- Second scan: no changes ---
	result = m.RunOnce(context.Background())
	if result.Created != 0 || result.Pruned != 0 {
		t.Errorf("expected no changes, got created=%d pruned=%d", result.Created, result.Pruned)
	}

	// --- Remove an album from source, add a new one ---
	os.RemoveAll(filepath.Join(sourceDir, "Artists", "Robyn", "Honey"))
	os.MkdirAll(filepath.Join(sourceDir, "Artists", "Robyn", "Body Talk"), 0o755)
	os.Chtimes(filepath.Join(sourceDir, "Artists", "Robyn", "Body Talk"), past, past)

	// --- Third scan: should prune Honey, create Body Talk ---
	result = m.RunOnce(context.Background())
	if result.Created != 1 {
		t.Errorf("expected 1 created, got %d", result.Created)
	}
	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}

	// Verify Honey is gone, Body Talk exists
	if _, err := os.Lstat(filepath.Join(targetDir, "Artists", "Robyn", "Honey")); !os.IsNotExist(err) {
		t.Error("Honey symlink should have been pruned")
	}
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Robyn", "Body Talk"),
		filepath.Join(sourceDir, "Artists", "Robyn", "Body Talk"))

	// --- Remove all of an artist's albums ---
	os.RemoveAll(filepath.Join(sourceDir, "Artists", "Charlie Puth"))

	// --- Fourth scan: should prune and clean up empty Charlie Puth dir ---
	result = m.RunOnce(context.Background())
	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}
	if _, err := os.Lstat(filepath.Join(targetDir, "Artists", "Charlie Puth")); !os.IsNotExist(err) {
		t.Error("empty Charlie Puth directory should have been cleaned up")
	}
	// Artists dir should still exist (Robyn is still there)
	assertRealDir(t, filepath.Join(targetDir, "Artists"))
}

func assertSymlink(t *testing.T, linkPath, expectedTarget string) {
	t.Helper()
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Errorf("expected symlink at %s: %v", linkPath, err)
		return
	}
	if target != expectedTarget {
		t.Errorf("symlink %s points to %q, want %q", linkPath, target, expectedTarget)
	}
}

func assertRealDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Errorf("expected directory at %s: %v", path, err)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("%s should be a real directory, not a symlink", path)
	}
	if !info.IsDir() {
		t.Errorf("%s should be a directory", path)
	}
}

func setMtimeRecursive(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, mtime, mtime)
	})
}
```

- [ ] **Step 2: Run integration test**

```bash
go test -tags integration -v -run TestIntegration
```

Expected: all assertions pass

- [ ] **Step 3: Create systemd unit file**

Create `deploy/shalott.service`:

```ini
[Unit]
Description=shalott symlink mirror daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/shalott --config /etc/shalott/config.yaml
Restart=on-failure
RestartSec=10
User=root

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 4: Run all tests to verify nothing is broken**

```bash
go test ./... -v
go test -tags integration -v
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add integration_test.go deploy/shalott.service
git commit -m "Add integration test and systemd unit file"
```

- [ ] **Step 6: Final build verification**

```bash
go build -o shalott ./cmd/shalott && rm shalott
go vet ./...
```

Expected: clean build with no vet warnings

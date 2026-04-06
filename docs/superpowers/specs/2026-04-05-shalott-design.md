# shalott Design Spec

**A symlink mirror daemon for inotify-unaware filesystems.**

Named after Tennyson's *The Lady of Shalott*, who only sees the world through a mirror's reflection.

## Problem

Applications that watch directories for changes (Roon, Immich, Jellyfin, etc.) typically rely on Linux's `inotify` mechanism. `inotify` does not fire for changes originating on remote filesystems (NFS, CIFS, FUSE, etc.) — only for changes made locally. This means files added to a NAS and exported via NFS are invisible to inotify-dependent applications until a manual rescan or service restart.

## Solution

`shalott` maintains a local symlink tree that mirrors the directory structure of a remote source. When new directories appear on the remote filesystem (detected via polling/`stat()`), `shalott` creates corresponding symlinks on the local filesystem. The symlink creation is a local operation, which *does* generate inotify events, causing the watching application to discover the new content.

```
Remote FS (NFS)                Local FS (ext4/ZFS)
/mnt/music/                    /opt/roon/library/
├── Artists/                   ├── Artists/              (real dir)
│   ├── Robyn/                 │   ├── Robyn/            (real dir)
│   │   └── Sexistential/      │   │   └── Sexistential  -> /mnt/music/Artists/Robyn/Sexistential/
│   └── Charlie Puth/          │   └── Charlie Puth/     (real dir)
│       └── Whatever's Clever/ │       └── Whatever's Clever -> /mnt/music/Artists/Charlie Puth/Whatever's Clever/
└── Soundtracks/               └── Soundtracks/          (real dir)
    └── TRON Legacy/               └── TRON Legacy      -> /mnt/music/Soundtracks/TRON Legacy/
```

Parent directories in the target are real local directories. Only entries at the configured depth are symlinks pointing back to the source. Symlinks in the source tree are ignored — only real directories are mirrored.

## Configuration

YAML config file, default location `/etc/shalott/config.yaml`, overridable via `--config` CLI flag. Validated at startup using `go-playground/validator` for struct-level rules, plus custom validation for cross-mirror overlap checks.

```yaml
mirrors:
  - name: music
    source: /mnt/music
    target: /opt/roon/library
    poll_interval: 30s
    debounce: 2m
    paths:
      - prefix: Artists
        depth: 2
      - prefix: Classical
        depth: 2
      - prefix: Soundtracks
        depth: 1
      - prefix: Cast Recordings
        depth: 1

  - name: photos
    source: /mnt/photos
    target: /opt/immich/watch
    poll_interval: 5m
    debounce: 30s
    paths:
      - prefix: "2026"
        depth: 1
```

### Mirror fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes | — | Human-readable identifier, used in log output |
| `source` | string | yes | — | Absolute path to the remote/NFS mount |
| `target` | string | yes | — | Absolute path to the local symlink tree |
| `poll_interval` | duration | no | `30s` | How often to scan for changes |
| `debounce` | duration | no | `2m` | Ignore directories whose mtime is newer than this (still being copied) |
| `paths` | list | no | `[{prefix: "", depth: 1}]` | List of path configs; if omitted, mirrors direct children of source |

### Path fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `prefix` | string | yes | — | Subdirectory under `source` to mirror (e.g. `Artists`) |
| `depth` | int | yes | — | Depth at which to create symlinks. Must be a positive integer. `1` = direct children of prefix, `2` = grandchildren, etc. |

### Startup validation

**Fatal (refuse to start):**

- Config file missing or unparseable
- Mirror missing required fields (`name`, `source`, `target`)
- Overlapping sources or targets between mirrors
- Duplicate mirror names
- `depth` is zero or negative
- Path prefix is empty string when explicitly listed
- Target path doesn't exist and cannot be created

**Warning (log and continue):**

- Source path doesn't exist or isn't reachable (NFS may not be mounted at boot)

Shalott creates the target root directory (and any necessary parents) at startup if it doesn't already exist.

## Core Algorithm

Each mirror runs on its own goroutine with an independent ticker. All long-running operations receive a `context.Context` for clean cancellation.

### Per-tick scan

```
1. HEALTH CHECK: stat(mirror.source) — if unreachable, skip entire tick, log error.
   Never prune when source is unhealthy.

for each path config in mirror:
    source_dir = source/prefix
    target_dir = target/prefix

    2. SCAN: walk source_dir to exactly the configured depth, collecting directory paths.
       - Skip entries whose mtime is within the debounce window.
       - Skip symlinks in the source tree — only follow real directories.
       - Check context cancellation between entries.

    3. DIFF: compare discovered directories against existing symlinks in target_dir.
       - new:     directory exists in source but no symlink in target
       - orphan:  symlink exists in target but source directory no longer exists
       - valid:   symlink exists and source exists (no-op)
       - foreign: non-symlink entry exists at a target path — skip and log warning

    4. CREATE: for each new directory:
       - mkdir -p parent directories in target (real local dirs)
       - os.Symlink(source_path, target_path)
       - log at Info level
       - On failure: log error and continue with remaining entries (best-effort)

    5. PRUNE: for each orphaned symlink:
       - os.Remove(symlink)
       - Remove empty parent directories, recursing up but stopping at the prefix boundary
       - log at Info level
       - On failure: log error and continue with remaining entries (best-effort)
```

### Startup behavior

On startup, shalott runs a full scan immediately (before entering the ticker loop). This converges the target to match the current source state, handling any changes that occurred while shalott was not running. The symlink directory *is* the state — no database or marker files needed.

### Debounce

Directories with an mtime newer than `now - debounce` are skipped. This prevents creating a symlink to a half-copied album. The mtime naturally extends as new files are written, so large albums that take time to copy are handled correctly.

### Atomicity

Symlink creation (`os.Symlink`) is atomic on Linux. No risk of a watching application seeing a partially created link.

## Signals

| Signal | Behavior |
|---|---|
| `SIGTERM`, `SIGINT` | Cancel context, in-flight operations wind down promptly, then exit |
| `SIGHUP` | Force an immediate scan on all mirrors (skip waiting for next tick) |

## CLI

```
shalott                                             # run daemon with default config (/etc/shalott/config.yaml)
shalott --config /path/to/config.yaml               # run daemon with explicit config
shalott --config /path/to/config.yaml --once         # single scan, then exit
shalott --config /path/to/config.yaml --dry-run      # log what would happen, don't execute
shalott --version                                    # print version
```

`--once` and `--dry-run` can be combined: `--once --dry-run` shows what a single scan would do without making changes.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success (scan completed, no errors) |
| 1 | Scan completed but encountered errors (some symlinks failed, source unreachable) |
| 2 | Startup failure (bad config, validation errors) |

Exit codes 1 and 2 are most meaningful with `--once`; in daemon mode, shalott logs errors and keeps running.

## Logging

Uses `log/slog` with structured output to stdout only. Systemd/journald handles rotation, persistence, and querying.

| Level | Events |
|---|---|
| Info | Symlink created, symlink pruned, startup/shutdown, source health errors, config validation warnings |
| Debug | Each scan tick, scan duration, number of directories found, skipped entries (debounce, symlinks in source) |

In `--dry-run` mode, would-be creates and prunes are logged at Info level with a `dry_run=true` attribute.

## Deployment

**Binary:** `/usr/local/bin/shalott`

**Config:** `/etc/shalott/config.yaml`

**Systemd unit:**

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

Runs as root for NFS read access and local write access. Can run as a less-privileged user if that user has the necessary permissions on both source and target.

## Build

Standard Go module. Target `linux/amd64`.

```
go build -o shalott ./cmd/shalott
```

### Dependencies

- `gopkg.in/yaml.v3` — config parsing
- `github.com/go-playground/validator/v10` — struct validation
- Standard library for everything else (`log/slog`, `os`, `filepath`, `context`, `os/signal`)

## Future Considerations

- **Metrics:** Expose Prometheus metrics (symlinks created/pruned, scan duration, errors) if monitoring is added to the homelab.
- **Filesystem events on source:** If the source is ever on a local filesystem, optionally use `fsnotify` instead of polling.
- **Config reload on SIGHUP:** Currently SIGHUP forces an immediate scan; config reload could be added later.

# shalott

A symlink mirror daemon for inotify-unaware filesystems.

Named after Tennyson's [_The Lady of Shalott_](https://en.wikipedia.org/wiki/The_Lady_of_Shalott), who only sees the world through a mirror's reflection.

## Problem

Applications that watch directories for changes (Roon, Immich, Jellyfin, etc.) typically rely on Linux's `inotify` mechanism. `inotify` does not fire for changes originating on remote filesystems (NFS, CIFS, FUSE, etc.) -- only for changes made locally. Files added to a NAS and exported via NFS are invisible to inotify-dependent applications until a manual rescan or service restart.

## Solution

`shalott` maintains a local symlink tree that mirrors the directory structure of a remote source. When new directories appear on the remote filesystem (detected via polling), `shalott` creates corresponding symlinks on the local filesystem. The symlink creation is a local operation, which _does_ generate inotify events, causing the watching application to discover the new content.

```
Remote FS (NFS)                Local FS (ext4/ZFS)
/mnt/music/                    /opt/roon/library/
├── Artists/                   ├── Artists/              (real dir)
│   ├── Robyn/                 │   ├── Robyn/            (real dir)
│   │   └── Sexistential/      │   │   └── Sexistential  -> /mnt/music/Artists/Robyn/Sexistential/
│   └── Charlie Puth/          │   └── Charlie Puth/     (real dir)
│       └── Whatever's Clever/ │       └── Whatever's Clever -> ...
└── Soundtracks/               └── Soundtracks/          (real dir)
    └── TRON Legacy/               └── TRON Legacy      -> /mnt/music/Soundtracks/TRON Legacy/
```

Parent directories in the target are real local directories. Only the leaf entries are symlinks pointing back to the source.

## Installation

Download the binary for your platform from the
[releases](https://github.com/adamrothman/shalott/releases), check it,
and install it:

```sh
version=0.1.0
platform=linux_amd64   # or linux_arm64
file=shalott_${version}_${platform}
base=https://github.com/adamrothman/shalott/releases/download/v$version
curl -fsSLO "$base/$file" -O "$base/SHA256SUMS"
sha256sum --check --ignore-missing SHA256SUMS
gh attestation verify "$file" --repo adamrothman/shalott
install -m 0755 "$file" /usr/local/bin/shalott
```

`gh attestation verify` checks that this repository's release workflow
built the binary. Or install it with Go:

```
go install github.com/adamrothman/shalott@latest
```

Or build from source:

```
go build -o shalott .
```

## Usage

```
shalott [flags]
```

| Flag        | Short | Default                    | Description                            |
| ----------- | ----- | -------------------------- | -------------------------------------- |
| `--config`  | `-c`  | `/etc/shalott/config.yaml` | Path to config file                    |
| `--once`    |       |                            | Run a single scan and exit             |
| `--dry-run` | `-n`  |                            | Log intended changes without executing |
| `--debug`   |       |                            | Enable debug logging                   |
| `--version` | `-v`  |                            | Print version                          |

## Configuration

```yaml
mirrors:
  music:
    source: /mnt/music
    target: /opt/roon/library
    poll_interval: 30s
    debounce: 2m
    paths:
      - prefix: Artists
        depth: 2
      - prefix: Soundtracks
        depth: 1

  photos:
    source: /mnt/photos
    target: /opt/immich/watch
    poll_interval: 5m
    debounce: 30s
    paths:
      - prefix: "2026"
        depth: 1
```

### Mirror fields

| Field           | Type     | Required | Default        | Description                                                 |
| --------------- | -------- | -------- | -------------- | ----------------------------------------------------------- |
| _(key)_         | string   | yes      | --             | Mirror name, used in log output                             |
| `source`        | string   | yes      | --             | Absolute path to the remote/NFS mount                       |
| `target`        | string   | yes      | --             | Absolute path to the local symlink tree                     |
| `poll_interval` | duration | no       | `30s`          | How often to scan for changes                               |
| `debounce`      | duration | no       | `2m`           | Ignore directories newer than this (still being copied)     |
| `paths`         | list     | no       | `[{depth: 1}]` | Path configs; if omitted, mirrors direct children of source |

### Path fields

| Field    | Type   | Required | Default | Description                                                                    |
| -------- | ------ | -------- | ------- | ------------------------------------------------------------------------------ |
| `prefix` | string | no       | --      | Subdirectory under `source` to mirror (e.g. `Artists`)                         |
| `depth`  | int    | yes      | --      | Depth at which to create symlinks (`1` = direct children, `2` = grandchildren) |

### Depth

`depth` controls where symlinks are created relative to the prefix:

- `depth: 1` with `prefix: Soundtracks` symlinks each `Soundtracks/<album>/`
- `depth: 2` with `prefix: Artists` symlinks each `Artists/<artist>/<album>/`

## Signals

| Signal              | Behavior                                          |
| ------------------- | ------------------------------------------------- |
| `SIGTERM`, `SIGINT` | Graceful shutdown: finish current scan, then exit |
| `SIGHUP`            | Force an immediate scan on all mirrors            |

## How it works

Each mirror runs in its own goroutine with an independent poll timer.

**Per-tick scan:**

1. **Health check** -- verify the source is reachable via `stat()`. If unreachable, skip the entire tick. Existing symlinks are never pruned when the source is down, as this would cause the watching application to delete its library metadata.
2. **Scan** -- walk the source to the configured depth, collecting directories. Skip entries whose mtime is within the debounce window (still being written). The mtime naturally extends as new files are written, so large albums that take time to copy are handled correctly.
3. **Diff** -- compare discovered directories against existing symlinks in the target.
4. **Create** -- for each new directory, create parent dirs and a symlink.
5. **Prune** -- for each orphaned symlink, remove it and clean up empty parent directories.

On startup, a full scan runs immediately before entering the poll loop, converging the target to match the current source state. The symlink tree _is_ the state -- no database needed.

## Deployment

A systemd unit file is provided in [`deploy/shalott.service`](deploy/shalott.service).

```
sudo cp shalott /usr/local/bin/
sudo mkdir -p /etc/shalott
sudo cp config.yaml /etc/shalott/
sudo cp deploy/shalott.service /etc/systemd/system/
sudo systemctl enable --now shalott
```

## Testing

```
go test ./...                          # unit tests
go test -tags integration ./...        # include integration tests
```

## Future considerations

- **Metrics** -- expose Prometheus metrics (symlinks created/pruned, scan duration, errors) if monitoring is added.
- **Filesystem events** -- if the source is ever migrated to a local filesystem, shalott could use `fsnotify` instead of polling for lower latency.
- **Config reload** -- watch the config file for changes and reload on `SIGHUP` (currently `SIGHUP` just forces a scan).

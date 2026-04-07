# CLAUDE.md

## Project

shalott is a Go polling daemon that maintains local symlink trees mirroring remote directory sources, generating inotify events for watching applications like Roon, Immich, and Jellyfin.

## Build and test

```bash
go build .              # build binary
go test ./...           # unit tests
go test -tags integration ./...  # include integration tests
```

## Conventions

- Use `any` instead of `interface{}`
- `go-playground/validator` struct tags handle field validation; custom checks (`checkOverlaps`, `checkEmptyPrefixes`) cover cross-field rules
- Config uses `map[string]*Mirror` -- the map key is the mirror name
- `Mirror.Run` uses a `*Broadcast` for force-scan signaling (close-and-replace pattern)
- Tests use `t.Context()` instead of `context.Background()`
- Integration tests are gated behind `//go:build integration`

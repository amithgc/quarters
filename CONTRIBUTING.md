# Contributing to Quarters

Thanks for helping! Bug reports, ideas and pull requests are all welcome.

## Before you start

- For anything bigger than a small fix, open an issue first so we can agree
  on the approach.
- Security problems go through [SECURITY.md](SECURITY.md), not issues.

## Setting up

You need macOS, Go (see `go.mod` for the version), Node.js 20+ and the
[Wails](https://wails.io) CLI:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
make vendor   # npm ci + copy xterm.js and the font into frontend/dist/vendor
make dev      # run the app with live reload
```

Your normal Quarters settings live in `~/.config/quarters`. To try changes
against a throw-away setup, point `XDG_CONFIG_HOME` somewhere else:

```sh
XDG_CONFIG_HOME=/tmp/quarters-dev make dev
```

To test remote access without a tunnel, set the public URL to
`http://localhost:8421` (plain http is allowed for localhost only) and use
Chrome's DevTools → WebAuthn panel for a virtual passkey.

## Where things are

| | |
| --- | --- |
| `main.go` | window, menus, `--serve` |
| `app.go` | the `App` type: lifecycle, settings access, projects |
| `sessions.go`, `stats_view.go`, `settings.go`, `alerts.go`, `restore.go`, `gitfeatures.go`, `devlinks.go`, `schedule.go`, `projectinfo.go` | one file per feature, all methods on `App` (exported ones are callable from the frontend) |
| `remote_app.go` | what a browser may call, and per-device access |
| `internal/…` | packages with no Wails dependency (see the README) |
| `frontend/dist` | the frontend: plain ES modules and CSS, no bundler |
| `tools/mascot/build.mjs` | draws the mascot SVGs |

## Guidelines

- **Go:** `gofmt`, `go vet` and `staticcheck` must be clean (`make lint`).
  Exported identifiers get doc comments. Errors that reach the user are
  written for them ("folder not found: …").
- **Settings** are only changed through `App.editConfig`, which works on a
  copy, so code reading the settings never sees them change under it.
- **Remote access:** a new `App` method is *not* callable from a browser
  until it is added to `remoteMethods` in `remote_app.go`, and to
  `viewMethods` if watch-only devices may use it. Anything touching files
  must stay within the user's projects (see `insideProject`). Add a test.
- **Frontend:** escape anything put into HTML with `esc()`, or build it
  with DOM calls. No external requests: everything is served by the app.
- **Optional features** get an entry in `config.Features` (on by default
  unless it has side effects) and check `cfg.On("key")`.
- Keep commits focused, with messages that say why.

## Tests

```sh
make test   # go test -race ./...
```

Tests use temporary folders and set `XDG_CONFIG_HOME` and `HOME`, so they
never touch your settings or Claude Code's logs. Please add tests for new
behaviour, especially anything in `internal/remote` or `remote_app.go`.

## Pull requests

- `make lint` and `make test` pass.
- Describe what changed and how you tried it (on a phone too, if it touches
  remote access).
- Update the README (and Settings text) when behaviour changes.

By contributing you agree that your contribution is licensed under the
[MIT License](LICENSE).

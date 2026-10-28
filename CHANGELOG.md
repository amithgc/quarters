# Changelog

## Unreleased

- The public URL in Remote Access is optional: without one, the server runs
  at `http://localhost:8421`, for this Mac or an SSH tunnel.
- Opening `http://127.0.0.1:<port>` without a public URL goes to
  `http://localhost:<port>`, where passkeys work (it used to fail with
  "cross-origin request blocked").
- The title bar shows the date and a 12-hour clock; the remote pill is always
  there (greyed out while the server is stopped) and its dot pulses while
  it runs.
- Dialogs open in the middle of the window (Settings used to sit at the
  bottom).

## 0.3.0 — first public release

- Renamed from "harness" to **Quarters**. Settings move from
  `~/.config/harness` to `~/.config/quarters` automatically on first start
  (the old folder is left as it was).
- A new, original mascot: a little monitor-headed robot, animated SVG.
- **Settings** (⌘,): every optional feature can be switched on or off.
- Phone alerts with the app closed (Web Push), keep the Mac awake, saved
  prompts, resume sessions after a restart, per-project permission mode,
  usage-limit warnings, conversation search, git worktrees, git status in the
  project list, "what Claude changed" with diffs, scheduled automations,
  dev server links, skills and MCP servers, a global hotkey.
- Remote access: start and stop the server from the app (⌥⌘R), per-device
  access (full / type only / watch only), per-address lockout.

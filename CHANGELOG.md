# Changelog

## Unreleased

- On phones and tablets, Claude Code's menus (trust this folder, allow a
  tool…) are answered by tapping: tap an option to highlight it, tap it
  again to choose it. No arrow keys needed.

- Phones show the terminal at 15px (was 11px), closer to a chat app, and
  pinching a terminal with two fingers changes the text size.

- Each window works on its own. A Claude tab in a window you're not using
  pauses ("In use on another device") instead of following the other
  window's size and keystrokes; clicking or typing there takes it back,
  and Claude reprints the whole conversation at that window's size.
  Shell and automation tabs still mirror live, at the size of the window
  in use.

- The phone's key bar (esc, tab, ⇧tab, ctrl, arrows) shows only while you
  type in a terminal, and can be switched off in Settings (Phone key bar).
- Claude Code's ⏺, ✔, ⏸, ✳, ⚡… no longer turn into wide colour emoji on
  phones: a small bundled font (tools/symbols) draws them one cell wide.

- The welcome screen lists the projects with open sessions (working or
  waiting for you); tap one to open it.
- A session taken over by a window of another width (phone ↔ Mac) is
  redrawn cleanly instead of leaving copies of Claude's input box behind.
  The phone keyboard no longer shows the AutoFill bar over the terminal.

- **Password sign-in:** a username and password, set on the Mac, for
  browsers that can't use a passkey. Stored as an argon2id hash, with the
  same lockouts as passkeys. `Quarters --set-password <name>` sets it
  without a window.
- **Home network:** Remote Access can also open Quarters at the Mac's
  address on your Wi-Fi (`http://192.168.x.x:8421`), with a QR code, for
  private addresses only.

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

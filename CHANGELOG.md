# Changelog

## Unreleased

- On Android over the home network's plain HTTP, the sign-in page shows how
  to let Chrome install the app (it installs only from secure addresses;
  Chrome's per-address setting is enough, no HTTPS needed).

- **Linux:** the remote-access server alone (no window) builds for Linux
  (`make server`, amd64 and arm64, no dependencies) and runs from the
  command line: `--set-password`, `--pair`, `--devices`, `--remove-device`.
  It reads Claude Code's login from `~/.claude/.credentials.json`, keeps
  the machine awake with `systemd-inhibit` and opens links with `xdg-open`;
  home-network addresses include Linux port names (eth0, enp3s0, wlan0…).
  CI builds and tests it on Linux.
- Without `$SHELL` (a service, a container), shells use the first of zsh,
  bash and sh the machine has.
- **Keyboard shortcuts** dialog on the Mac: Help → Keyboard Shortcuts (⌘/),
  or "⌘/ shortcuts" in the bottom bar.

- The **Claude Remote Control** switch is about the Claude session in view:
  on when Claude Code has it connected (read from Claude Code's own list,
  whoever turned it on), and flipping it runs `/remote-control` in that
  session. "Start new Claude tabs with it" is a checkbox below. The
  project-list mark follows the same live state.

- **End this Claude session** in the side panel (⋯ on a phone), for the tab
  you're looking at; it asks first.

- iPhone home-screen app: the status bar is opaque (`black`), so the page
  reaches the bottom of the screen; with the translucent bar iOS cut it 62
  points short (re-add the app to the home screen to pick this up). The
  page follows the visual viewport, so the message box stays above the
  keyboard.
- The phone chat follows the log Claude Code actually writes, and no longer
  mistakes ❯ lines from earlier prompts for a menu.
- Project list: a little room in git counts (● 6, ↑ 3), and a broadcast
  icon on projects with a session under Claude Remote Control.

- **Chat view on phones:** a Claude tab reads like the Claude app. The
  conversation comes from Claude Code's own log: your messages as bubbles,
  Claude's replies formatted, tool use folded into rows such as "Ran a
  command, edited a file ›" (tap for commands, results and diffs), and
  "✻ Working…" while it works. Claude's menus and questions become
  buttons, the send button stops Claude while it works, and ▤ switches to
  the terminal.

- Phones look and feel like the Claude app: a round ☰, a centred title
  with what the tab is doing, a status pill with ⋯ for Quarters' extras
  (plan, session, tokens, git, servers, MCP, skills), tabs as pills, and a
  message card with the tools (new tab, shell, automations, prompts,
  conversations, worktrees) and a round send button.
- An **ACTIVE** section at the top of the project list shows the projects
  with open sessions, working ones first.

- Phones type into a real text field (autocorrect, dictation, selection,
  a backspace that repeats) and send with ↑ or return; backspace in an
  empty field deletes in Claude's input. Tapping the terminal opens it.
- Swipe a terminal to scroll its history on touch screens, with momentum.
- **Claude Remote Control** toggle in the side panel: new Claude tabs start
  with `claude --remote-control <project>`, so they can also be continued
  from claude.ai/code and the Claude app.

- Phone-sized interface on phones: 16px text, a bigger title bar (19px
  project name), taller tabs with 44pt buttons, roomier project rows in the
  drawer, and the working/waiting counts as just ● and ◐ with a number.

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

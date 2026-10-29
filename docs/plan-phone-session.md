# Plan: one session view for every coding agent on phones

Status: phases 1 and 2 built (2026-10-29): `frontend/dist/layout.js`
decides, `applySessionView` in `app.js` applies it, the adapter fields are
`Interrupt`, `MenuMarker` and `Reader` in `internal/config/harness.go`.
Decisions taken: the card is below the terminal for every agent (Claude
included), chat where a reader exists, tablets unchanged. Agents without a
known menu marker always get the key bar. Phase 3 (readers for other
agents) is not built: none of those agents is installed here to check
their log formats against.

## 1. What's wrong today

On a phone, a Claude Code tab and a Codex/Antigravity/… tab are two different
products:

| | Claude Code tab | Any other agent's tab |
| --- | --- | --- |
| Transcript | Chat view (read from Claude's log), or the terminal | The terminal only |
| Typing | Quarters' message card (textarea + ↑ + tools), laid over Claude's own input box | Straight into xterm's hidden textarea, like a shell |
| Keys | Card; key strip only while typing in terminal view | Key strip while typing |
| Stop | ■ in the card sends Esc | Nothing; ^C on the key strip |
| Menus | Tap an option (❯ detection), or buttons in the chat | Arrow keys on the strip |
| Tools (new tab, shell, ▶, ⚡, 🕘, ⎇) | In the card | In the tab bar |

The reasons are historical: the card, the chat view and the overlay were
built for Claude Code alone, and other agents were bolted on as "a shell
with a different name" (`body.term-tab`). The logic is spread across
`isClaude`, `mirrors`, `term-tab`, `compose-over`, `chatOn`, `placeTools`,
`layComposer`, `claudeBoxTop`, `menuKeysAt`, `chatMenu` and three media
queries in `style.css`, each deciding part of the layout on its own.

What the three agents' screens look like at 44×30 (captured in the test
server): Claude frames its input with `────` rules; OpenCode is a
full-screen TUI with its input mid-screen under a `▀▀▀` bar, modals over
it and a status line at the bottom; Antigravity wasn't captured (see §8).
Any design that needs to know where an agent's input box is will break
per agent.

## 2. Design

One **session view** for every agent tab on a phone, built from the same
four parts, in the same places, with the same behaviour. Shell and
automation tabs stay plain terminals (as asked earlier).

```
┌──────────────────────────────┐
│ ‹ shop · codex · working     │  header (exists; unchanged)
├──────────────────────────────┤
│                              │
│   transcript                 │  the terminal (every agent), or a chat
│   (terminal | chat)          │  rendering where Quarters can read the
│                              │  agent's log (Claude today; §4)
│                              │
├──────────────────────────────┤
│ esc tab ⇧tab ctrl ^C ↑ ↓ ⚡  │  key strip: only while the card's
├──────────────────────────────┤  textarea has focus (keyboard is up)
│ ┌──────────────────────────┐ │
│ │ Message Codex…           │ │  message card: the one place to type,
│ │ ＋ ›_ ▶ 🕘 ⎇ ▤        ↑ │ │  identical for every agent
│ └──────────────────────────┘ │
└──────────────────────────────┘
```

Rules:

1. **One layout decision.** A single function `layoutFor(tab)` returns
   `{ transcript: "chat" | "terminal", card: bool, strip: bool,
   toolsIn: "card" | "tabs" }` from the tab kind, the agent, the screen
   class and the user's view choice. Everything else reads that: no more
   per-feature `isClaude`/`term-tab` checks in the layout code.
2. **Screen classes:** `phone` (≤ 760 px), `tablet` (touch, > 760 px),
   `desktop`. Phone: card + strip + transcript. Tablet and desktop: as
   today (terminal, type directly; strip on tablets while typing).
3. **The card is the same for every agent.** Placeholder "Message
   Codex…"; ↑ sends the text and Enter; ■ while the agent is busy sends
   the agent's interrupt key; Backspace on an empty card deletes in the
   agent's own input; the tools row is identical. Multi-line text is
   pasted as one block (works for every TUI that accepts bracketed paste;
   verified per agent in §7).
4. **The card never depends on the agent's screen layout.** It sits below
   the terminal, and the terminal is fitted to the space above it (no
   overlay, no padding arithmetic, no box detection). The agent's own
   input box stays visible at the bottom of its terminal, showing exactly
   what the agent has received; the card is the keyboard. This replaces
   `layComposer`/`claudeBoxTop` for all agents, including Claude.
   *If you'd rather keep Claude's overlay (card over its box), it stays as
   a Claude-only adapter hook (§3) and the generic rule applies to the
   others. Decision 1 in §9.*
5. **Transcript default:** chat where a reader exists (Claude today),
   terminal otherwise. ▤/💬 toggles and is shown only where a reader
   exists. The choice is remembered per agent, not globally.
6. **Menus:** the strip always has ↑ ↓ ↵ so any agent's menu works. On
   top of that, tapping an option works where the agent's marker is known
   (Claude's `❯` today; others added to the adapter when verified).
7. **Status and alerts** stay output-based (busy = output in the last 3 s;
   "your turn" after a stretch of work) so they work for every agent
   without reading logs.

## 3. The agent adapter

Everything agent-specific moves into one table, in Go (`config.Harness`,
sent to the frontend in `Settings`) so the frontend has no agent names in
it:

| field | Claude Code | Codex | Gemini CLI | Antigravity | OpenCode | Copilot CLI | Devin | Aider |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `bin`, `yolo`, `continue` | exist today | | | | | | | |
| `interrupt` (■) | `Esc` | `Esc`? | `Esc`? | `Esc`? | `Esc`? | `Esc`? | `Esc`? | `^C` |
| `menuMarker` (tap-to-choose) | `❯` | ? | ? | ? | none (modal buttons) | ? | ? | none |
| `reader` (chat transcript) | `claude` (exists) | later | later | — | later | — | — | later |
| `newline` (Shift+Enter in the card) | `^J` | ? | ? | ? | ? | ? | ? | ? |

"?" = default to `Esc` / no marker / `^J`, then verify on the real agent
and correct. Nothing in the UI breaks when a field is wrong: the strip's
keys are always there.

## 4. Chat transcripts for other agents (optional, later)

The chat view is the nicest part of the phone experience and today only
Claude has it. The backend already has the shape for more: `Chat(id)` →
`[]ChatItem{user | text | tools}`. Add a `Reader` interface
(`Find(tab) path`, `Version(path)`, `Read(path, max)`) and one
implementation per agent whose log format is stable and local:

| agent | where the log lives (to verify before building) |
| --- | --- |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, matched to the folder by its `cwd` |
| Gemini CLI | `~/.gemini/tmp/<project-hash>/chats/session-*.json` |
| Aider | `<project>/.aider.chat.history.md` |
| OpenCode | `~/.local/share/opencode/storage/` (session + message JSON) |
| Antigravity | `~/.gemini/antigravity-cli/brain/<id>/…` — IDE-shaped, likely not worth it |
| Copilot CLI, Devin | not known; skip |

Each reader is ~150 lines of Go + a test with a real log sample. Without
a reader an agent simply has no 💬 toggle; the rest is identical.

## 5. Changes by file

**Go**
- `internal/config/harness.go`: add `Interrupt`, `MenuMarker`, `Newline`,
  `Reader` to `Harness`; keep the table as the one place agents are
  described.
- `settings.go`: already sends `harnesses`; nothing else.
- `chat.go` / `internal/stats/chat.go`: behind a `Reader` interface;
  Claude's reader is the existing code. (Phase 3 adds readers.)

**Frontend `app.js`** (the bulk of the work; ~400 lines touched, net smaller)
- New `layoutFor(tab)` and one `applyLayout()` that sets body classes
  `view-chat | view-term`, `has-card`, `has-strip`, moves the tools, fits
  the terminal. Called from `showActive`, resize, focus changes and the
  ▤/💬 toggle.
- Delete `layComposer`, `claudeBoxTop`'s layout use (keep it as Claude's
  `menuMarker` helper), `placeTools`'s media-query logic, the `term-tab`
  and `compose-over` classes and their special cases.
- `wireCompose`: agent-neutral send/stop/backspace/newline using the
  adapter; the card's placeholder from `agentName`.
- `wireMenuTaps` and `chatMenu`: take the marker from the adapter.
- `renderChat`: unchanged apart from reading `layoutFor`.
- `wireKeybar`: unchanged.

**Frontend `style.css`**
- Replace the phone/touch rules spread over `compose-over`, `term-tab`,
  the tablet block and the chat block with one block keyed on
  `has-card`/`has-strip`/`view-*` and the screen class. Expect ~60 lines
  removed, ~30 added.
- The card, strip and chat styles themselves are kept (they're what the
  user approved as "like the Claude app").

**Docs**: the phones and tablets sections in README and the site.

## 6. Phases

| phase | what | size |
| --- | --- | --- |
| 0 | A screen-capture harness (done in this session's scratchpad: starts an agent in the test server at 44×30 and prints the screen) kept under `tools/` so adapters can be verified per agent | small |
| 1 | `layoutFor`/`applyLayout`; card below the terminal for every agent; adapter fields with defaults; delete the overlay code | ~1 day |
| 2 | Verify and fill the adapter per installed agent (Claude, Antigravity, OpenCode here; Codex and Gemini if installed for testing): interrupt key, newline, menu marker, bracketed paste | ~½ day + per agent |
| 3 | Chat readers: Codex, then Gemini, Aider, OpenCode, each only after checking its log format on a real session | ~½ day each |
| 4 | Docs and screenshots (a Codex or OpenCode tab on the phone for the site) | small |

Phase 1 alone fixes the complaint: every agent gets the same card, strip
and tools. Phases 2–4 are polish and can be cut.

## 7. Testing

- Puppeteer, iPhone 13 and iPad emulation, against the test server with
  the real agents on PATH: for each agent, start a tab, check the card and
  strip are shown, send a line and see it echoed in the terminal, press ■
  while busy, Backspace on the empty card, the ▤/💬 toggle where a reader
  exists, rotate (resize) and keyboard up/down (visual viewport) without
  the card leaving the screen.
- The same for a shell tab: no card, strip while typing (unchanged).
- Unit tests for `layoutFor` (pure function) over tab kind × agent × screen
  class × view choice.
- Go tests for each reader with a sample log.
- Your real phone, before and after, for the two things emulation can't
  show: keyboard animation and the home-indicator gap.

## 8. Risks and unknowns

- **Bracketed paste**: multi-line sends rely on the TUI accepting a paste
  as one block. Claude does. Others verified in phase 2; the fallback is
  sending lines with `^J` between them (the adapter's `newline`).
- **Interrupt keys** differ and a wrong one can quit an agent (`^C` twice
  quits several). Default is `Esc` (harmless everywhere seen); `^C` only
  where verified.
- **Menu markers** are heuristics; the strip's ↑ ↓ ↵ always works.
- **Agents not installed here** (Codex, Gemini, Copilot, Devin) can only
  be verified if installed; the plan treats them as "defaults, unverified"
  until then.
- **Two `agy` binaries** on this Mac: `/opt/homebrew/bin/agy` is a broken
  wrapper (points at a missing `Antigravity.app`); `~/.local/bin/agy` is
  the real one. The Mac app uses your shell's PATH order and finds the
  right one; the test server didn't. Worth deleting the broken one.

## 9. Decisions needed

1. **Card placement for Claude:** below the terminal like every other
   agent (one rule, Claude's own box visible above the card), or keep
   Claude's overlay (card over its box) as a special case? Recommended:
   below, for one rule everywhere; the chat view hides the terminal
   anyway, so this only shows in ▤ terminal view.
2. **Transcript default on phones:** chat where available, terminal
   otherwise (recommended), or terminal for everyone?
3. **Tablets:** keep typing directly into the terminal (as asked earlier),
   or give tablets the same card as phones? Recommended: keep.
4. **Phase 3 readers:** which agents, if any, and in what order.
   Recommended: Codex first (most used after Claude), then stop and see.
5. **Install Codex and Gemini CLI on this Mac** for phase 2 verification?
   Both are `npm install -g` and need a sign-in you'd do yourself.

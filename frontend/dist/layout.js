// How a tab is shown and typed into: one decision for every coding agent.
//
// On a phone an agent's tab (Claude Code, Codex, Gemini CLI, …) is a
// session view: its transcript (the agent's own terminal, or a chat where
// Quarters can read the agent's log), the key bar while typing, and the
// message card, the one place to type, the same for every agent. The card
// sits below the terminal, which is fitted to the space above it, so
// nothing depends on where an agent draws its own input box.
//
// Shells and automations stay plain terminals you type into, and so do
// tablets and the Mac.

// screenClass is "phone" (narrow), "tablet" (touch, wider) or "desktop".
export function screenClass({ narrow, touch }) {
  return narrow ? "phone" : touch ? "tablet" : "desktop";
}

// layoutFor decides, for the tab in view:
//   transcript  "chat" or "terminal"
//   card        whether the message card is the place to type
//   toolsIn     where the tab tools go: "card" or "tabs" (the tab bar)
//
// tab: null (no tab), or { agent: bool, reader: bool }
//   agent   a coding agent's tab (not a shell or automation)
//   reader  Quarters can read the agent's conversation (a chat view)
// screen: screenClass()
// view: the transcript chosen on this device for the agent, "chat" or
//   "terminal" ("" = the default: chat where there is one)
export function layoutFor(tab, screen, view = "") {
  const plain = { transcript: "terminal", card: false, toolsIn: "tabs" };
  if (!tab || !tab.agent || screen !== "phone") return plain;
  const chat = tab.reader && view !== "terminal";
  return { transcript: chat ? "chat" : "terminal", card: true, toolsIn: "card" };
}

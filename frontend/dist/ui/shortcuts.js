// The keyboard shortcuts dialog (Help → Keyboard Shortcuts, ⌘/). mac: only
// in the Mac app.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  APPLE,
  REMOTE,
  esc,
  keyText,
  state,
} from "../app.js";

function shortcutGroups() {
  const hotkey = (state.settings?.hotkey || "ctrl+alt+space").split("+").map((k) => ({ ctrl: "⌃", alt: "⌥", option: "⌥", cmd: "⌘", shift: "⇧", space: "Space" })[k] || k.toUpperCase()).join("");
  return [
    ["Projects", [
      ["⇧ ⇧  or  ⌘P", "Jump to a project or automation"],
      ["⇧⌘P", "Every command, by name (or type > in the jump list)"],
      ["C", "Start Claude in the open project"],
      ["⇧C", "Continue the last conversation"],
      ["S", "Open a shell"],
      ["⌘O", "Add a project"],
      ["⇧⌘R", "Show the project in Finder", "mac"],
    ]],
    ["Tabs", [
      ["⌘T", "New Claude tab"],
      ["⇧⌘T", "Continue the last conversation in a new tab"],
      ["⌥⌘T", "New shell tab"],
      ["⌘W", "Close the tab (in a split: the pane)"],
      ...(REMOTE && APPLE ? [["⌃⌘T  ⌃⌘W", "In a browser, where Chrome keeps ⌘T, ⌘W and ⇧⌘[ ] for its own tabs: add ⌃"]] : []),
      ["⌘1 … ⌘9", "Go to tab 1–9"],
      ["⇧⌘[  ⇧⌘]", "Previous / next tab"],
      ["⌘J", "Go to the session waiting for you longest"],
      ["Double-click", "Rename a tab"],
    ]],
    ["Split panes", [
      ["⌘D", "Split: a shell beside this one"],
      ["⇧⌘D", "Split: a shell below this one"],
      ["⌘[  ⌘]", "Previous / next pane"],
      ["Drag a line", "Resize (double-click: make them equal)"],
    ]],
    ["In a terminal", [
      ["⇧↵", "New line in Claude's prompt (without sending)"],
      ["⌘K", "Clear the scrollback"],
      ["⌘=  ⌘-  ⌘0", "Bigger / smaller / normal text"],
      ["Drop files", "Type their paths"],
    ]],
    ["Panels and tools", [
      ["⌘B", "Show or hide the projects panel"],
      ["⌘I", "Show or hide the side panel"],
      ["⇧⌘E", "Show or hide the files panel (click a file to edit it)"],
      ["⌘S", "Save the file in view"],
      ["⇧⌘↵", "Focus mode: only the terminal (again to come back)"],
      ["⇧⌘F", "Search past conversations"],
      ["⇧⌘G", "Worktrees"],
      ["⇧⌘U", "Usage by project"],
      ["⌘,", "Settings"],
      ["⌥⌘,", "Edit config.json"],
      ["⌥⌘R", "Start or stop the remote-access server", "mac"],
      [hotkey, "From any app: bring Quarters forward or hide it", "mac"],
      ["⌘/", "This list"],
    ]],
  ].map(([title, rows]) => [title, rows.filter(([, , mac]) => !mac || !REMOTE)]); // a browser can't reach the Mac's Finder
}

function openShortcuts() {
  $("#keys-list").innerHTML = shortcutGroups()
    .map(([title, rows]) => `<div class="keys-group"><div class="field-label">${esc(title)}</div>${rows.map(([k, what]) => `<div class="keys-row"><span class="keys-k">${k.split(/\s{2,}/).map((part) => (part === "or" ? '<span class="muted">or</span>' : `<kbd>${esc(keyText(part))}</kbd>`)).join(" ")}</span><span class="keys-what">${esc(what)}</span></div>`).join("")}</div>`)
    .join("");
  $("#keys-dialog").classList.remove("hidden");
  $("#keys-close").focus();
}

function closeShortcuts() {
  $("#keys-dialog").classList.add("hidden");
}

export { openShortcuts, closeShortcuts };

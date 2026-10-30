// Tips: one quiet line at the bottom right, always, about a key or a panel.
// The ones you haven't used come first; every two minutes the next one
// fades in, and › shows it now. Loaded by startTips() in app.js.

import { $, REMOTE, activeTab, isNarrow, keyText, saveTips, state, tipsDue, tipsState } from "../app.js";

const SHOW_MS = 2 * 60e3; // how long one stays
const CHECK_MS = 15e3;

// done: the actions that mean you know it already (see learned() in app.js)
const TIPS = [
  { id: "panels", text: "<b>⌘B</b> <b>⇧⌘E</b> <b>⌘I</b> hide and show the projects, files and stats panels, or use the buttons at the top right", done: ["toggle-left", "toggle-files", "toggle-right"], spot: "#btn-left, #btn-files, #btn-right" },
  { id: "open-file", text: "Click a file in the files panel to edit it in a tab · <b>⌘S</b> saves", done: ["open-file"], when: () => state.showFiles, spot: "#file-tree" },
  { id: "split", text: "<b>⌘D</b> splits the terminal to the right, <b>⇧⌘D</b> below · <b>⌘[</b> <b>⌘]</b> move between the panes", done: ["split-right", "split-down"], when: () => !!activeTab() },
  { id: "palette", text: "<b>⇧⇧</b> or <b>⌘P</b> jumps to a project: type a few letters of its name", done: ["palette"] },
  { id: "continue", text: "<b>⇧⌘T</b> picks up the last conversation in this project", done: ["continue"] },
  { id: "waiting", text: "<b>⌘J</b> jumps to the session that's waiting for you", done: ["next-waiting"] },
  { id: "find-file", text: "The box at the top of the files panel finds any file in the project", when: () => state.showFiles, spot: "#files-filter" },
  { id: "history", text: "<b>⇧⌘F</b> searches all your past conversations", done: ["history"] },
  { id: "resize", text: "Drag the gap between two panels to make one wider", spot: ".handle" },
  { id: "fold", text: "Click a heading in the stats panel to fold that section away", when: () => state.showRight },
  { id: "focus", text: "<b>⇧⌘↵</b> focus mode: only the terminal · the same keys bring the panels back", done: ["focus-mode"] },
  { id: "commands", text: "Type <b>&gt;</b> in the <b>⌘P</b> box to run any command by name" },
  { id: "font", text: "<b>⌘=</b> and <b>⌘-</b> change the text size · <b>⌘0</b> resets it", done: ["font-up", "font-down"] },
  { id: "worktrees", text: "<b>⇧⌘G</b> worktrees: give an agent its own copy of the project to work in", done: ["worktrees"] },
  { id: "remote", text: "Projects → Remote Access… lets you check on your agents from your phone", done: ["remote"], when: () => !REMOTE },
  { id: "shortcuts", text: "<b>⌘/</b> lists every shortcut", done: ["shortcuts"] },
];

let tip = null; // the one showing
let nextAt = 0;
let hideAt = 0;
let hovered = false;
let ticking = null;

// pick: a tip not seen yet whose keys you haven't used; when every one has
// been seen, round again after the one showing (still the unused ones,
// while there are a couple).
function pick() {
  const t = tipsState();
  const used = new Set(t.used || []), seen = new Set(t.seen || []);
  const fits = TIPS.filter((x) => !x.when || x.when());
  const unused = fits.filter((x) => !(x.done || []).some((d) => used.has(d)));
  const fresh = unused.find((x) => !seen.has(x.id));
  if (fresh) return fresh;
  const pool = unused.length > 1 ? unused : fits;
  return pool[(pool.indexOf(tip) + 1) % pool.length];
}

// quiet moments only: the window in use, nothing open over it, room for a line
function canShow() {
  return document.visibilityState === "visible" && document.hasFocus() && !isNarrow() && !state.palette.open && !document.querySelector(".overlay:not(.hidden)") && !document.body.classList.contains("focus-mode");
}

function tick() {
  if (Date.now() < nextAt || !tipsDue() || !canShow()) return;
  if (!tip || (!hovered && Date.now() > hideAt)) next();
}

// next fades the one showing out and the next one in
function next() {
  const x = pick();
  if (!x) return;
  const el = $("#tip");
  spot(false);
  if (!tip) return show(x);
  el.classList.remove("on");
  setTimeout(() => show(x), 600);
}

function show(x) {
  const t = tipsState();
  if (!(t.seen || []).includes(x.id)) saveTips({ ...t, seen: [...(t.seen || []), x.id] });
  tip = x;
  hideAt = Date.now() + SHOW_MS;
  const el = $("#tip");
  el.innerHTML = `<span class="tip-ic">✦</span><span class="tip-text">${keyText(x.text)}</span><button type="button" class="tip-btn" data-tip="next" title="Another tip">›</button>`;
  el.hidden = false;
  el.classList.remove("done");
  requestAnimationFrame(() => el.classList.add("on"));
  if (hovered) spot(true);
}

function spot(on) {
  for (const el of tip?.spot ? document.querySelectorAll(tip.spot) : []) el.classList.toggle("tip-spot", on);
}

// startTips begins the round of checks; the first tip comes after delay
export function startTips(delay = 0) {
  nextAt = Date.now() + delay;
  if (ticking) return;
  ticking = setInterval(tick, CHECK_MS);
  setTimeout(tick, delay + 50);
}

function wire() {
  const el = $("#tip");
  el.addEventListener("click", (e) => {
    if (e.target.closest("[data-tip]")?.dataset.tip === "next") next();
  });
  el.addEventListener("mouseenter", () => {
    hovered = true;
    spot(true);
  });
  el.addEventListener("mouseleave", () => {
    hovered = false;
    spot(false);
    hideAt = Math.max(hideAt, Date.now() + 10e3);
  });
  // you just did what the tip says: a tick, and the next one
  document.addEventListener("tips:learned", (e) => {
    if (!tip?.done?.includes(e.detail)) return;
    el.classList.add("done");
    el.querySelector(".tip-ic").textContent = "✓";
    hovered = false;
    setTimeout(() => el.classList.contains("done") && next(), 2000);
  });
}

wire();

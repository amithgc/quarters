// Tips for the first week: now and then, one quiet line at the bottom right
// about a key or a panel you haven't used yet (one you have never shows).
// It fades after a while; › shows another, × turns them off (Settings
// turns them back on). Loaded by startTips() in app.js.

import { $, REMOTE, activeTab, flash, isNarrow, saveTips, state, tipsDue, tipsState } from "../app.js";

const SHOW_MS = 2 * 60e3; // how long one stays
const GAP_MS = 10 * 60e3; // the quiet between two
const PER_DAY = 3;
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

const today = () => new Date().toDateString();

function pick() {
  const t = tipsState();
  const used = new Set(t.used || []), seen = new Set(t.seen || []);
  return TIPS.find((x) => !seen.has(x.id) && !(x.done || []).some((d) => used.has(d)) && (!x.when || x.when()));
}

// quiet moments only: the window in use, nothing open over it, room for a line
function canShow() {
  return document.visibilityState === "visible" && document.hasFocus() && !isNarrow() && !state.palette.open && !document.querySelector(".overlay:not(.hidden)") && !document.body.classList.contains("focus-mode");
}

function tick() {
  if (tip) {
    if (!hovered && Date.now() > hideAt) hideTip();
    return;
  }
  if (Date.now() < nextAt || !tipsDue() || !canShow()) return;
  const t = tipsState();
  if (t.day === today() && t.count >= PER_DAY) return;
  const x = pick();
  if (x) show(x);
}

function show(x) {
  const t = tipsState();
  saveTips({ ...t, seen: [...(t.seen || []), x.id], day: today(), count: t.day === today() ? (t.count || 0) + 1 : 1 });
  tip = x;
  hideAt = Date.now() + SHOW_MS;
  const el = $("#tip");
  el.innerHTML = `<span class="tip-ic">✦</span><span class="tip-text">${x.text}</span><button type="button" class="tip-btn" data-tip="next" title="Another tip">›</button><button type="button" class="tip-btn" data-tip="off" title="No more tips (Settings turns them back on)">×</button>`;
  el.hidden = false;
  el.classList.remove("done");
  requestAnimationFrame(() => el.classList.add("on"));
}

export function hideTip() {
  const el = $("#tip");
  spot(false);
  el.classList.remove("on");
  tip = null;
  hovered = false;
  nextAt = Date.now() + GAP_MS;
  setTimeout(() => !tip && (el.hidden = true), 600);
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
    const act = e.target.closest("[data-tip]")?.dataset.tip;
    if (act === "off") {
      saveTips({ ...tipsState(), off: true });
      hideTip();
      flash("no more tips · Settings turns them back on");
    } else if (act === "next") {
      spot(false);
      const x = pick();
      if (x) show(x);
      else {
        hideTip();
        flash("that's every tip · ⌘/ lists all the shortcuts");
      }
    }
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
  // you just did what the tip says: a tick, and it goes
  document.addEventListener("tips:learned", (e) => {
    if (!tip?.done?.includes(e.detail)) return;
    el.classList.add("done");
    el.querySelector(".tip-ic").textContent = "✓";
    hideAt = 0;
    hovered = false;
    setTimeout(() => tip && el.classList.contains("done") && hideTip(), 2000);
  });
}

wire();

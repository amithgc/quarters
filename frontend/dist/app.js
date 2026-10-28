import { Terminal } from "./vendor/xterm.js";
import { FitAddon } from "./vendor/addon-fit.js";
import { WebglAddon } from "./vendor/addon-webgl.js";
import { WebLinksAddon } from "./vendor/addon-web-links.js";
import { Unicode11Addon } from "./vendor/addon-unicode11.js";

// In a browser (remote access) the Mac app's bridge comes over a WebSocket.
const REMOTE = !window.go && location.protocol.startsWith("http");
const remoteMod = REMOTE ? await import("./remote.js") : null;

const Go = () => window.go.main.App;
const rt = () => window.runtime;
const $ = (s) => document.querySelector(s);

const BUSY_MS = 1500;
const DEFAULT_LEFT = 290;
const DEFAULT_RIGHT = 330;
const FONT_KEY = "quarters.fontSize";
// Phones read like a chat app; pinch the terminal to change it.
const defaultFontSize = () => (matchMedia("(max-width: 760px)").matches ? 15 : 13);

const state = {
  projects: [],
  selected: null, // path highlighted in the list
  current: null, // path whose tabs are showing
  tabs: {}, // path -> [tab]
  active: {}, // path -> index
  byId: {}, // session id -> tab
  focus: "projects",
  left: DEFAULT_LEFT,
  right: DEFAULT_RIGHT,
  showLeft: true,
  showRight: true,
  stats: null,
  statsPath: null,
  account: null,
  openSec: { mcp: true, skills: false }, // stats sections unfolded
  collapsed: new Set(), // folded project groups
  tokenView: "session",
  fontSize: Number(localStorage.getItem(FONT_KEY)) || defaultFontSize(),
  byKey: {}, // tab key -> tab (a key names a session across windows)
  groups: {}, // automation run id -> split group
  me: "", // this window's name among the windows sharing sessions
  settings: null, // features switched on/off, saved prompts…
  tools: {}, // path -> { skills, mcp, at }
  links: [], // dev servers the terminals mentioned
  palette: { open: false, sel: 0, matches: [] },
};

const darkTheme = {
  background: "#101016",
  foreground: "#E8E6E1",
  cursor: "#E08A6A",
  cursorAccent: "#101016",
  selectionBackground: "rgba(224, 138, 106, 0.30)",
  black: "#1A1A23", red: "#F28B82", green: "#7CD992", yellow: "#E9C46A",
  blue: "#7AB8FF", magenta: "#B59CFF", cyan: "#6FD3D6", white: "#D8D6DF",
  brightBlack: "#5C5A6B", brightRed: "#FFA39C", brightGreen: "#9BE8AD", brightYellow: "#F3D68C",
  brightBlue: "#9CCBFF", brightMagenta: "#CBB8FF", brightCyan: "#93E3E5", brightWhite: "#F5F3EE",
};

const lightTheme = {
  background: "#FFFFFF",
  foreground: "#24232A",
  cursor: "#C8643F",
  cursorAccent: "#FFFFFF",
  selectionBackground: "rgba(200, 100, 63, 0.25)",
  black: "#3B3A42", red: "#C9372C", green: "#2E8A44", yellow: "#9A6B00",
  blue: "#2F6FC8", magenta: "#7F4FD1", cyan: "#1E8A8E", white: "#5C5A66",
  brightBlack: "#6E6B78", brightRed: "#E0483C", brightGreen: "#38A856", brightYellow: "#B88300",
  brightBlue: "#3F82E0", brightMagenta: "#9466E6", brightCyan: "#24A0A5", brightWhite: "#24232A",
};

// The terminals follow macOS light / dark mode, like the rest of the window.
const lightMode = matchMedia("(prefers-color-scheme: light)");
const termTheme = () => (lightMode.matches ? lightTheme : darkTheme);
lightMode.addEventListener("change", () => {
  for (const list of Object.values(state.tabs)) for (const t of list) t.term.options.theme = termTheme();
});

// ---------- helpers ----------

const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

function fmtTokens(n) {
  n = n || 0;
  if (n < 1000) return String(n);
  if (n < 100_000) return (n / 1000).toFixed(1) + "k";
  if (n < 1_000_000) return Math.round(n / 1000) + "k";
  if (n % 1_000_000 === 0) return n / 1_000_000 + "M";
  if (n < 100_000_000) return (n / 1e6).toFixed(2) + "M";
  return Math.round(n / 1e6) + "M";
}

function fmtAgo(ms) {
  if (!ms) return "";
  const d = (Date.now() - ms) / 1000;
  if (d < 60) return "now";
  if (d < 3600) return Math.floor(d / 60) + "m";
  if (d < 86400) return Math.floor(d / 3600) + "h";
  if (d < 14 * 86400) return Math.floor(d / 86400) + "d";
  return Math.floor(d / 604800) + "w";
}

function fmtDuration(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  const p = (n) => String(n).padStart(2, "0");
  return `${p(Math.floor(s / 3600))}:${p(Math.floor(s / 60) % 60)}:${p(s % 60)}`;
}

function prettyModel(id) {
  if (!id) return "—";
  const parts = id.replace(/^claude-/, "").split("-").filter((p) => !/^\d{8,}$/.test(p));
  const [name, ...ver] = parts;
  return name[0].toUpperCase() + name.slice(1) + (ver.length ? " " + ver.join(".") : "");
}

const MODES = { "": "default", default: "default", acceptEdits: "accept edits", bypassPermissions: "bypass perms", plan: "plan" };

let flashTimer;
function flash(msg) {
  const el = $("#flash");
  el.textContent = msg;
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => (el.textContent = ""), 6000);
}

function b64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// fuzzy: case-insensitive subsequence; runs and word starts score higher.
function fuzzy(q, s) {
  q = q.toLowerCase();
  const lower = s.toLowerCase();
  if (!q) return { score: 1, pos: [] };
  let score = 0, qi = 0, prev = -2;
  const pos = [];
  for (let i = 0; i < lower.length && qi < q.length; i++) {
    if (lower[i] !== q[qi]) continue;
    let pts = 1;
    if (i === prev + 1) pts += 4;
    if (i === 0 || /[^a-z0-9]/i.test(s[i - 1])) pts += 3;
    score += pts;
    pos.push(i);
    prev = i;
    qi++;
  }
  if (qi < q.length) return null;
  if (lower.startsWith(q)) score += 10;
  return { score, pos };
}

function highlight(name, pos) {
  const set = new Set(pos);
  return [...name].map((ch, i) => (set.has(i) ? `<span class="hit">${esc(ch)}</span>` : esc(ch))).join("");
}

// ---------- project icons ----------

const iconData = {}; // icon path -> data: URL ("" while loading, null if unreadable)

function loadIcon(path, then) {
  if (!path || path in iconData) return;
  iconData[path] = "";
  Go().IconData(path)
    .then((url) => (iconData[path] = url))
    .catch(() => (iconData[path] = null))
    .finally(then || (() => { renderProjects(); renderTitle(); if (!activeTab()) renderStart(project(state.current)); }));
}

// A letter tile in a colour picked from the name, for projects without an icon.
function monogram(name) {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  const letter = ([...name.replace(/[^\p{L}\p{N}]/gu, "")][0] || "?").toUpperCase();
  return `<span class="picon mono" style="background:hsl(${h % 360} 38% 42%)">${esc(letter)}</span>`;
}

function iconHTML(p) {
  if (p.icon) {
    loadIcon(p.icon);
    if (iconData[p.icon]) return `<img class="picon" src="${iconData[p.icon]}" alt="">`;
  }
  return monogram(p.name);
}

// ---------- pasted images ----------

// imageLinks makes "[Image #N]" in Claude Code's output show the pasted
// image when hovered; Claude Code keeps pasted images in its session log.
function imageLinks(term, tabOf) {
  return {
    provideLinks(y, done) {
      const line = term.buffer.active.getLine(y - 1)?.translateToString(true) || "";
      const links = [];
      for (const m of line.matchAll(/\[Image #(\d+)\]/g)) {
        const n = Number(m[1]);
        links.push({
          text: m[0],
          range: { start: { x: m.index + 1, y }, end: { x: m.index + m[0].length, y } },
          decorations: { pointerCursor: false, underline: true },
          activate: (e) => showImagePop(tabOf(), n, e), // a tap, on a phone
          hover: (e) => showImagePop(tabOf(), n, e),
          leave: hideImagePop,
        });
      }
      done(links);
    },
  };
}

const imageCache = {}; // "tab start|path|n" -> Promise of data: URL
let imagePopFor = "";

function showImagePop(tab, n, e) {
  const key = `${tab.started}|${tab.path}|${n}`;
  imagePopFor = key;
  const pop = $("#img-pop");
  pop.innerHTML = `<div class="ip-cap muted">Image #${n} · loading…</div>`;
  pop.classList.remove("hidden");
  placeImagePop(e.clientX, e.clientY);
  imageCache[key] ??= Go().PastedImage(tab.path, n, tab.started);
  imageCache[key].then(
    (url) => {
      if (imagePopFor !== key) return;
      const img = document.createElement("img");
      img.alt = "";
      img.onload = () => placeImagePop(e.clientX, e.clientY);
      img.src = url;
      const cap = document.createElement("div");
      cap.className = "ip-cap muted";
      cap.textContent = `Image #${n}`;
      pop.replaceChildren(img, cap);
    },
    (err) => {
      delete imageCache[key];
      if (imagePopFor === key) pop.innerHTML = `<div class="ip-cap muted">Image #${n} · ${esc(String(err).replace(/^Error: /, ""))}</div>`;
    },
  );
}

// placeImagePop puts the popup above the pointer, or below when there is no room.
function placeImagePop(x, y) {
  const pop = $("#img-pop");
  const w = pop.offsetWidth, h = pop.offsetHeight, pad = 12;
  const left = Math.max(pad, Math.min(x - w / 2, innerWidth - w - pad));
  const top = y - h - 16 >= pad ? y - h - 16 : Math.min(y + 20, innerHeight - h - pad);
  pop.style.left = left + "px";
  pop.style.top = top + "px";
}

function hideImagePop() {
  imagePopFor = "";
  $("#img-pop").classList.add("hidden");
}

// ---------- sessions ----------

function project(path) {
  return state.projects.find((p) => p.path === path);
}

function tabsOf(path) {
  return state.tabs[path] || [];
}

function activeTab() {
  const list = tabsOf(state.current);
  if (!list.length) return null;
  return list[Math.min(state.active[state.current] || 0, list.length - 1)];
}

function tabStatus(tab) {
  if (tab.exited) return "ended";
  return Date.now() - tab.lastData < BUSY_MS ? "busy" : "waiting";
}

function projectStatus(path) {
  const list = tabsOf(path);
  if (list.some((t) => tabStatus(t) === "busy")) return "busy";
  if (list.some((t) => !t.exited)) return "waiting";
  return "idle";
}

const STATUS_ICON = {
  busy: '<span class="green">●</span>',
  waiting: '<span class="yellow">◐</span>',
  idle: '<span class="muted">○</span>',
  ended: '<span class="red">✕</span>',
};

// opts (automation tabs): { title, dir, command, autoKey, group } — a shell in dir with command typed in.
// opts.resume: a conversation id to resume; opts.cwd: a worktree of the project to run in.
// opts.adopt: a session already running (started in another window, or before a reload) to show.
async function newTab(path, kind = "claude", opts = {}) {
  const adopt = opts.adopt;
  const p = project(path);
  if (!adopt && p && p.missing) {
    flash(`folder not found: ${p.shortPath}`);
    return;
  }
  if (!adopt) {
    state.current = path;
    state.selected = path;
  }

  const el = document.createElement("div");
  el.className = "term";
  const head = document.createElement("div");
  head.className = "pane-head";
  head.innerHTML = `<span class="pane-name">${esc(opts.title || kind)}</span><span class="pane-status"></span><span class="pane-dir muted">${esc((opts.dir || "").split("/").slice(-2).join("/"))}</span>`;
  const body = document.createElement("div");
  body.className = "term-body";
  const paused = document.createElement("div");
  paused.className = "term-paused hidden";
  paused.textContent = isTouch() ? "In use on another device · tap to continue here" : "In use on another device · click to continue here";
  el.append(head, body, paused);
  $("#terms").appendChild(el);

  const term = new Terminal({
    // Fallbacks cover symbols JetBrains Mono lacks (Claude Code's ❯, ⏵, ✻…)
    // before the system reaches for the colour emoji font; Quarters Symbols
    // has the ones phones would draw as emoji (⏺, ✔…), two cells wide.
    fontFamily: '"JetBrains Mono", "Quarters Symbols", "MesloLGS NF", Menlo, "Apple Symbols", "Symbols Nerd Font", monospace',
    fontSize: state.fontSize,
    lineHeight: 1.2,
    letterSpacing: 0,
    fontWeight: 400,
    fontWeightBold: 700,
    cursorBlink: true,
    cursorStyle: "block",
    allowProposedApi: true,
    macOptionIsMeta: true,
    macOptionClickForcesSelection: true,
    scrollback: 10000,
    theme: termTheme(),
    linkHandler: { activate: (_e, uri) => rt().BrowserOpenURL(uri) },
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.loadAddon(new WebLinksAddon((_e, uri) => rt().BrowserOpenURL(uri)));
  if (kind !== "shell") term.registerLinkProvider(imageLinks(term, () => tab));
  const unicode = new Unicode11Addon();
  term.loadAddon(unicode);
  term.unicode.activeVersion = "11";

  const meta = adopt ? adopt.meta : null;
  const autoOpts = opts.autoKey ? { title: opts.title, dir: opts.dir, command: opts.command, autoKey: opts.autoKey, group: opts.group } : null;
  const startOpts = { resume: opts.resume || "", cwd: opts.cwd || "", title: opts.title || "" };
  const tab = { key: meta ? meta.key : newKey(), started: meta ? meta.started : Date.now(), group: opts.group || null, head, autoOpts, startOpts, id: 0, kind, path, term, fit, el, title: "", fixedTitle: opts.title || "", wt: opts.cwd && opts.cwd !== path ? opts.cwd.split("/").pop() : "", autoKey: opts.autoKey || "", lastData: 0, exited: false, code: 0, owner: adopt ? adopt.owner : state.me, pty: adopt && adopt.cols ? { cols: adopt.cols, rows: adopt.rows } : null, off: 0, synced: false, early: [] };
  state.byKey[tab.key] = tab;
  const list = (state.tabs[path] = tabsOf(path).concat(tab));
  if (!adopt) state.active[path] = list.length - 1;
  showActive();

  term.open(body);
  // Phones and tablets draw with the DOM renderer: dependable in mobile
  // browsers and lighter on the battery than WebGL.
  if (!isTouch()) {
    try {
      const gl = new WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      term.loadAddon(gl);
    } catch {
      /* falls back to the DOM renderer */
    }
  }
  fitTab(tab);
  // Phones: no AutoFill bar (passwords, cards) over the keyboard.
  term.textarea?.setAttribute("autocomplete", "off");

  // Shift+Enter makes a new line in Claude Code: it sends Ctrl+J (line
  // feed), which Claude Code always reads as "new line", never "send".
  term.attachCustomKeyEventHandler((e) => {
    if (e.key === "Enter" && e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey && tab.kind !== "shell") {
      if (e.type === "keydown" && tab.id) Go().Write(tab.id, "\n");
      e.preventDefault();
      return false;
    }
    if (e.metaKey) return false; // ⌘ shortcuts belong to the menus
    return true;
  });
  term.onTitleChange((t) => {
    tab.title = cleanTitle(t);
    renderTabs();
  });
  term.onFocus?.(() => setFocus("terminal"));
  el.addEventListener("pointerdown", () => claimTab(tab));
  if (isTouch() && kind !== "shell") wireMenuTaps(tab);
  el.addEventListener("mousedown", () => {
    setFocus("terminal");
    // Clicking a pane of a split automation makes it the one you type into.
    const list = tabsOf(tab.path), i = list.indexOf(tab);
    if (i >= 0 && state.active[tab.path] !== i) {
      state.active[tab.path] = i;
      if (tab.group) tab.group.focus = tab;
      showActive();
    }
  });

  try {
    if (adopt) tab.id = adopt.id;
    else {
      const m = { key: tab.key, started: tab.started, path, kind, title: opts.title || "", dir: opts.dir || "", command: opts.command || "", autoKey: tab.autoKey, group: groupMeta(opts.group), resume: opts.resume || "", cwd: opts.cwd || "" };
      tab.id = opts.autoKey
        ? await Go().StartCommandSession(opts.dir || path, opts.command, term.cols, term.rows, m)
        : await Go().StartSession(path, kind, term.cols, term.rows, m);
    }
  } catch (err) {
    flash(String(err));
    closeTab(tab, true);
    return;
  }
  state.byId[tab.id] = tab;
  term.onData((d) => {
    if (!canType()) return watchOnly();
    tab.lastInput = Date.now();
    if (isPaused(tab)) claimTab(tab);
    if (!tab.exited) Go().Write(tab.id, stickyCtrl(d));
  });
  term.onBinary((d) => canType() && !tab.exited && Go().Write(tab.id, d));
  let resizing = 0;
  term.onResize(() => {
    clearTimeout(resizing); // dragging a panel resizes many times: send the size it settles on
    resizing = setTimeout(() => {
      if (tab.owner !== state.me || tab.exited) return;
      const size = `${term.cols}x${term.rows}`;
      if (tab.sent && tab.sent !== size && !mirrors(tab)) term.reset(); // Claude reprints it all at the new size
      tab.sent = size;
      Go().Resize(tab.id, term.cols, term.rows);
    }, 120);
  });
  await syncOutput(tab);
  if (adopt && adopt.exited) markExited(tab, adopt.code);
  if (!adopt) Go().Resize(tab.id, term.cols, term.rows);
  tab.sent = adopt ? "" : `${term.cols}x${term.rows}`;
  if (!opts.background) {
    setFocus("terminal");
    term.focus();
  }
  render();
  refreshStats();
  return tab;
}

const newKey = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;

// What other windows need to rebuild a split automation's group.
const groupMeta = (g) => g && { id: g.id, name: g.name, autoName: g.autoName, path: g.path, layout: g.layout, cols: g.cols || null, rows: g.rows || null };

function groupFor(g) {
  if (!g || !g.id) return null;
  return (state.groups[g.id] ||= { ...g, focus: null });
}

// syncOutput fills a tab's terminal with what its session has written so
// far, then lets live output through (skipping what the snapshot covered).
async function syncOutput(tab, reset) {
  tab.synced = false;
  let snap = { data: "", off: 0 };
  try {
    snap = (await Go().Snapshot(tab.id)) || snap;
  } catch {}
  if (reset) tab.term.reset();
  if (snap.data) tab.term.write(b64ToBytes(snap.data));
  tab.off = snap.off;
  tab.synced = true;
  for (const c of tab.early.splice(0)) writeChunk(tab, c);
}

function writeChunk(tab, { data, off }) {
  if (typeof off === "number" && off < tab.off) return; // already shown
  const bytes = b64ToBytes(data);
  tab.off = (off ?? tab.off) + bytes.length;
  if (isPaused(tab)) return setPaused(tab, true); // drawn for another window's size
  tab.term.write(bytes);
}

// Shells and automations mirror live in every window. A Claude tab is
// drawn by the window in use only; others pause, and catch up when used
// (Claude reprints the whole conversation for the new size).
const mirrors = (tab) => tab.kind === "shell" || !!tab.autoKey;
const isPaused = (tab) => !mirrors(tab) && !!tab.owner && tab.owner !== state.me;

function setPaused(tab, on) {
  if (tab.paused === on) return;
  tab.paused = on;
  tab.el.querySelector(".term-paused")?.classList.toggle("hidden", !on);
}

// claimTab gives a session this window's terminal size (the window you
// are using decides; the others just watch).
function claimTab(tab) {
  if (!tab || !tab.id || tab.exited || (tab.owner === state.me && !tab.paused)) return;
  tab.owner = state.me;
  fitTab(tab); // back to this window's size
  // Claude reprints the whole conversation for this size (a claim always
  // makes it redraw), so start from an empty terminal.
  if (!mirrors(tab)) tab.term.reset();
  setPaused(tab, false);
  tab.sent = `${tab.term.cols}x${tab.term.rows}`;
  Go().Claim(tab.id, tab.term.cols, tab.term.rows);
}

// On a touch screen, Claude Code's menus (trust this folder? allow this
// tool?) are answered by tapping: a tap on an option moves the ❯ to it,
// a tap on the chosen one presses Enter. Phones have no arrow keys.
function wireMenuTaps(tab) {
  let down = null;
  tab.el.addEventListener("touchstart", (e) => {
    const t = e.touches[0];
    down = e.touches.length === 1 ? { x: t.clientX, y: t.clientY, t: Date.now() } : null;
  }, { passive: true });
  tab.el.addEventListener("touchend", (e) => {
    const t = e.changedTouches[0];
    if (!down || !t || Date.now() - down.t > 500 || Math.hypot(t.clientX - down.x, t.clientY - down.y) > 10) return;
    down = null;
    const keys = menuKeys(tab.term, t.clientY);
    if (!keys || !canType() || tab.exited) return;
    e.preventDefault(); // no click: the keyboard stays down
    claimTab(tab);
    tab.lastInput = Date.now();
    Go().Write(tab.id, keys);
  }, { passive: false });
}

// menuKeys is what to type for a tap at clientY: arrows to move the menu's
// ❯ to the tapped option, or Enter on the chosen one; null if the tap
// isn't on a menu option.
function menuKeys(term, clientY) {
  const screen = term.element?.querySelector(".xterm-screen");
  if (!screen) return null;
  const r = screen.getBoundingClientRect();
  const row = Math.floor((clientY - r.top) / (r.height / term.rows));
  if (row < 0 || row >= term.rows) return null;
  const buf = term.buffer.active, top = buf.viewportY;
  const text = (y) => buf.getLine(top + y)?.translateToString(true) ?? "";
  // the chosen option: "❯ Yes, …" (the input box's bare "❯" has no text after it)
  let sel = -1, col = -1;
  for (let y = 0; y < term.rows; y++) {
    const m = /^(\s*)❯\s+(?=\S)/.exec(text(y));
    if (m && !/^\s*[─━]/.test(text(y - 1)) && !/^\s*[─━]/.test(text(y + 1))) {
      sel = y;
      col = m[0].length;
    }
  }
  if (sel < 0) return null;
  if (row === sel) return "\r";
  // the options are the lines whose text starts where the chosen one's does
  const isOption = (y) => {
    const t = text(y);
    return t.trim() !== "" && t.search(/\S/) === col && !buf.getLine(top + y)?.isWrapped;
  };
  if (!isOption(row)) return null;
  const step = row > sel ? 1 : -1;
  let n = 0;
  for (let y = sel + step; y !== row + step; y += step) {
    const t = text(y);
    if (t.trim() === "") continue;
    if (!isOption(y) && t.search(/\S/) < col) return null; // left the menu
    if (isOption(y)) n++;
  }
  return n > 0 && n < 20 ? (step > 0 ? "\x1b[B" : "\x1b[A").repeat(n) : null;
}

// fitTab sizes a tab's terminal: to this window, or for a shell another
// window drives, to the session's own size (scaled down if it doesn't fit),
// so output that moves the cursor lands where the program meant it to.
function fitTab(t) {
  if (!t) return;
  const x = t.term.element;
  if (!x) return;
  if (!(mirrors(t) && t.owner && t.owner !== state.me && t.pty)) {
    if (x.style.transform) x.style.width = x.style.height = x.style.transform = "";
    t.fit.fit();
    return;
  }
  if (t.term.cols !== t.pty.cols || t.term.rows !== t.pty.rows) t.term.resize(t.pty.cols, t.pty.rows);
  const screen = x.querySelector(".xterm-screen"), box = x.parentElement;
  if (!screen || !box || !screen.offsetWidth) return;
  const w = screen.offsetWidth + 12, h = screen.offsetHeight; // + the scroll bar
  const s = Math.min(1, box.clientWidth / w, box.clientHeight / h);
  x.style.width = w + "px";
  x.style.height = h + "px";
  x.style.transformOrigin = "0 0";
  x.style.transform = `scale(${s})`;
}

function claimActive() {
  const cur = activeTab();
  if (cur) claimTab(cur);
}

// adoptSession shows a session this window did not start.
async function adoptSession(info) {
  const m = info && info.meta;
  if (!m || !m.key || !m.path || state.byKey[m.key] || state.byId[info.id]) return;
  await newTab(m.path, m.kind || "claude", { adopt: info, title: m.title, dir: m.dir, command: m.command, autoKey: m.autoKey, group: groupFor(m.group), resume: m.resume, cwd: m.cwd, background: true });
}

// syncSessions matches the tabs to the sessions running in quarters: on
// start (a reloaded or remote window) and after a lost connection.
async function syncSessions(reconnect) {
  let list;
  try {
    list = (await Go().Sessions()) || [];
  } catch {
    return;
  }
  const live = new Set(list.map((i) => i.id));
  for (const t of Object.values(state.byId)) if (!live.has(t.id)) closeTab(t, true, true);
  for (const info of list) {
    const t = state.byId[info.id];
    if (!t) await adoptSession(info);
    else {
      t.owner = info.owner;
      if (info.cols) t.pty = { cols: info.cols, rows: info.rows };
      fitTab(t);
      if (reconnect) await syncOutput(t, true);
      if (info.exited && !t.exited) markExited(t, info.code);
    }
  }
  showActive();
  render();
}

function cleanTitle(t) {
  t = (t || "").trim().replace(/^[^\w\s]\s+/u, ""); // spinner glyph Claude Code puts first
  return t === "Claude Code" ? "" : t;
}

// closeTab ends a tab's session; ended elsewhere (gone) it only removes the tab.
function closeTab(tab, silent, gone) {
  const list = tabsOf(tab.path);
  const i = list.indexOf(tab);
  if (i < 0) return;
  if (tab.id && !gone) Go().CloseSession(tab.id);
  delete state.byId[tab.id];
  delete state.byKey[tab.key];
  tab.term.dispose();
  tab.el.remove();
  list.splice(i, 1);
  if (!list.length) delete state.tabs[tab.path];
  state.active[tab.path] = Math.max(0, Math.min(state.active[tab.path] || 0, list.length - 1));
  if (!silent) showActive();
  render();
}

async function requestCloseEntry(k) {
  const e = entries(state.current)[k];
  if (!e) return;
  if (!e.group) return requestCloseTab(e.tabs[0]);
  const live = e.tabs.filter((t) => !t.exited).length;
  if (live && !(await Go().Confirm(`Close “${e.group.name}”?`, `This ends its ${live} running shell(s).`, "Close all"))) return;
  for (const t of e.tabs) closeTab(t, true);
  showActive();
  render();
}

async function requestCloseTab(tab) {
  if (!tab) return;
  const name = tab.kind === "shell" ? "shell" : "Claude Code";
  if (tab.exited || (await Go().Confirm("Close tab?", `This ends the ${name} session running in this tab.`, "Close"))) closeTab(tab);
}

async function restartTab(tab) {
  const { path, kind } = tab;
  const i = tabsOf(path).indexOf(tab);
  closeTab(tab, true);
  // a resumed conversation restarts where it left off, not from the beginning
  await newTab(path, tab.autoOpts ? kind : kind === "claude" || kind === "resume" ? "continue" : kind, tab.autoOpts || { cwd: tab.startOpts.cwd, title: tab.startOpts.title });
  // move the fresh tab back into the old slot
  const list = tabsOf(path);
  const fresh = list.pop();
  list.splice(i, 0, fresh);
  state.active[path] = i;
  showActive();
  render();
}

// What the tab bar shows: one entry per tab, except that the tabs of a split
// automation (layout grid / columns / rows) share one entry.
function entries(path) {
  const out = [];
  const seen = new Map();
  for (const t of tabsOf(path)) {
    const g = t.group && t.group.layout !== "tabs" ? t.group : null;
    if (g && seen.has(g)) {
      seen.get(g).tabs.push(t);
      continue;
    }
    const e = { group: g, tabs: [t] };
    if (g) seen.set(g, e);
    out.push(e);
  }
  return out;
}

const isSplit = (tab) => !!(tab && tab.group && tab.group.layout !== "tabs");

function gridFor(layout, n) {
  if (layout === "columns") return [n, 1];
  if (layout === "rows") return [1, n];
  const cols = Math.ceil(Math.sqrt(n));
  return [cols, Math.ceil(n / cols)];
}

function showActive() {
  const cur = activeTab();
  const panes = isSplit(cur) ? tabsOf(cur.path).filter((t) => t.group === cur.group) : cur ? [cur] : [];
  const terms = $("#terms");
  const split = panes.length > 1;
  terms.classList.toggle("split", split);
  if (split) applySplitSizes(cur.group, panes.length);
  else terms.style.gridTemplateColumns = terms.style.gridTemplateRows = "";
  document.querySelectorAll(".term").forEach((el) => {
    const i = panes.findIndex((t) => t.el === el);
    el.classList.toggle("active", i >= 0);
    el.classList.toggle("focused-pane", split && !!cur && el === cur.el);
    el.style.order = i >= 0 ? String(i) : "";
  });
  const start = !cur && state.current ? project(state.current) : null;
  $("#welcome").classList.toggle("hidden", !!cur || !!start);
  renderStart(start);
  if (cur) {
    requestAnimationFrame(() => {
      for (const t of panes) {
        try {
          fitTab(t);
        } catch {}
      }
      if (state.focus === "terminal" && !$("#tab-list .tab-rename") && !isTouch()) cur.term.focus();
      if (document.hasFocus()) claimTab(cur);
      placeSplitHandles();
    });
  } else placeSplitHandles();
  reportViewing();
  document.body.classList.toggle("has-tab", !!cur);
  renderTabs();
}

// ---------- split panes: drag the lines between them ----------

const MIN_PANE = 140; // px

// track sizes: the saved ones when they fit the grid, else equal
const tracks = (saved, n) => (saved && saved.length === n ? saved : Array(n).fill(1));

function applySplitSizes(group, n) {
  const [cols, rows] = gridFor(group.layout, n);
  const tpl = (sizes) => sizes.map((f) => `minmax(0, ${f}fr)`).join(" ");
  $("#terms").style.gridTemplateColumns = tpl(tracks(group.cols, cols));
  $("#terms").style.gridTemplateRows = tpl(tracks(group.rows, rows));
}

// placeSplitHandles puts a draggable line in each gap between pane columns and rows.
function placeSplitHandles() {
  const terms = $("#terms");
  terms.querySelectorAll(".split-handle").forEach((h) => h.remove());
  const cur = activeTab();
  if (!isSplit(cur) || !terms.classList.contains("split")) return;
  const panes = tabsOf(cur.path).filter((t) => t.group === cur.group);
  if (panes.length < 2) return;
  const [cols, rows] = gridFor(cur.group.layout, panes.length);
  const box = terms.getBoundingClientRect();
  const rect = (i) => panes[Math.min(i, panes.length - 1)].el.getBoundingClientRect();
  const add = (axis, k, pos) => {
    const h = document.createElement("div");
    h.className = `split-handle ${axis}`;
    h.title = "Drag to resize · double-click to make all equal";
    if (axis === "col") h.style.left = pos - box.left + "px";
    else h.style.top = pos - box.top + "px";
    h.addEventListener("mousedown", (e) => dragSplit(e, cur.group, panes, axis, k));
    h.addEventListener("dblclick", () => {
      cur.group.cols = cur.group.rows = null;
      applySplitSizes(cur.group, panes.length);
      saveSplitSizes(cur.group);
      fitActive();
    });
    terms.appendChild(h);
  };
  for (let k = 0; k < cols - 1; k++) add("col", k, (rect(k).right + rect(k + 1).left) / 2);
  for (let k = 0; k < rows - 1; k++) add("row", k, (rect(k * cols).bottom + rect((k + 1) * cols).top) / 2);
}

function dragSplit(e, group, panes, axis, k) {
  e.preventDefault();
  const [cols, rows] = gridFor(group.layout, panes.length);
  // current track sizes in px, read from the panes
  const px = axis === "col"
    ? Array.from({ length: cols }, (_, i) => panes[i].el.getBoundingClientRect().width)
    : Array.from({ length: rows }, (_, i) => panes[Math.min(i * cols, panes.length - 1)].el.getBoundingClientRect().height);
  const start = axis === "col" ? e.clientX : e.clientY;
  document.body.classList.add("dragging", axis === "col" ? "drag-col" : "drag-row");
  const move = (ev) => {
    const pair = px[k] + px[k + 1];
    const a = Math.max(MIN_PANE, Math.min(pair - MIN_PANE, px[k] + (axis === "col" ? ev.clientX : ev.clientY) - start));
    const sizes = px.slice();
    sizes[k] = a;
    sizes[k + 1] = pair - a;
    const total = sizes.reduce((x, y) => x + y, 0);
    const fr = sizes.map((v) => Math.round((v / total) * sizes.length * 1000) / 1000);
    if (axis === "col") group.cols = fr;
    else group.rows = fr;
    applySplitSizes(group, panes.length);
    fitActive();
  };
  const up = () => {
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    document.body.classList.remove("dragging", "drag-col", "drag-row");
    placeSplitHandles();
    saveSplitSizes(group);
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
}

function saveSplitSizes(group) {
  if (!group.autoName) return;
  Go().SaveAutomationSizes(group.path, group.autoName, group.cols || [], group.rows || []).catch((err) => flash(String(err)));
  // keep the project's copy current, so the next run starts with these sizes
  const a = (project(group.path)?.automations || []).find((x) => x.name === group.autoName);
  if (a) {
    a.cols = group.cols;
    a.rows = group.rows;
  }
}

function selectTab(i) {
  const list = tabsOf(state.current);
  if (i < 0 || i >= list.length) return;
  state.active[state.current] = i;
  if (list[i].group) list[i].group.focus = list[i];
  setFocus("terminal");
  showActive();
}

// selectEntry picks the k-th entry of the tab bar (a split group returns to the pane last used).
function selectEntry(k) {
  const e = entries(state.current)[k];
  if (!e) return;
  const t = e.group && e.tabs.includes(e.group.focus) ? e.group.focus : e.tabs[0];
  selectTab(tabsOf(state.current).indexOf(t));
}

function currentEntry() {
  const cur = activeTab();
  return entries(state.current).findIndex((e) => e.tabs.includes(cur));
}

function cycleTab(d) {
  const n = entries(state.current).length;
  if (!n) return;
  selectEntry((Math.max(0, currentEntry()) + d + n) % n);
}

// openProject shows a project: its tabs, or — when nothing runs there yet —
// the start screen, where C starts Claude Code.
function openProject(path) {
  closeDrawers();
  state.selected = path;
  state.current = path;
  if (tabsOf(path).length) setFocus("terminal");
  showActive();
  render();
  refreshStats();
  autoStart(path);
}

// autoStart runs a project's "run on open" automations the first time it is
// opened after Quarters starts (not again if you stop them).
const autoStarted = new Set();
async function autoStart(path) {
  const p = project(path);
  if (REMOTE || !p || p.missing || autoStarted.has(path)) return;
  autoStarted.add(path);
  const autos = (p.automations || []).filter((a) => a.auto && !autoRunning(path, a));
  for (const a of autos) await runAutomation(path, a);
  if (autos.length) flash(`▶ started ${autos.map((a) => a.name).join(", ")} · ⌘T for Claude`);
}

// renderStart fills the start screen for p (a project with no tabs), or hides it.
function renderStart(p) {
  const el = $("#start");
  el.classList.toggle("hidden", !p);
  if (!p) return;
  const sig = `${p.path}|${on("history")}`;
  if (el.dataset.path !== sig || el.dataset.icon !== (iconData[p.icon] ? p.icon : "")) {
    el.dataset.path = sig;
    el.dataset.icon = iconData[p.icon] ? p.icon : "";
    const autos = (p.automations || []).map((a, i) =>
      `<button class="btn" type="button" data-start="auto" data-i="${i}">▶ ${esc(a.name)}</button>`).join("");
    el.innerHTML = `
      <div class="st-project">${iconHTML(p)}<span class="st-name">${esc(p.name)}</span></div>
      <div class="st-path muted">${esc(p.shortPath)}${p.branch ? ` · <span class="violet">⎇ ${esc(p.branch)}</span>` : ""}${p.missing ? ` · <span class="red">folder not found</span>` : ""}</div>
      <button class="st-press" type="button" data-start="claude">
        <span class="keys-only">Press</span><b class="st-key">C</b><span class="keys-only">to start Claude</span><span class="touch-only">Start Claude</span>
      </button>
      <div class="st-more muted">
        <button type="button" data-start="continue"><b class="kbd keys-only">⇧C</b><span class="touch-only">↻</span> continue last conversation</button>
        <button type="button" data-start="shell"><b class="kbd keys-only">S</b><span class="touch-only">›_</span> shell</button>
        ${on("history") ? '<button type="button" data-start="history"><span class="muted">🕘</span> past conversations</button>' : ""}
        <button type="button" data-start="edit"><span class="muted">✎</span> edit</button>
      </div>
      ${autos ? `<div class="st-autos">${autos}</div>` : ""}`;
  }
  if (state.focus === "terminal") setFocus("projects");
  if (document.activeElement?.closest?.(".xterm")) document.activeElement.blur();
}

function startHere(kind, i) {
  const p = project(state.current);
  if (!p || tabsOf(p.path).length) return;
  if (kind === "auto") return runAutomation(p.path, p.automations[i]);
  if (kind === "edit") return openProjectDialog(p);
  if (kind === "history") return openHistory(p.path);
  setFocus("terminal");
  newTab(p.path, kind);
}

function wireStart() {
  $("#start").addEventListener("click", (e) => {
    const b = e.target.closest("[data-start]");
    if (b) startHere(b.dataset.start, Number(b.dataset.i));
  });
  window.addEventListener("keydown", (e) => {
    if (e.defaultPrevented || $("#start").classList.contains("hidden")) return;
    if (state.palette.open || dlg.open || e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.target.closest?.("input, textarea, .overlay, .xterm")) return;
    const k = e.key.toLowerCase();
    if (k === "c") startHere(e.shiftKey ? "continue" : "claude");
    else if (k === "s") startHere("shell");
    else return;
    e.preventDefault();
  });
}

function setFocus(f) {
  state.focus = f;
  $("#left").classList.toggle("focused", f === "projects");
  $("#center").classList.toggle("focused", f === "terminal");
}

// ---------- "Claude is waiting" notifications ----------

const WORK_MS = 4000; // a stretch of work this long, then quiet, means Claude finished and wants you
let badgeCount = -1;

const tabKey = (t) => t.key;
const tabSeen = (t) => document.hasFocus() && t.el.classList.contains("active");

// checkAttention runs twice a second: a Claude tab that worked for a while
// and went quiet is waiting for you. If you are not looking at it, it is
// marked, counted on the Dock icon and announced with a notification.
function checkAttention() {
  const now = Date.now();
  let unseen = 0;
  for (const list of Object.values(state.tabs))
    for (const t of list) {
      if (t.kind === "shell" || !t.id) continue;
      if (t.workStart && now - t.workLast >= QUIET_MS) {
        const worked = t.workLast - t.workStart >= WORK_MS;
        t.workStart = 0;
        if (worked && !t.exited && !tabSeen(t)) {
          t.unseen = true;
          const p = project(t.path);
          Go().Notify(`${p ? p.name : "Claude"} · your turn`, t.title || "Claude Code is waiting for you", tabKey(t));
        }
      }
      if (t.unseen && (tabSeen(t) || t.exited)) t.unseen = false;
      if (t.unseen) unseen++;
    }
  if (unseen !== badgeCount) {
    badgeCount = unseen;
    Go().SetBadge(unseen);
    renderTabs();
    renderProjects();
  }
}

function openNotifiedTab(key) {
  for (const list of Object.values(state.tabs))
    for (const t of list)
      if (tabKey(t) === key) {
        openProject(t.path);
        selectTab(tabsOf(t.path).indexOf(t));
        return;
      }
}

// ---------- events from Go ----------

function wireEvents() {
  rt().EventsOn("pty:data", (chunk) => {
    const tab = state.byId[chunk.id];
    if (!tab) return;
    if (tab.synced) writeChunk(tab, chunk);
    else tab.early.push(chunk);
    const now = Date.now();
    tab.lastData = now;
    // Output that is not the echo of your typing counts as Claude working.
    if (now - (tab.lastInput || 0) > 250) {
      if (!tab.workStart || now - tab.workLast > QUIET_MS) tab.workStart = now;
      tab.workLast = now;
    }
  });
  rt().EventsOn("pty:exit", ({ id, code }) => {
    const tab = state.byId[id];
    if (tab) markExited(tab, code);
  });
  rt().EventsOn("session:open", adoptSession);
  rt().EventsOn("session:close", ({ id }) => {
    const tab = state.byId[id];
    if (tab) closeTab(tab, false, true);
  });
  rt().EventsOn("pty:owner", ({ id, owner, cols, rows }) => {
    const tab = state.byId[id];
    if (!tab) return;
    tab.owner = owner;
    if (cols) tab.pty = { cols, rows };
    if (owner === state.me && tab.paused) return claimTab(tab); // typed here while paused: catch up
    fitTab(tab);
    if (owner === state.me) Go().Resize(id, tab.term.cols, tab.term.rows);
  });
  rt().EventsOn("notify:open", openNotifiedTab);
  rt().EventsOn("flash", flash);
  rt().EventsOn("menu", onMenu);
  rt().EventsOn("settings", loadSettings);
  rt().EventsOn("links", refreshLinks);
  rt().EventsOn("restore:done", () => renderRestore([]));
}

// markExited shows the "process ended" bar on a tab.
function markExited(tab, code) {
  if (tab.exited) return;
  tab.exited = true;
  tab.code = code;
  const bar = document.createElement("div");
  bar.className = "ended";
  bar.innerHTML = `<span class="grow">Process ended${code ? ` (exit code ${code})` : ""}.</span>
    <button class="btn primary" type="button" data-act="restart">Restart</button>
    <button class="btn" type="button" data-act="close">Close tab</button>`;
  bar.addEventListener("click", (e) => {
    const act = e.target.closest("button")?.dataset.act;
    if (act === "restart") restartTab(tab);
    if (act === "close") closeTab(tab);
  });
  tab.el.appendChild(bar);
  render();
}

function onMenu(action) {
  const cur = activeTab();
  const target = state.focus === "projects" || !state.current ? state.selected || state.current : state.current;
  switch (action) {
    case "new-claude":
      if (target) newTab(target, "claude");
      break;
    case "continue":
      if (target) newTab(target, "continue");
      break;
    case "new-shell":
      if (target) newTab(target, "shell");
      break;
    case "close-tab":
      if (isSplit(cur)) requestCloseEntry(currentEntry());
      else requestCloseTab(cur);
      break;
    case "next-tab":
      cycleTab(1);
      break;
    case "prev-tab":
      cycleTab(-1);
      break;
    case "clear":
      cur?.term.clear();
      break;
    case "palette":
      openPalette();
      break;
    case "add-project":
      addProject();
      break;
    case "edit-config":
      Go().OpenConfig().catch((e) => flash(String(e)));
      break;
    case "reveal":
      if (target) Go().RevealProject(target).catch((e) => flash(String(e)));
      break;
    case "remote":
      openRemoteDialog();
      break;
    case "remote-toggle":
      toggleRemote();
      break;
    case "settings":
      openSettings();
      break;
    case "history":
      openHistory(target);
      break;
    case "worktrees":
      if (target && on("worktrees")) showWorktreeMenu(target, $("#btn-wt").classList.contains("hidden") ? $("#tab-path") : $("#btn-wt"));
      break;
    case "toggle-left":
      state.showLeft = !state.showLeft;
      applyLayout();
      break;
    case "toggle-right":
      state.showRight = !state.showRight;
      applyLayout();
      break;
    case "font-up":
      setFontSize(state.fontSize + 1);
      break;
    case "font-down":
      setFontSize(state.fontSize - 1);
      break;
    case "font-reset":
      setFontSize(defaultFontSize());
      break;
    default:
      if (action.startsWith("tab-")) selectEntry(Number(action.slice(4)) - 1);
  }
}

function setFontSize(n) {
  state.fontSize = Math.max(9, Math.min(24, n));
  localStorage.setItem(FONT_KEY, String(state.fontSize));
  for (const list of Object.values(state.tabs)) for (const t of list) t.term.options.fontSize = state.fontSize;
  fitActive();
}

function addProject() {
  openProjectDialog(null);
}

// ---------- automations ----------

const autoKey = (path, a) => `${path}::${a.name}`;
const autoTabs = (path, a) => tabsOf(path).filter((t) => t.autoKey === autoKey(path, a));
const autoRunning = (path, a) => autoTabs(path, a).some((t) => !t.exited);

async function runAutomation(path, a) {
  const old = autoTabs(path, a);
  if (old.some((t) => !t.exited)) {
    if (!(await Go().Confirm(`Restart “${a.name}”?`, `Its ${old.length} tab(s) are still running. Restarting stops them and starts fresh ones.`, "Restart"))) return;
  }
  for (const t of old) closeTab(t, true);
  state.current = path;
  state.selected = path;
  const group = { id: newKey(), name: a.name, autoName: a.name, path, layout: a.layout || "grid", focus: null, cols: a.cols, rows: a.rows };
  state.groups[group.id] = group;
  let first = null;
  for (const t of a.tabs) {
    const tab = await newTab(path, "shell", { title: t.name, dir: t.dir, command: t.command, autoKey: autoKey(path, a), group, background: true });
    first = first || tab;
  }
  if (first) selectTab(tabsOf(path).indexOf(first));
  flash(`▶ ${a.name}: ${a.tabs.length} tab${a.tabs.length === 1 ? "" : "s"}`);
}

async function stopAutomation(path, a) {
  for (const t of autoTabs(path, a)) closeTab(t, true);
  showActive();
  render();
  flash(`■ stopped ${a.name}`);
}

// A small menu of a project's automations, under the element that opened it.
function showAutomationMenu(path, anchor) {
  const p = project(path);
  if (!p || !(p.automations || []).length) return;
  const menu = $("#auto-menu");
  menu.innerHTML =
    `<div class="am-head">AUTOMATIONS · ${esc(p.name)}</div>` +
    p.automations
      .map((a, i) => {
        const on = autoRunning(path, a);
        return `<div class="am-row"><button class="am-item" type="button" data-run="${i}" title="${esc(a.tabs.map((t) => `${t.name}: ${t.command}`).join("\n"))}">
            <span class="${on ? "yellow" : "green"}">${on ? "↻" : "▶"}</span><span class="grow">${esc(a.name)}</span><span class="muted">${a.tabs.length} tab${a.tabs.length === 1 ? "" : "s"}</span></button>
          ${on ? `<button class="am-stop" type="button" data-stop="${i}" title="Stop: close its tabs">■</button>` : ""}</div>`;
      })
      .join("") +
    `<button class="am-item am-edit" type="button" data-edit="1"><span class="muted">✎</span><span class="grow muted">Edit automations…</span></button>`;
  menu.dataset.path = path;
  menu.classList.remove("hidden");
  const r = anchor.getBoundingClientRect();
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.max(8, Math.min(window.innerWidth - w - 8, r.left)) + "px";
  menu.style.top = (r.bottom + h + 8 > window.innerHeight ? r.top - h - 4 : r.bottom + 4) + "px";
}

function hideAutomationMenu() {
  $("#auto-menu").classList.add("hidden");
}

function wireAutomations() {
  const menu = $("#auto-menu");
  menu.addEventListener("click", (e) => {
    const p = project(menu.dataset.path);
    const run = e.target.closest("[data-run]"), stop = e.target.closest("[data-stop]"), edit = e.target.closest("[data-edit]");
    hideAutomationMenu();
    if (!p) return;
    if (run) runAutomation(p.path, p.automations[Number(run.dataset.run)]);
    else if (stop) stopAutomation(p.path, p.automations[Number(stop.dataset.stop)]);
    else if (edit) openProjectDialog(p);
  });
  window.addEventListener("mousedown", (e) => {
    if (!e.target.closest("#auto-menu, [data-act='auto'], #btn-auto")) hideAutomationMenu();
  }, true);
  window.addEventListener("keydown", (e) => e.key === "Escape" && hideAutomationMenu(), true);
  $("#btn-auto").addEventListener("click", (e) => {
    const path = state.current || state.selected;
    if ($("#auto-menu").classList.contains("hidden")) showAutomationMenu(path, e.currentTarget);
    else hideAutomationMenu();
  });
}

// ---------- add / edit project dialog ----------

const dlg = { open: false, oldPath: "", autoName: true, icon: "", foundFor: "", autos: [] };

// ---- automations in the dialog ----

const DAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
const LAYOUTS = [["grid", "grid"], ["columns", "side by side"], ["rows", "stacked"], ["tabs", "separate tabs"]];

// What the dialog edits: each automation as written in config.json.
function autosForDialog(p) {
  return ((p && p.automations) || []).map((a) => ({
    name: a.name, layout: a.layout, auto: !!a.auto, cols: a.cols || null, rows: a.rows || null,
    at: (a.schedule && a.schedule.at) || "", days: (a.schedule && a.schedule.days) || [],
    was: `${a.layout}/${a.tabs.length}`, // sizes are kept only while the grid stays the same
    tabs: a.tabs.map((t) => ({ name: t.rawName || "", dir: t.rawDir || "", command: t.command || "" })),
  }));
}

function renderAutos() {
  const opt = (a) => LAYOUTS.map(([v, l]) => `<option value="${v}"${a.layout === v ? " selected" : ""}>${l}</option>`).join("");
  $("#pd-autos").innerHTML = dlg.autos
    .map((a, i) => `<div class="auto-card" data-a="${i}">
      <div class="ac-head">
        <input type="text" class="ac-name" data-f="name" value="${esc(a.name)}" placeholder="name, e.g. Start dev" spellcheck="false">
        <select data-f="layout" title="How the terminals are arranged">${opt(a)}</select>
        <label class="ac-check" title="Run it by itself the first time you open the project"><input type="checkbox" data-f="auto"${a.auto ? " checked" : ""}> run on open</label>
        <button class="ac-del" type="button" data-del-auto title="Delete this automation">✕</button>
      </div>
      <div class="ac-tabs">${a.tabs
        .map((t, j) => `<div class="ac-tab" data-t="${j}">
          <input type="text" class="t-name" data-f="name" value="${esc(t.name)}" placeholder="name" spellcheck="false">
          <input type="text" class="t-dir" data-f="dir" value="${esc(t.dir)}" placeholder="folder (in project)" spellcheck="false">
          <input type="text" class="t-cmd" data-f="command" value="${esc(t.command)}" placeholder="command, e.g. ./run.sh" spellcheck="false">
          <button class="ac-del" type="button" data-del-tab title="Remove this terminal">✕</button></div>`)
        .join("")}</div>
      <div class="ac-foot"><button class="btn" type="button" data-add-tab>＋ terminal</button><span class="muted">empty command = plain shell · folder is relative to the project</span></div>
      ${on("schedules") || a.at ? `<div class="ac-sched"><span class="muted">⏰ run by itself at</span>
        <input type="time" data-f="at" value="${esc(a.at)}">
        <span class="ac-days">${DAYS.map((d) => `<button type="button" class="chip${!a.days.length || a.days.includes(d) ? " on" : ""}" data-day="${d}">${d}</button>`).join("")}</span>
        ${a.at ? '<button type="button" class="ac-del" data-clear-at title="No schedule">✕</button>' : ""}</div>` : ""}
    </div>`)
    .join("");
}

function wireAutosEditor() {
  const box = $("#pd-autos");
  const update = (e) => {
    const card = e.target.closest("[data-a]"), f = e.target.dataset.f;
    if (!card || !f) return;
    const a = dlg.autos[Number(card.dataset.a)];
    const row = e.target.closest("[data-t]");
    if (row) a.tabs[Number(row.dataset.t)][f] = e.target.value;
    else a[f] = e.target.type === "checkbox" ? e.target.checked : e.target.value;
  };
  box.addEventListener("input", update);
  box.addEventListener("change", update);
  box.addEventListener("click", (e) => {
    const card = e.target.closest("[data-a]");
    if (!card) return;
    const i = Number(card.dataset.a), a = dlg.autos[i];
    if (e.target.closest("[data-del-auto]")) dlg.autos.splice(i, 1);
    else if (e.target.closest("[data-del-tab]")) a.tabs.splice(Number(e.target.closest("[data-t]").dataset.t), 1);
    else if (e.target.closest("[data-add-tab]")) a.tabs.push({ name: "", dir: "", command: "" });
    else if (e.target.closest("[data-clear-at]")) a.at = "";
    else if (e.target.closest("[data-day]")) {
      const d = e.target.closest("[data-day]").dataset.day;
      const set = new Set(a.days.length ? a.days : DAYS);
      set.has(d) ? set.delete(d) : set.add(d);
      a.days = set.size === DAYS.length ? [] : DAYS.filter((x) => set.has(x));
      if (!set.size) a.days = []; // never "no day": back to every day
    } else return;
    renderAutos();
    if (e.target.closest("[data-add-tab]")) box.querySelector(`[data-a="${i}"] .ac-tab:last-child .t-name`)?.focus();
  });
  $("#pd-auto-add").addEventListener("click", () => {
    dlg.autos.push({ name: "", layout: "grid", auto: false, cols: null, rows: null, at: "", days: [], was: "", tabs: [{ name: "", dir: "", command: "" }] });
    renderAutos();
    box.querySelector(".auto-card:last-child .ac-name")?.focus();
  });
}

// The automations to save; pane sizes are dropped when the grid changed.
function autosToSave() {
  return dlg.autos.map((a) => {
    const tabs = a.tabs.filter((t) => t.name.trim() || t.dir.trim() || t.command.trim());
    const same = a.was === `${a.layout}/${tabs.length}`;
    return { name: a.name, layout: a.layout, auto: a.auto, tabs, cols: same ? a.cols : null, rows: same ? a.rows : null, schedule: a.at ? { at: a.at, days: a.days } : null };
  });
}

function renderIconField() {
  const prev = $("#pd-icon-preview");
  const name = $("#pd-name").value.trim() || baseName($("#pd-path").value.trim()) || "?";
  if (dlg.icon) {
    loadIcon(dlg.icon, renderIconField);
    prev.innerHTML = iconData[dlg.icon] ? `<img class="picon" src="${iconData[dlg.icon]}" alt="">` : iconData[dlg.icon] === null ? '<span class="red">✕</span>' : "";
  } else prev.innerHTML = monogram(name);
  $("#pd-icon-clear").classList.toggle("hidden", !dlg.icon);
  document.querySelectorAll("#pd-icon-found button").forEach((b) => b.classList.toggle("on", b.dataset.icon === dlg.icon));
}

// Suggest favicons / app icons found in the chosen folder.
async function findIcons() {
  const path = $("#pd-path").value.trim();
  if (!path || path === dlg.foundFor) return;
  dlg.foundFor = path;
  const box = $("#pd-icon-found");
  box.innerHTML = "";
  const found = (await Go().FindIcons(path).catch(() => [])) || [];
  if (path !== $("#pd-path").value.trim()) return;
  for (const f of found) loadIcon(f, () => showFound(found));
  showFound(found);
}

function showFound(found) {
  const ready = found.filter((f) => iconData[f]);
  $("#pd-icon-found").innerHTML = ready.length
    ? ready.map((f) => `<button type="button" data-icon="${esc(f)}" title="${esc(f)}"><img src="${iconData[f]}" alt=""></button>`).join("") +
      '<span class="hint">found in the folder — click to use</span>'
    : "";
  renderIconField();
}

function openProjectDialog(p) {
  dlg.open = true;
  dlg.oldPath = p ? p.path : "";
  dlg.autoName = !p;
  $("#pd-title").textContent = p ? "EDIT PROJECT" : "ADD PROJECT";
  $("#pd-save").textContent = p ? "Save" : "Add project";
  $("#pd-remove").classList.toggle("hidden", !p);
  $("#pd-path").value = p ? p.path : "";
  $("#pd-name").value = p ? p.name : "";
  $("#pd-group").value = p ? p.group : "";
  $("#pd-yolo").value = p ? p.yolo || "" : "";
  $("#pd-yolo").disabled = REMOTE; // only changed on the Mac
  $("#pd-error").textContent = "";
  dlg.icon = p ? p.icon || "" : "";
  dlg.autos = autosForDialog(p);
  renderAutos();
  dlg.foundFor = "";
  $("#pd-icon-found").innerHTML = "";
  renderIconField();
  if (p) findIcons();
  renderGroupChips();
  $("#proj-dialog").classList.remove("hidden");
  (p ? $("#pd-name") : $("#pd-path")).focus();
  if (!p) browseFolder(); // adding starts with the folder picker
}

function closeProjectDialog() {
  dlg.open = false;
  $("#proj-dialog").classList.add("hidden");
  if (state.focus === "terminal") activeTab()?.term.focus();
}

function renderGroupChips() {
  const groups = [...new Set(state.projects.map((p) => p.group).filter(Boolean))];
  const cur = $("#pd-group").value.trim();
  $("#pd-groups").innerHTML = groups
    .map((g) => `<button class="chip${g === cur ? " on" : ""}" type="button" data-group="${esc(g)}">${esc(g)}</button>`)
    .join("");
}

const baseName = (path) => path.replace(/\/+$/, "").split("/").pop() || "";

async function browseFolder() {
  try {
    const dir = await Go().ChooseFolder($("#pd-path").value.trim());
    if (!dir) return;
    $("#pd-path").value = dir;
    if (dlg.autoName || !$("#pd-name").value.trim()) $("#pd-name").value = baseName(dir);
    findIcons();
    renderIconField();
    $("#pd-error").textContent = "";
    $("#pd-name").focus();
    $("#pd-name").select();
  } catch (e) {
    $("#pd-error").textContent = String(e);
  }
}

async function saveProjectDialog() {
  try {
    const full = await Go().SaveProject(dlg.oldPath, $("#pd-name").value, $("#pd-path").value, $("#pd-group").value, dlg.icon, autosToSave(), $("#pd-yolo").value);
    const was = dlg.oldPath;
    closeProjectDialog();
    // An edited path moves its open tabs along with it.
    if (was && was !== full && state.tabs[was]) {
      state.tabs[full] = state.tabs[was];
      state.active[full] = state.active[was];
      delete state.tabs[was];
      for (const t of state.tabs[full]) t.path = full;
      if (state.current === was) state.current = full;
    }
    await loadProjects();
    state.selected = full;
    render();
    flash(was ? "project saved" : `added ${baseName(full)}`);
  } catch (e) {
    $("#pd-error").textContent = String(e);
  }
}

async function removeProjectDialog() {
  const p = project(dlg.oldPath);
  if (!p) return closeProjectDialog();
  const open = tabsOf(p.path).filter((t) => !t.exited).length;
  const msg = `${p.name} will be removed from the list (the folder itself is not touched).` + (open ? ` Its ${open} open tab(s) will be closed.` : "");
  if (!(await Go().Confirm("Remove project?", msg, "Remove"))) return;
  try {
    await Go().RemoveProject(p.path);
    for (const t of [...tabsOf(p.path)]) closeTab(t, true);
    if (state.current === p.path) state.current = null;
    closeProjectDialog();
    await loadProjects();
    showActive();
    render();
    flash(`removed ${p.name}`);
  } catch (e) {
    $("#pd-error").textContent = String(e);
  }
}

function wireProjectDialog() {
  wireAutosEditor();
  $("#proj-form").addEventListener("submit", (e) => {
    e.preventDefault();
    saveProjectDialog();
  });
  $("#pd-browse").addEventListener("click", browseFolder);
  $("#pd-cancel").addEventListener("click", closeProjectDialog);
  $("#pd-remove").addEventListener("click", removeProjectDialog);
  $("#pd-name").addEventListener("input", () => {
    dlg.autoName = !$("#pd-name").value.trim();
    renderIconField();
  });
  $("#pd-path").addEventListener("change", findIcons);
  $("#pd-icon-choose").addEventListener("click", async () => {
    try {
      const f = await Go().ChooseIcon($("#pd-path").value.trim());
      if (f) {
        dlg.icon = f;
        renderIconField();
      }
    } catch (e) {
      $("#pd-error").textContent = String(e);
    }
  });
  $("#pd-icon-clear").addEventListener("click", () => {
    dlg.icon = "";
    renderIconField();
  });
  $("#pd-icon-found").addEventListener("click", (e) => {
    const b = e.target.closest("[data-icon]");
    if (!b) return;
    dlg.icon = dlg.icon === b.dataset.icon ? "" : b.dataset.icon;
    renderIconField();
  });
  $("#pd-group").addEventListener("input", renderGroupChips);
  $("#pd-groups").addEventListener("click", (e) => {
    const g = e.target.closest("[data-group]")?.dataset.group;
    if (g == null) return;
    $("#pd-group").value = $("#pd-group").value.trim() === g ? "" : g;
    renderGroupChips();
  });
  $("#proj-dialog").addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeProjectDialog();
  });
  $("#proj-dialog").addEventListener("mousedown", (e) => {
    if (e.target.id === "proj-dialog") closeProjectDialog();
  });
}

// ---------- layout + resizing ----------

function applyLayout() {
  const main = $("#main");
  const r = document.documentElement.style;
  r.setProperty("--left", state.left + "px");
  r.setProperty("--right", state.right + "px");
  main.classList.toggle("no-left", !state.showLeft);
  main.classList.toggle("no-right", !state.showRight);
  fitActive();
}

let fitPending = false;
function fitActive() {
  if (fitPending) return;
  fitPending = true;
  requestAnimationFrame(() => {
    fitPending = false;
    for (const el of document.querySelectorAll(".term.active")) {
      const t = Object.values(state.byId).find((x) => x.el === el) || activeTab();
      try {
        fitTab(t);
      } catch {}
    }
    if (!document.body.classList.contains("dragging")) placeSplitHandles();
  });
}

function wireResize() {
  const limits = { left: [200, 560], right: [260, 600] };
  for (const side of ["left", "right"]) {
    const handle = $(`#handle-${side}`);
    handle.addEventListener("mousedown", (e) => {
      e.preventDefault();
      document.body.classList.add("dragging");
      handle.classList.add("active");
      const startX = e.clientX;
      const start = state[side];
      const move = (ev) => {
        const dx = ev.clientX - startX;
        const [lo, hi] = limits[side];
        state[side] = Math.round(Math.max(lo, Math.min(hi, side === "left" ? start + dx : start - dx)));
        applyLayout();
      };
      const up = () => {
        window.removeEventListener("mousemove", move);
        window.removeEventListener("mouseup", up);
        document.body.classList.remove("dragging");
        handle.classList.remove("active");
        Go().SaveLayout(state.left, state.right);
      };
      window.addEventListener("mousemove", move);
      window.addEventListener("mouseup", up);
    });
    handle.addEventListener("dblclick", () => {
      state[side] = side === "left" ? DEFAULT_LEFT : DEFAULT_RIGHT;
      applyLayout();
      Go().SaveLayout(state.left, state.right);
    });
  }
  new ResizeObserver(fitActive).observe($("#terms"));
}

// ---------- rendering ----------

function render() {
  renderTitle();
  renderProjects();
  renderTabs();
  renderActiveList();
}

// renderActiveList shows, on the welcome screen, the projects with open
// sessions: working ones first, then those waiting for you.
function renderActiveList() {
  const el = $("#active-list");
  const rank = { busy: 0, waiting: 1 };
  const live = orderedProjects()
    .map((p) => ({ p, st: projectStatus(p.path), n: tabsOf(p.path).filter((t) => !t.exited).length }))
    .filter((x) => x.st !== "idle")
    .sort((a, b) => rank[a.st] - rank[b.st]);
  const html = live.length
    ? `<div class="al-head muted">Active now · ${live.length}</div>` +
      live.map(({ p, st, n }) => `<button type="button" class="al-row" data-open="${esc(p.path)}">${iconHTML(p)}<span class="al-name">${esc(p.name)}</span>
        <span class="al-st">${STATUS_ICON[st]} ${st === "busy" ? "working" : "waiting for you"}${n > 1 ? ` · ${n} tabs` : ""}</span></button>`).join("")
    : "";
  if (el.dataset.html === html) return;
  el.dataset.html = html;
  el.innerHTML = html;
  el.classList.toggle("hidden", !live.length);
}

function renderTitle() {
  const path = state.current || state.selected;
  const p = project(path);
  $("#tb-name").textContent = p ? p.name : "";
  const ic = p ? iconHTML(p) : "";
  if ($("#tb-icon").dataset.html !== ic) {
    $("#tb-icon").innerHTML = ic;
    $("#tb-icon").dataset.html = ic;
  }
  $("#tb-path").textContent = p ? p.shortPath : "";
  let busy = 0, waiting = 0;
  for (const list of Object.values(state.tabs))
    for (const t of list) {
      const s = tabStatus(t);
      if (s === "busy") busy++;
      else if (s === "waiting") waiting++;
    }
  const r = $("#tb-running"), w = $("#tb-waiting");
  r.classList.toggle("hidden", !busy);
  w.classList.toggle("hidden", !waiting);
  r.lastElementChild.textContent = busy;
  w.lastElementChild.textContent = waiting;
  const now = new Date();
  $("#tb-clock").textContent = `${now.toLocaleDateString([], { weekday: "short", day: "numeric", month: "short" })} · ${now.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", hour12: true })}`;
}

// Projects in list order: each group gathered together, groups in the order they first appear in config.json.
function orderedProjects() {
  const first = new Map();
  state.projects.forEach((p, i) => first.has(p.group || "") || first.set(p.group || "", i));
  return state.projects
    .map((p, i) => ({ p, i }))
    .sort((a, b) => first.get(a.p.group || "") - first.get(b.p.group || "") || a.i - b.i)
    .map((x) => x.p);
}

function visibleProjects() {
  const q = $("#filter").value.trim();
  if (!q) return orderedProjects().map((p) => ({ p, pos: [] }));
  return orderedProjects()
    .map((p) => ({ p, m: fuzzy(q, p.name) || (q.includes("/") ? fuzzy(q, p.path) : null) }))
    .filter((x) => x.m)
    .map((x) => ({ p: x.p, pos: x.m.pos || [] }));
}

let lastProjectsHTML = "";
function renderProjects() {
  const vis = visibleProjects();
  $("#project-count").textContent = state.projects.length;
  let html = "";
  if (!state.projects.length) {
    html = `<div class="empty">No projects yet.<br><br>Press <b>⌘O</b> to add a folder, or <b>⌘,</b> to edit<br><span class="soft">${esc("~/.config/quarters/config.json")}</span></div>`;
  } else if (!vis.length) {
    html = `<div class="empty">No project matches.</div>`;
  }
  let group = null;
  const grouped = state.projects.some((p) => p.group);
  const filtering = !!$("#filter").value.trim();
  for (const { p, pos } of vis) {
    const g = p.group || "Other";
    if (grouped && g !== group) {
      group = g;
      const members = state.projects.filter((x) => (x.group || "Other") === g);
      const folded = !filtering && state.collapsed.has(g);
      const sts = members.map((x) => projectStatus(x.path));
      const gst = sts.includes("busy") ? STATUS_ICON.busy : sts.includes("waiting") ? STATUS_ICON.waiting : "";
      html += `<button class="group${folded ? " folded" : ""}" type="button" data-group="${esc(g)}" data-raw="${esc(p.group || "")}" title="${folded ? "Expand" : "Collapse"}">
        <span class="chev">▾</span>${esc(g)}${folded && gst ? ` <span class="gstat">${gst}</span>` : ""}<span class="gcount">${members.length}</span></button>`;
    }
    if (grouped && !filtering && state.collapsed.has(g)) continue;
    const sel = p.path === state.selected;
    const st = projectStatus(p.path);
    const n = tabsOf(p.path).length;
    const gitBits = p.git ? `${p.dirty ? ` <span class="yellow" title="${p.dirty} changed file${p.dirty === 1 ? "" : "s"}">●${p.dirty}</span>` : ""}${p.ahead ? ` <span class="green" title="${p.ahead} commit(s) to push">↑${p.ahead}</span>` : ""}${p.behind ? ` <span class="red" title="${p.behind} commit(s) to pull">↓${p.behind}</span>` : ""}` : "";
    const branch = p.missing ? "folder missing" : p.branch ? `⎇ ${esc(p.branch)}${gitBits}` : esc(p.shortPath);
    const attn = tabsOf(p.path).some((t) => t.unseen);
    html += `<div class="proj${sel ? " selected" : ""}${attn ? " attn" : ""}${p.path === state.current ? " current" : ""}${p.missing ? " missing" : ""}" data-path="${esc(p.path)}" data-raw="${esc(p.group || "")}"${filtering ? "" : ' draggable="true"'} role="button" tabindex="-1" title="${esc(p.path)}">
      <span class="l1"><span class="name"><span class="marker">${sel ? "▸" : ""}</span>${iconHTML(p)}<span class="nm">${pos.length ? highlight(p.name, pos) : esc(p.name)}</span></span>
        <span class="status">${(p.automations || []).some((a) => autoRunning(p.path, a)) ? '<span class="tabs-count green" title="An automation is running">▶</span>' : ""}${n > 1 ? `<span class="tabs-count">${n}×</span>` : ""}${STATUS_ICON[st]}</span></span>
      <span class="l2"><span class="branch">${branch}</span><span>${fmtAgo(p.lastActive)}</span></span>
      <span class="actions">
        ${(p.automations || []).length ? `<button class="act act-auto" type="button" data-act="auto" title="Automations">▶</button>` : ""}
        <button class="act" type="button" data-act="claude" title="New Claude tab">+</button>
        <button class="act" type="button" data-act="continue" title="Continue last conversation">↻</button>
        <button class="act" type="button" data-act="shell" title="Shell tab">›_</button>
        <button class="act" type="button" data-act="reveal" title="Show in Finder">⌕</button>
        <button class="act" type="button" data-act="edit" title="Edit name, folder or group">✎</button>
      </span>
    </div>`;
  }
  if (html !== lastProjectsHTML && !projDrag.path) {
    lastProjectsHTML = html;
    $("#project-list").innerHTML = html;
  }
}

function renderTabs() {
  if ($("#tab-list .tab-rename")) return; // don't wipe a rename in progress
  const cur = activeTab();
  const label = (t) => (t.wt ? `⎇ ${t.wt} · ` : "") + (t.customTitle || t.fixedTitle || (t.kind === "shell" ? t.title || "shell" : t.title || (t.kind === "continue" || t.kind === "resume" ? "claude ↻" : "claude")));
  const html = entries(state.current)
    .map((e, k) => {
      const on = e.tabs.includes(cur);
      if (e.group) {
        const st = e.tabs.some((t) => tabStatus(t) === "busy") ? "busy" : e.tabs.some((t) => !t.exited) ? "waiting" : "ended";
        return `<div class="tab${on ? " active" : ""}${e.tabs.some((t) => t.unseen) ? " attn" : ""}" data-e="${k}" role="button" title="${esc(e.group.name)}: ${esc(e.tabs.map(label).join(", "))}">
          <span class="muted">${k + 1}</span><span class="green">▦</span><span class="label">${esc(e.group.label || e.group.name)}</span><span class="muted">${e.tabs.length}</span>${STATUS_ICON[st]}
          <button class="close" type="button" data-close="${k}" aria-label="Close all panes">×</button></div>`;
      }
      const t = e.tabs[0];
      return `<div class="tab${on ? " active" : ""}${t.unseen ? " attn" : ""}" data-e="${k}" role="button" title="${esc(label(t))}">
        <span class="muted">${k + 1}</span><span class="label">${esc(label(t))}</span>${STATUS_ICON[tabStatus(t)]}
        <button class="close" type="button" data-close="${k}" aria-label="Close tab">×</button></div>`;
    })
    .join("");
  // Only touch the DOM when something changed, so double-clicks land on the same element.
  if (html !== $("#tab-list").dataset.html) {
    $("#tab-list").innerHTML = html;
    $("#tab-list").dataset.html = html;
  }
  // pane headers of split automations show each shell's status
  for (const t of tabsOf(state.current)) if (t.head) t.head.querySelector(".pane-status").innerHTML = STATUS_ICON[tabStatus(t)];
  // on a phone a split automation shows one pane at a time; these switch between them
  const panes = isSplit(cur) ? tabsOf(cur.path).filter((t) => t.group === cur.group) : [];
  const pb = panes.length > 1
    ? panes.map((t) => `<button type="button" class="${t === cur ? "on" : ""}" data-pane="${tabsOf(cur.path).indexOf(t)}">${STATUS_ICON[tabStatus(t)]} ${esc(t.fixedTitle || t.title || "shell")}</button>`).join("")
    : "";
  if ($("#pane-bar").dataset.html !== pb) {
    $("#pane-bar").innerHTML = pb;
    $("#pane-bar").dataset.html = pb;
  }
  const p = project(state.current);
  // the marks keep the path in order while the right-to-left box cuts it from the start
  $("#tab-path").textContent = p ? `\u200E${p.shortPath}\u200E` : "";
  $("#btn-new-tab").classList.toggle("hidden", !state.current && !state.selected);
  $("#btn-new-shell").classList.toggle("hidden", !state.current && !state.selected);
  const ap = project(state.current || state.selected);
  $("#btn-auto").classList.toggle("hidden", !(ap && (ap.automations || []).length));
  $("#btn-auto").classList.toggle("running", !!(ap && (ap.automations || []).some((a) => autoRunning(ap.path, a))));
  $("#btn-prompts").classList.toggle("hidden", !on("prompts") || !cur || cur.kind === "shell");
  $("#btn-history").classList.toggle("hidden", !on("history") || !ap);
  $("#btn-wt").classList.toggle("hidden", !on("worktrees") || !ap || !ap.git);
}

function renderStats() {
  const s = state.stats;
  const path = state.statsPath;
  const el = $("#stats");
  const plan = renderPlan();
  if (!path || !s) {
    el.dataset.html = "";
    el.innerHTML = plan + `<div class="sec-row"><span class="panel-title">SESSION</span></div><div class="muted">Select a project.</div>`;
    return;
  }
  const live = tabsOf(path).some((t) => !t.exited);
  const state_ = live ? '<span class="green">● live</span>' : s.has ? `<span class="muted">last ${fmtAgo(s.last)}</span>` : '<span class="muted">○ none yet</span>';
  let h = plan + `<div class="sec-row"><span class="panel-title">SESSION</span>${state_}</div>`;

  if (s.has) {
    if (s.title) h += `<div class="title-line" title="${esc(s.title)}">${esc(s.title)}</div>`;
    const end = live ? Date.now() : s.last;
    const frac = s.window ? s.context / s.window : 0;
    h += `<div class="kv">
      <div class="row"><span class="muted">model</span><span>${esc(prettyModel(s.model))}</span></div>
      <div class="row"><span class="muted">${end - s.start > 86400000 ? "started" : "elapsed"}</span><span>${end - s.start > 86400000 ? fmtAgo(s.start) + " ago" : fmtDuration(end - s.start)}</span></div>
      <div class="row"><span class="muted">mode</span><span class="violet">${esc(MODES[s.mode] ?? s.mode)}</span></div>
      <div class="row"><span class="muted">prompts</span><span>${s.turns}</span></div></div>
      <div class="kv" style="gap:6px">
      <div class="row"><span class="muted">context</span><span>${fmtTokens(s.context)} / ${fmtTokens(s.window)}  <span class="accent">${Math.round(frac * 100)}%</span></span></div>
      <div class="meter${frac > 0.8 ? " hot" : ""}"><div style="width:${Math.min(100, frac * 100).toFixed(1)}%"></div></div></div>`;
  } else {
    h += `<div class="muted">No Claude Code history in this folder yet.</div>`;
  }

  // tokens
  const u = state.tokenView === "today" ? s.today : s.usage;
  const prompt = (u.Input || 0) + (u.CacheRead || 0) + (u.CacheWrite || 0);
  const hit = prompt ? Math.round((100 * (u.CacheRead || 0)) / prompt) : 0;
  h += `<div class="divider"></div>
    <div class="sec-row"><span class="sec">TOKENS</span>
      <span class="seg"><button type="button" data-view="session" class="${state.tokenView === "session" ? "on" : ""}">session</button><button type="button" data-view="today" class="${state.tokenView === "today" ? "on" : ""}">today</button></span></div>
    <div class="tiles">
      <div class="tile"><div class="t-label">input</div><div class="t-value">${fmtTokens(u.Input)}</div></div>
      <div class="tile"><div class="t-label">output</div><div class="t-value">${fmtTokens(u.Output)}</div></div>
      <div class="tile"><div class="t-label">cache read</div><div class="t-value">${fmtTokens(u.CacheRead)}</div></div>
      <div class="tile"><div class="t-label">cache hit</div><div class="t-value green">${hit}%</div></div>
    </div>`;
  if (state.tokenView === "today") h += `<div class="muted" style="margin-top:-6px;font-size:12px">${s.todaySessions} sessions today, all projects</div>`;

  // activity
  const act = s.activity || [];
  const peak = Math.max(1, ...act);
  h += `<div class="kv" style="gap:6px"><div class="row" style="font-size:12px"><span class="muted">tokens / 5 min</span><span class="muted">last 2h</span></div>
    <div class="chart">${act
      .map((v, i) => {
        const cls = !v ? "zero" : i === act.length - 1 ? "now" : "";
        const hgt = v ? Math.max(4, Math.round((44 * v) / peak)) : 2;
        return `<div class="${cls}" style="height:${hgt}px" title="${fmtTokens(v)} tokens"></div>`;
      })
      .join("")}</div></div>`;

  // git
  const g = s.git;
  if (g && g.IsRepo) {
    const sync = g.Ahead || g.Behind ? ` <span class="muted">↑${g.Ahead} ↓${g.Behind}</span>` : "";
    h += `<div class="divider"></div><div class="sec-row"><span class="sec">GIT</span><span><span class="violet">⎇ ${esc(g.Branch)}</span>${sync}</span></div><div class="files">`;
    const files = g.Files || [];
    if (!files.length) h += `<div class="green">✓ working tree clean</div>`;
    for (const f of files.slice(0, 8)) {
      const col = f.Status === "A" || f.Status === "??" ? "green" : f.Status === "D" ? "red" : "yellow";
      const diff = `${f.Added ? `<span class="green">+${f.Added}</span>` : ""}${f.Del ? ` <span class="red">−${f.Del}</span>` : ""}`;
      const st = f.Status === "??" ? "U" : f.Status;
      const cut = f.Path.replace(/\/$/, "").lastIndexOf("/");
      const dir = cut >= 0 ? f.Path.slice(0, cut + 1) : "", base = f.Path.slice(cut + 1);
      h += `<button type="button" class="row file-row" data-diff="${esc(f.Path)}"><span class="path" title="${esc(f.Path)} — show changes"><span class="${col} st">${esc(st)}</span><span class="muted">${esc(dir)}</span>${esc(base)}</span><span>${diff}</span></button>`;
    }
    if (files.length > 8) h += `<div class="muted">+ ${files.length - 8} more</div>`;
    h += `</div>`;
  }

  // files Claude edited this session
  const edited = (s.edited || []).filter((f) => f.startsWith(path + "/")).map((f) => f.slice(path.length + 1));
  if (on("changes") && edited.length) {
    h += `<div class="divider"></div><div class="sec-row"><span class="sec">CLAUDE CHANGED</span><span class="muted" style="font-size:12px">${edited.length} file${edited.length === 1 ? "" : "s"} · tap for diff</span></div><div class="files">`;
    for (const f of edited.slice(-12).reverse()) {
      const g = ((s.git && s.git.Files) || []).find((x) => x.Path === f);
      const cut = f.lastIndexOf("/");
      const diff = g ? `${g.Added ? `<span class="green">+${g.Added}</span>` : ""}${g.Del ? ` <span class="red">−${g.Del}</span>` : ""}${!g.Added && !g.Del ? `<span class="${g.Status === "??" ? "green" : "yellow"}">${g.Status === "??" ? "new" : esc(g.Status)}</span>` : ""}` : s.git && s.git.IsRepo ? '<span class="muted">committed</span>' : "";
      h += `<button type="button" class="row file-row" data-diff="${esc(f)}"><span class="path" title="${esc(f)}"><span class="violet st">✎</span><span class="muted">${esc(cut >= 0 ? f.slice(0, cut + 1) : "")}</span>${esc(f.slice(cut + 1))}</span><span>${diff}</span></button>`;
    }
    if (edited.length > 12) h += `<div class="muted">+ ${edited.length - 12} earlier</div>`;
    h += `</div>`;
  }

  // local servers the terminals started
  const links = state.links.filter((l) => l.path === path);
  if (on("dev_links") && links.length) {
    h += `<div class="divider"></div><div class="sec-row"><span class="sec">SERVERS</span><span class="muted" style="font-size:12px">${REMOTE ? "tap to open on the Mac" : "click to open"}</span></div><div class="kv" style="gap:4px;font-size:12px">`;
    for (const l of links)
      h += `<button type="button" class="row link-row" data-link="${esc(l.url)}" title="${esc(l.url)} · from ${esc(l.title)}"><span>${l.up ? '<span class="green">●</span>' : '<span class="muted">○</span>'} ${esc(l.url.replace(/^https?:\/\//, ""))}</span><span class="muted">${l.up ? esc(l.title) : "not answering"}</span></button>`;
    h += `</div>`;
  }

  // tools
  if (s.has && s.tools && s.tools.length) {
    const top = s.tools[0].Count || 1;
    h += `<div class="divider"></div><div class="sec-row"><span class="sec">TOOLS</span><span class="muted" style="font-size:12px">calls this session</span></div><div class="kv">`;
    for (const t of s.tools)
      h += `<div class="tool"><div class="row"><span>${esc(t.Name)}</span><span class="soft">${t.Count}</span></div><div class="tool-bar" style="width:${Math.round((100 * t.Count) / top)}%"></div></div>`;
    h += `</div>`;
  }

  // skills and MCP servers (with what each is), when switched on
  const tools = on("skills") ? state.tools[path] : null;
  if (tools) {
    const calls = Object.fromEntries((s.mcp || []).map((m) => [m.name, m.calls]));
    h += `<div class="divider"></div><details class="tools-sec"${state.openSec.mcp ? " open" : ""} data-sec="mcp"><summary class="sec-row"><span class="sec">MCP SERVERS · ${tools.mcp.length}</span><span class="chev">▾</span></summary><div class="kv tools-list">`;
    if (!tools.mcp.length) h += `<div class="muted">None configured.</div>`;
    for (const m of tools.mcp)
      h += `<div class="tool-item"><div class="row"><span><b>${esc(m.name)}</b> <span class="badge-s">${esc(m.scope)}</span></span>${calls[m.name] ? `<span class="green">● ${calls[m.name]} calls</span>` : '<span class="muted">○</span>'}</div><div class="muted ti-sub">${esc(m.type)} · ${esc(m.target || "—")}</div></div>`;
    h += `</div></details>`;
    h += `<details class="tools-sec"${state.openSec.skills ? " open" : ""} data-sec="skills"><summary class="sec-row"><span class="sec">SKILLS · ${tools.skills.length}</span><span class="chev">▾</span></summary><div class="kv tools-list">`;
    if (!tools.skills.length) h += `<div class="muted">No skills.</div>`;
    for (const sk of tools.skills)
      h += `<div class="tool-item" title="${esc(sk.about)}"><div class="row"><span><b>${esc(sk.name)}</b></span><span class="badge-s">${esc(sk.scope)}</span></div>${sk.about ? `<div class="muted ti-sub ti-about">${esc(sk.about)}</div>` : ""}</div>`;
    h += `</div></details>`;
  } else if (s.mcp && s.mcp.length) {
    h += `<div class="divider"></div><div class="sec">MCP SERVERS</div><div class="kv" style="font-size:12px">`;
    for (const m of s.mcp)
      h += `<div class="row"><span>${esc(m.name)}</span>${m.calls ? `<span class="green">● ${m.calls} calls</span>` : '<span class="muted">○ configured</span>'}</div>`;
    h += `</div>`;
  }
  // keep the scroll position and only redraw when something changed
  if (el.dataset.html !== h) {
    el.dataset.html = h;
    el.innerHTML = h;
  }
}

function fmtResets(ms) {
  if (!ms) return "";
  const d = Math.max(0, (ms - Date.now()) / 1000);
  if (d < 3600) return `resets in ${Math.ceil(d / 60)}m`;
  if (d < 86400) return `resets in ${Math.floor(d / 3600)}h ${Math.floor((d % 3600) / 60)}m`;
  return "resets " + new Date(ms).toLocaleDateString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
}

// renderPlan draws the Claude account's plan limits (what /usage shows).
function renderPlan() {
  const a = state.account;
  if (!a) return "";
  let h = `<div class="sec-row"><span class="panel-title">CLAUDE PLAN</span>${a.plan ? `<span class="badge">${esc(a.plan)}</span>` : ""}</div>`;
  if (a.error && !(a.limits || []).length) return h + `<div class="muted">${esc(a.error)}</div><div class="divider"></div>`;
  h += `<div class="kv" style="gap:10px">`;
  for (const l of a.limits || []) {
    const pct = Math.round(l.percent);
    const hot = l.severity === "critical" || pct >= 90 ? "hot" : l.severity === "warning" || pct >= 75 ? "warn" : "";
    h += `<div class="limit">
      <div class="row"><span>${esc(l.label)}${l.active ? ' <span class="muted" title="The limit currently in effect">•</span>' : ""}</span><span class="${hot === "hot" ? "red" : hot ? "yellow" : "accent"}">${pct}%</span></div>
      <div class="meter ${hot}"><div style="width:${Math.min(100, l.percent)}%"></div></div>
      <div class="muted" style="font-size:11px;line-height:16px">${esc(fmtResets(l.resetsAt))}</div></div>`;
  }
  h += `</div>`;
  if (a.error) h += `<div class="muted" style="font-size:11px">${esc(a.error)}</div>`;
  return h + `<div class="divider"></div>`;
}

async function refreshAccount() {
  try {
    state.account = await Go().Account();
    renderStats();
  } catch {}
}

// ---------- mascot ----------

const MOODS = ["thinking", "happy", "sleepy", "sad", "nervous", "excited"];
const QUIET_MS = 3000; // Claude counts as "your turn" only after this long without output, so short pauses don't flicker
let mascotMood = "", cheerUntil = 0;

function preloadMascot() {
  for (const m of MOODS) new Image().src = `mascot/${m}.svg`;
}

function mascotState() {
  if (Date.now() < cheerUntil) return ["excited", "Hi!", "thanks for the click"];
  const tab = activeTab();
  const top = Math.max(0, ...((state.account && state.account.limits) || []).map((l) => l.percent));
  const working = tab && !tab.exited && Date.now() - tab.lastData < QUIET_MS;
  if (top >= 90 && !working) {
    const l = state.account.limits.find((x) => x.percent === top);
    return ["nervous", `Plan at ${Math.round(top)}%`, `${l.label.toLowerCase()} · ${fmtResets(l.resetsAt)}`];
  }
  if (!tab) {
    const live = Object.values(state.tabs).flat().filter((t) => !t.exited).length;
    return ["sleepy", "Napping", live ? `${live} session${live > 1 ? "s" : ""} in other projects` : "no session open"];
  }
  const name = project(tab.path)?.name || "";
  if (tab.exited) return ["sad", "Session ended", tab.code ? `exit code ${tab.code} · restart below` : "restart or close the tab"];
  if (working) return ["thinking", tab.kind === "shell" ? "Busy…" : "Claude is working…", name];
  return ["happy", tab.kind === "shell" ? "Shell ready" : "Your turn", tab.kind === "shell" ? name : `Claude is waiting · ${name}`];
}

function renderMascot() {
  const [mood, title, sub] = mascotState();
  if (mood !== mascotMood) {
    mascotMood = mood;
    $("#mascot-img").src = `mascot/${mood}.svg`;
  }
  $("#mascot-title").textContent = title;
  $("#mascot-sub").textContent = sub;
}

// Double-tap Shift (with nothing typed in between) toggles the project jumper, even inside the terminal.
function wireDoubleShift() {
  let clean = false, last = 0;
  window.addEventListener("keydown", (e) => {
    clean = e.key === "Shift" && !e.repeat;
  }, true);
  window.addEventListener("keyup", (e) => {
    if (e.key !== "Shift" || !clean) return;
    clean = false;
    const now = Date.now();
    if (now - last < 350) {
      last = 0;
      if (dlg.open) return;
      state.palette.open ? closePalette() : openPalette();
    } else last = now;
  }, true);
  window.addEventListener("mousedown", () => (last = 0), true);
}

// ---------- palette ----------

function openPalette() {
  state.palette.open = true;
  state.palette.sel = 0;
  $("#pal-query").value = "";
  $("#palette").classList.remove("hidden");
  renderPalette();
  $("#pal-query").focus();
}

function closePalette() {
  state.palette.open = false;
  $("#palette").classList.add("hidden");
  if (state.focus === "terminal") activeTab()?.term.focus();
}

function renderPalette() {
  const q = $("#pal-query").value.trim();
  const matches = state.projects
    .map((p) => ({ p, m: fuzzy(q, p.name) || (q.includes("/") ? fuzzy(q, p.path) : null) }))
    .filter((x) => x.m);
  // Automations match on "<automation> <project>" (or the other way round) and rank just below a project match.
  for (const p of state.projects)
    for (const a of p.automations || []) {
      const m = q ? fuzzy(q, `${a.name} ${p.name}`) || fuzzy(q, `${p.name} ${a.name}`) : { score: 0, pos: [] };
      if (m) matches.push({ p, a, m: { score: m.score - 1, pos: [] } });
    }
  matches.sort((x, y) => y.m.score - x.m.score);
  state.palette.matches = matches;
  state.palette.sel = Math.min(state.palette.sel, Math.max(0, matches.length - 1));
  $("#pal-count").textContent = `${matches.length} of ${state.projects.length}`;
  $("#pal-list").innerHTML =
    matches
      .map(({ p, m, a }, i) => {
        if (a) {
          const on = autoRunning(p.path, a);
          return `<button class="pal-item${i === state.palette.sel ? " sel" : ""}" type="button" data-i="${i}">
          <span class="marker">${i === state.palette.sel ? "▸" : ""}</span><span class="${on ? "yellow" : "green"}">${on ? "↻" : "▶"}</span>
          <span class="grow"><span class="nm">${esc(a.name)}</span> <span class="muted">${esc(p.name)} · ${a.tabs.length} tab${a.tabs.length === 1 ? "" : "s"}</span></span>
          <span class="meta">${on ? '<span class="yellow">running · ↵ restart</span>' : "automation"}</span></button>`;
        }
        const st = projectStatus(p.path);
        const label = { busy: '<span class="green">working</span>', waiting: '<span class="yellow">waiting for input</span>', idle: fmtAgo(p.lastActive) }[st];
        return `<button class="pal-item${i === state.palette.sel ? " sel" : ""}" type="button" data-i="${i}">
        <span class="marker">${i === state.palette.sel ? "▸" : ""}</span>${STATUS_ICON[st]}${iconHTML(p)}
        <span class="grow"><span class="nm">${highlight(p.name, m.pos || [])}</span> <span class="muted">${esc(p.shortPath)}</span></span>
        <span class="meta">${label}</span></button>`;
      })
      .join("") || `<div class="empty">No matching project.</div>`;
  $("#pal-list .sel")?.scrollIntoView({ block: "nearest" });
}

function pickPalette(i, newTabToo) {
  const m = state.palette.matches[i];
  closePalette();
  if (!m) return;
  if (m.a) return runAutomation(m.p.path, m.a);
  if (newTabToo) newTab(m.p.path, "claude");
  else openProject(m.p.path);
}

function wirePalette() {
  $("#pal-query").addEventListener("input", () => {
    state.palette.sel = 0;
    renderPalette();
  });
  $("#pal-query").addEventListener("keydown", (e) => {
    const n = state.palette.matches.length;
    if (e.key === "Escape") closePalette();
    else if (e.key === "ArrowDown" || (e.ctrlKey && (e.key === "n" || e.key === "j"))) state.palette.sel = Math.min(n - 1, state.palette.sel + 1);
    else if (e.key === "ArrowUp" || (e.ctrlKey && (e.key === "p" || e.key === "k"))) state.palette.sel = Math.max(0, state.palette.sel - 1);
    else if (e.key === "Enter") {
      // Handled here; the projects list must not also act on this Enter.
      e.preventDefault();
      e.stopPropagation();
      return pickPalette(state.palette.sel, e.metaKey);
    } else return;
    e.preventDefault();
    renderPalette();
  });
  $("#pal-list").addEventListener("click", (e) => {
    const item = e.target.closest(".pal-item");
    if (item) pickPalette(Number(item.dataset.i), e.metaKey);
  });
  $("#palette").addEventListener("mousedown", (e) => {
    if (e.target.id === "palette") closePalette();
  });
  document.querySelectorAll(".pal-action").forEach((b) =>
    b.addEventListener("click", () => {
      closePalette();
      onMenu(b.dataset.action);
    }),
  );
}

// ---------- projects panel input ----------

function wireProjects() {
  $("#project-list").addEventListener("click", (e) => {
    const grp = e.target.closest(".group");
    if (grp) {
      const g = grp.dataset.group;
      state.collapsed.has(g) ? state.collapsed.delete(g) : state.collapsed.add(g);
      renderProjects();
      Go().SetCollapsed([...state.collapsed]);
      return;
    }
    const row = e.target.closest(".proj");
    if (!row) return;
    const path = row.dataset.path;
    const act = e.target.closest(".act")?.dataset.act;
    setFocus("projects");
    if (act === "reveal") return Go().RevealProject(path);
    if (act === "edit") return openProjectDialog(project(path));
    if (act === "auto") return showAutomationMenu(path, e.target.closest(".act"));
    if (act) {
      closeDrawers();
      return newTab(path, act);
    }
    openProject(path);
  });
  $("#project-list").addEventListener("contextmenu", (e) => {
    const row = e.target.closest(".proj");
    if (!row) return;
    e.preventDefault();
    openProjectDialog(project(row.dataset.path));
  });
  $("#left").addEventListener("mousedown", (e) => {
    if (!e.target.closest("input")) setFocus("projects");
  });
  $("#filter").addEventListener("input", () => {
    const vis = visibleProjects();
    if (vis.length && !vis.some((v) => v.p.path === state.selected)) state.selected = vis[0].p.path;
    renderProjects();
  });
  $("#filter").addEventListener("focus", () => setFocus("projects"));
  $("#filter").addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      $("#filter").value = "";
      $("#filter").blur();
      renderProjects();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (state.selected) openProject(state.selected);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      moveSelection(e.key === "ArrowDown" ? 1 : -1);
    }
  });
  // Keyboard navigation while the projects panel has focus.
  window.addEventListener("keydown", (e) => {
    if (e.defaultPrevented || state.palette.open || dlg.open || state.focus !== "projects" || e.metaKey) return;
    if (e.target.closest?.("input, textarea, .overlay")) return;
    if (document.activeElement === $("#filter")) return;
    if (e.target.closest?.(".xterm")) return;
    const k = e.key;
    if (k === "ArrowDown" || k === "j") moveSelection(1);
    else if (k === "ArrowUp" || k === "k") moveSelection(-1);
    else if (k === "Enter" && state.selected) openProject(state.selected);
    else if (k === "/") $("#filter").focus();
    else if (k === "Tab" || k === "Escape") {
      if (activeTab()) {
        setFocus("terminal");
        activeTab().term.focus();
      }
    } else return;
    e.preventDefault();
  });
  $("#btn-add").addEventListener("click", addProject);
  $("#btn-config").addEventListener("click", () => onMenu("settings"));
  $("#config-error").addEventListener("click", () => onMenu("edit-config"));
}

// ---------- drag projects to reorder them or move them to another group ----------

const projDrag = { path: "", target: null, after: false, group: "" };

function clearDropMarks() {
  document.querySelectorAll("#project-list .drop-before, #project-list .drop-after, #project-list .drop-into")
    .forEach((el) => el.classList.remove("drop-before", "drop-after", "drop-into"));
}

function wireProjectDrag() {
  const list = $("#project-list");
  list.addEventListener("dragstart", (e) => {
    const row = e.target.closest?.(".proj");
    if (!row) return;
    projDrag.path = row.dataset.path;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", row.dataset.path);
    requestAnimationFrame(() => row.classList.add("dragged"));
  });
  list.addEventListener("dragover", (e) => {
    if (!projDrag.path) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    clearDropMarks();
    const row = e.target.closest(".proj"), head = e.target.closest(".group");
    if (row && row.dataset.path !== projDrag.path) {
      const r = row.getBoundingClientRect();
      projDrag.target = row.dataset.path;
      projDrag.after = e.clientY > r.top + r.height / 2;
      projDrag.group = row.dataset.raw;
      row.classList.add(projDrag.after ? "drop-after" : "drop-before");
    } else if (head) {
      projDrag.target = "";
      projDrag.group = head.dataset.raw;
      head.classList.add("drop-into");
    } else projDrag.target = null;
  });
  list.addEventListener("drop", async (e) => {
    e.preventDefault();
    const { path, target, after, group } = projDrag;
    if (!path || target === null) return;
    try {
      await Go().MoveProject(path, target, after, group);
      if (group && state.collapsed.has(group)) {
        state.collapsed.delete(group); // show where it went
        Go().SetCollapsed([...state.collapsed]);
      }
    } catch (err) {
      flash(String(err));
    }
    endProjectDrag();
    await loadProjects();
  });
  list.addEventListener("dragend", endProjectDrag);
}

function endProjectDrag() {
  projDrag.path = "";
  projDrag.target = null;
  clearDropMarks();
  document.querySelectorAll("#project-list .dragged").forEach((el) => el.classList.remove("dragged"));
  lastProjectsHTML = "";
  renderProjects();
}

function moveSelection(d) {
  const grouped = state.projects.some((p) => p.group);
  const filtering = !!$("#filter").value.trim();
  const vis = visibleProjects().filter(({ p }) => filtering || !grouped || !state.collapsed.has(p.group || "Other"));
  if (!vis.length) return;
  let i = vis.findIndex((v) => v.p.path === state.selected);
  i = Math.max(0, Math.min(vis.length - 1, i + d));
  state.selected = vis[i].p.path;
  renderProjects();
  $("#project-list .selected")?.scrollIntoView({ block: "nearest" });
  if (!state.current) refreshStats();
}

function wireTabs() {
  $("#tab-list").addEventListener("click", (e) => {
    const close = e.target.closest("[data-close]");
    if (close) return requestCloseEntry(Number(close.dataset.close));
    const tab = e.target.closest(".tab");
    if (tab) selectEntry(Number(tab.dataset.e));
  });
  $("#tab-list").addEventListener("dblclick", (e) => {
    const tab = e.target.closest(".tab");
    if (tab && !e.target.closest("[data-close]")) renameEntry(Number(tab.dataset.e));
  });
  $("#tab-list").addEventListener("auxclick", (e) => {
    const tab = e.target.closest(".tab");
    if (tab && e.button === 1) requestCloseEntry(Number(tab.dataset.e));
  });
  $("#btn-new-tab").addEventListener("click", () => onMenu("new-claude"));
  $("#btn-new-shell").addEventListener("click", () => onMenu("new-shell"));
  $("#stats").addEventListener("click", (e) => {
    const v = e.target.closest("[data-view]")?.dataset.view;
    if (v) {
      state.tokenView = v;
      renderStats();
    }
  });
}

// renameEntry edits the k-th tab's name in place; empty puts the automatic name back.
function renameEntry(k) {
  const e = entries(state.current)[k];
  const label = $(`#tab-list .tab[data-e="${k}"] .label`);
  if (!e || !label) return;
  const t = e.tabs[0];
  const input = document.createElement("input");
  input.className = "tab-rename";
  input.value = e.group ? e.group.label || e.group.name : label.textContent;
  input.spellcheck = false;
  label.replaceWith(input);
  input.focus();
  input.select();
  let done = false;
  const finish = (save) => {
    if (done) return;
    done = true;
    const v = input.value.trim();
    if (save) {
      if (e.group) e.group.label = v;
      else t.customTitle = v;
    }
    input.remove();
    $("#tab-list").dataset.html = "";
    renderTabs();
    render();
  };
  input.addEventListener("keydown", (ev) => {
    ev.stopPropagation();
    if (ev.key === "Enter") finish(true);
    else if (ev.key === "Escape") finish(false);
  });
  input.addEventListener("blur", () => finish(true));
  input.addEventListener("click", (ev) => ev.stopPropagation());
}

// ---------- files dropped on a terminal ----------

// shellQuote escapes a path the way Terminal / iTerm do when you drop a file.
const shellQuote = (p) => p.replace(/([^\w@%+=:,./-])/g, "\\$1");

function wireFileDrop() {
  rt().OnFileDrop?.((x, y, paths) => {
    const el = document.elementFromPoint(x, y)?.closest(".term");
    const tab = Object.values(state.byId).find((t) => t.el === el) || activeTab();
    if (!tab || tab.exited || !paths?.length) return;
    tab.term.paste(paths.map(shellQuote).join(" ") + " ");
    setFocus("terminal");
    tab.term.focus();
  }, true);
}

// ---------- phones and tablets ----------

const isTouch = () => matchMedia("(hover: none) and (pointer: coarse)").matches;
const isNarrow = () => matchMedia("(max-width: 760px)").matches;

function openDrawer(side) {
  document.body.classList.toggle("drawer-left", side === "left" && !document.body.classList.contains("drawer-left"));
  document.body.classList.toggle("drawer-right", side === "right" && !document.body.classList.contains("drawer-right"));
  if (side === "right") refreshStats();
}

function closeDrawers() {
  document.body.classList.remove("drawer-left", "drawer-right");
}

// The key bar types what a phone keyboard has no key for.
const KEYS = {
  esc: "\x1b", tab: "\t", stab: "\x1b[Z", ctrlc: "\x03", up: "\x1b[A", down: "\x1b[B", left: "\x1b[D", right: "\x1b[C",
  enter: "\r", nl: "\n", slash: "/",
};
let ctrlArmed = false;

// stickyCtrl turns the next typed letter into Ctrl+letter after the ctrl key on the key bar.
function stickyCtrl(d) {
  if (!ctrlArmed || d.length !== 1) return d;
  ctrlArmed = false;
  $("#keybar [data-key=ctrl]")?.classList.remove("on");
  const c = d.toLowerCase().charCodeAt(0);
  return c >= 97 && c <= 122 ? String.fromCharCode(c - 96) : d;
}

function wireKeybar() {
  const bar = $("#keybar");
  // pointerdown + preventDefault: the terminal keeps focus, so the phone keyboard stays up
  bar.addEventListener("pointerdown", (e) => {
    const b = e.target.closest("[data-key]");
    if (!b) return;
    e.preventDefault();
    const tab = activeTab();
    if (!tab || tab.exited) return;
    const k = b.dataset.key;
    if (k === "prompts") return $("#prompt-menu").classList.contains("hidden") ? showPromptMenu(b) : hideMenus();
    if (!canType()) return watchOnly();
    if (k === "ctrl") {
      ctrlArmed = !ctrlArmed;
      b.classList.toggle("on", ctrlArmed);
      return;
    }
    if (k === "kbd") {
      if (document.activeElement?.closest?.(".xterm")) document.activeElement.blur();
      else tab.term.focus();
      return;
    }
    if (k === "paste") {
      navigator.clipboard?.readText?.().then((t) => t && tab.term.paste(t), () => flash("allow clipboard access to paste"));
      return;
    }
    claimTab(tab);
    tab.lastInput = Date.now();
    Go().Write(tab.id, KEYS[k]);
  });
}

// On a phone the keyboard covers the bottom of the page; the visual
// viewport is what is left, so the app is sized to it.
function wireViewport() {
  const vv = window.visualViewport;
  if (!vv) return;
  const fit = () => {
    document.documentElement.style.setProperty("--vvh", vv.height + "px");
    if (isNarrow() || isTouch()) window.scrollTo(0, 0);
    fitActive();
  };
  vv.addEventListener("resize", fit);
  vv.addEventListener("scroll", fit);
  fit();
}

function wireActiveList() {
  $("#active-list").addEventListener("click", (e) => {
    const b = e.target.closest("[data-open]");
    if (b) openProject(b.dataset.open);
  });
}

// Pinching a terminal with two fingers changes the text size (phones,
// tablets); it is kept per device, like ⌘+ and ⌘- on the Mac.
function wirePinch() {
  const terms = $("#terms");
  let start = 0, base = 0, last = 0;
  const gap = (t) => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
  terms.addEventListener("touchstart", (e) => {
    if (e.touches.length !== 2) return;
    start = gap(e.touches);
    base = last = state.fontSize;
  }, { passive: true });
  terms.addEventListener("touchmove", (e) => {
    if (e.touches.length !== 2 || !start) return;
    e.preventDefault(); // not the page's own zoom
    const n = Math.round(base * (gap(e.touches) / start));
    if (n !== last) {
      last = n;
      setFontSize(n);
    }
  }, { passive: false });
  terms.addEventListener("gesturestart", (e) => e.preventDefault()); // Safari's own pinch zoom
  terms.addEventListener("touchend", (e) => {
    if (e.touches.length < 2 && start) {
      start = 0;
      if (last !== base) flash(`text size ${state.fontSize}`);
    }
  });
}

function wireMobile() {
  $("#btn-drawer").addEventListener("click", () => openDrawer("left"));
  $("#btn-stats").addEventListener("click", () => openDrawer("right"));
  $("#scrim").addEventListener("click", closeDrawers);
  $("#pane-bar").addEventListener("click", (e) => {
    const b = e.target.closest("[data-pane]");
    if (b) selectTab(Number(b.dataset.pane));
  });
  wireKeybar();
  wireViewport();
  wirePinch();
  // The key bar shows while a terminal has the keyboard.
  const typing = () => document.body.classList.toggle("typing", !!document.activeElement?.closest?.(".xterm"));
  document.addEventListener("focusin", typing);
  document.addEventListener("focusout", () => setTimeout(typing, 0));
  matchMedia("(max-width: 760px)").addEventListener("change", () => {
    closeDrawers();
    fitActive();
  });
}

// ---------- remote access ----------

// In a browser: sign out, and ask to be notified when Claude needs you.
function wireRemoteUI() {
  document.body.classList.add("remote");
  $("#btn-signout").addEventListener("click", async () => {
    if (await Go().Confirm("Sign out?", "You will need your passkey to sign in again.", "Sign out")) remoteMod.signOut();
  });
  const bell = $("#btn-notify");
  const showBell = () => bell.classList.toggle("hidden", !("Notification" in window) || Notification.permission !== "default");
  bell.addEventListener("click", async () => {
    if (!("PushManager" in window) && /iPhone|iPad/.test(navigator.userAgent) && !navigator.standalone)
      flash("add Quarters to your Home Screen first (Share → Add to Home Screen) for alerts");
    const ok = await Notification.requestPermission();
    showBell();
    if (ok === "granted") {
      await syncPush();
      flash(remoteMod.remote.push ? "alerts on — even when Quarters is closed" : "alerts on while Quarters is open");
    }
  });
  showBell();
  const showAccess = () => {
    for (const a of ["full", "type", "view"]) document.body.classList.toggle(`access-${a}`, access() === a);
  };
  showAccess();
  if (access() !== "full") flash(access() === "view" ? "this device can only watch" : "this device can only type into running sessions");
  remoteMod.remote.onReconnect(() => {
    showAccess();
    loadSettings();
    state.me = remoteMod.remote.client;
    syncSessions(true);
    loadProjects();
    flash("reconnected");
  });
}

// The Mac's Remote Access dialog: turn it on, pair devices, see who signed in.
const rmd = { open: false, timer: 0, pairing: null, devices: 0, status: null };

// toggleRemote starts or stops the remote-access server from the menu.
async function toggleRemote() {
  if (REMOTE) return;
  const v = await Go().RemoteStatus();
  if (!v.url) return openRemoteDialog();
  try {
    await Go().SetRemote(!v.running, v.url, v.listen);
    flash(v.running ? "remote server stopped" : `remote server started · ${v.url}`);
  } catch (e) {
    flash(String(e).replace(/^Error: /, ""));
    openRemoteDialog();
  }
  Go().RemoteStatus().then(renderRemotePill).catch(() => {});
}

async function openRemoteDialog() {
  if (REMOTE) return;
  rmd.open = true;
  rmd.pairing = null;
  $("#remote-dialog").classList.remove("hidden");
  $("#rm-error").textContent = "";
  const v = await Go().RemoteStatus();
  $("#rm-url").value = v.url || "";
  $("#rm-listen").value = v.listen || "";
  $("#rm-lan").checked = !!v.lan;
  $("#rm-user").value = v.user || "";
  $("#rm-pass").value = $("#rm-pass2").value = "";
  rmd.devices = (v.devices || []).length;
  renderRemote(v);
  clearInterval(rmd.timer);
  rmd.timer = setInterval(refreshRemoteDialog, 2000);
}

function closeRemoteDialog() {
  rmd.open = false;
  clearInterval(rmd.timer);
  if (rmd.pairing) Go().CancelPairing();
  rmd.pairing = null;
  $("#remote-dialog").classList.add("hidden");
}

async function refreshRemoteDialog() {
  if (!rmd.open) return;
  const v = await Go().RemoteStatus();
  const n = (v.devices || []).length;
  if (rmd.pairing && n > rmd.devices) {
    const d = v.devices[n - 1];
    rmd.pairing = null;
    flash(`paired ${d.name}`);
  }
  rmd.devices = n;
  renderRemote(v);
}

function renderRemote(v) {
  rmd.status = v;
  $("#rm-state").innerHTML = v.running ? '<span class="green">● running</span>' : v.enabled ? '<span class="red">● failed to start</span>' : '<span class="muted">○ stopped</span>';
  const online = (v.devices || []).reduce((n, d) => n + (d.online || 0), 0);
  $("#rm-power-text").innerHTML = v.running
    ? `<b class="green">Server running</b> <span class="muted">at <a href="#" class="rm-addr" title="Open in your browser">${esc(v.address || v.url)}</a>${online ? ` · ${online} connected` : ""}</span>`
    : `<b>Server stopped</b> <span class="muted">— nothing can reach this Mac from outside</span>`;
  $("#rm-toggle").textContent = v.running ? "Stop server" : "Start server";
  $("#rm-toggle").classList.toggle("primary", !v.running);
  if (v.error) $("#rm-error").textContent = v.error;
  const devs = v.devices || [];
  $("#rm-devices").innerHTML = devs.length
    ? devs.map((d) => `<div class="rm-dev"><span class="${d.online ? "green" : "muted"}">${d.online ? "●" : "○"}</span>
        <span class="grow"><b>${esc(d.name)}</b>${d.push ? ' <span title="Gets phone alerts">🔔</span>' : ""} <span class="muted">${d.lastUsed ? `last used ${fmtAgo(d.lastUsed)} ago${d.lastIP ? " · " + esc(d.lastIP) : ""}` : ""}</span></span>
        <select class="rm-access" data-access="${esc(d.id)}" title="What this device may do">${[["full", "full control"], ["type", "type only"], ["view", "watch only"]].map(([v, l]) => `<option value="${v}"${(d.access || "full") === v ? " selected" : ""}>${l}</option>`).join("")}</select>
        <button class="btn danger" type="button" data-remove="${esc(d.id)}">Remove</button></div>`).join("")
    : '<div class="muted">No devices yet. Turn remote access on, then pair your phone.</div>';
  $("#rm-pw-state").innerHTML = v.user
    ? `On for <b>${esc(v.user)}</b>. Set a new password to change it (its browsers are signed out); remove <i>Password · ${esc(v.user)}</i> under Devices to turn it off.`
    : "Off. Set one (10+ characters) for browsers that can't use a passkey, such as at the Mac's home-network address.";
  $("#rm-pw-set").textContent = v.user ? "Change password" : "Set password";
  const li = $("#rm-lan-info"), lanOn = v.running && v.lan && (v.lanURLs || []).length > 0;
  li.classList.toggle("hidden", !lanOn);
  if (lanOn && li.dataset.key !== v.lanURLs.join() + "|" + v.user) { // rebuilt only when it changes
    li.dataset.key = v.lanURLs.join() + "|" + v.user;
    li.innerHTML = `<img src="${v.lanQR}" alt="QR code of ${esc(v.lanURLs[0])}" width="132" height="132">
      <div><b>On your home network</b><div class="muted">Scan with your phone's camera, or open:</div>
      ${v.lanURLs.map((u) => `<div><code>${esc(u)}</code></div>`).join("")}
      <div class="muted">${v.user ? `Sign in as <b>${esc(v.user)}</b>.` : '<span class="red">Set a password below first.</span>'}</div></div>`;
  }
  $("#rm-pair").disabled = !v.running;
  $("#rm-signout").disabled = !v.running;
  const pb = $("#rm-pairing");
  pb.classList.toggle("hidden", !rmd.pairing);
  if (rmd.pairing) {
    const left = Math.max(0, Math.round((rmd.pairing.expires - Date.now()) / 1000));
    if (!pb.dataset.url || pb.dataset.url !== rmd.pairing.url) {
      pb.dataset.url = rmd.pairing.url;
      pb.innerHTML = `<img src="${rmd.pairing.qr}" alt="pairing QR code">
        <div class="rm-pair-text"><b>Scan with your phone's camera</b>
          <span class="muted">or open this link on the device (it works once):</span>
          <code>${esc(rmd.pairing.url)}</code>
          <span class="muted" id="rm-left"></span>
          <button class="btn" type="button" id="rm-pair-cancel">Cancel</button></div>`;
      requestAnimationFrame(() => pb.scrollIntoView({ block: "nearest", behavior: "smooth" }));
      $("#rm-pair-cancel").onclick = () => {
        Go().CancelPairing();
        rmd.pairing = null;
        refreshRemoteDialog();
      };
    }
    $("#rm-left").textContent = left ? `expires in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}` : "expired";
  } else pb.dataset.url = "";
  $("#rm-log").textContent = (v.log || []).join("\n") || "nothing yet";
  renderRemotePill(v);
}

function renderRemotePill(v) {
  const pill = $("#btn-remote");
  const online = (v.devices || []).reduce((n, d) => n + (d.online || 0), 0);
  pill.classList.remove("hidden");
  pill.classList.toggle("bad", v.enabled && !v.running);
  pill.classList.toggle("off", !v.enabled);
  pill.innerHTML = v.running ? `<span class="green live">●</span> remote${online ? ` · ${online}` : ""}` : v.enabled ? '<span class="red">●</span> remote' : "○ remote";
  pill.title = v.running ? `Remote server running (${v.address || v.url})${online ? ` · ${online} connected` : ""} — click to manage` : v.error || "Remote server stopped — click to start it";
}

function wireRemoteDialog() {
  $("#btn-remote").addEventListener("click", openRemoteDialog);
  $("#rm-close").addEventListener("click", closeRemoteDialog);
  $("#remote-dialog").addEventListener("mousedown", (e) => e.target.id === "remote-dialog" && closeRemoteDialog());
  $("#remote-dialog").addEventListener("keydown", (e) => e.key === "Escape" && closeRemoteDialog());
  // Save keeps the server as it is (restarting it with the new addresses if it runs).
  const setRemote = async (on) => {
    $("#rm-error").textContent = "";
    try {
      await Go().SetRemote(on, $("#rm-url").value, $("#rm-listen").value, $("#rm-lan").checked);
      return true;
    } catch (e) {
      $("#rm-error").textContent = String(e).replace(/^Error: /, "");
      return false;
    } finally {
      refreshRemoteDialog();
    }
  };
  $("#rm-save").addEventListener("click", async () => (await setRemote(!!(rmd.status && rmd.status.enabled))) && flash("remote access saved"));
  $("#rm-lan").addEventListener("change", async () => (await setRemote(!!(rmd.status && rmd.status.enabled))) && flash($("#rm-lan").checked ? "home network on" : "home network off"));
  $("#rm-pw-set").addEventListener("click", async () => {
    const user = $("#rm-user").value.trim(), pw = $("#rm-pass").value;
    $("#rm-error").textContent = "";
    if (pw !== $("#rm-pass2").value) return ($("#rm-error").textContent = "the two passwords are different");
    try {
      await Go().SetRemotePassword(user, pw);
      $("#rm-pass").value = $("#rm-pass2").value = "";
      flash(`password set for ${user}`);
    } catch (e) {
      $("#rm-error").textContent = String(e).replace(/^Error: /, "");
    }
    refreshRemoteDialog();
  });
  $("#rm-power-text").addEventListener("click", (e) => {
    if (!e.target.closest(".rm-addr")) return;
    e.preventDefault();
    openLink(rmd.status.address || rmd.status.url);
  });
  $("#rm-toggle").addEventListener("click", async () => {
    const on = !(rmd.status && rmd.status.running);
    if (await setRemote(on)) flash(on ? "remote server started" : "remote server stopped");
  });
  $("#rm-pair").addEventListener("click", async () => {
    try {
      rmd.pairing = await Go().PairDevice();
      refreshRemoteDialog();
    } catch (e) {
      $("#rm-error").textContent = String(e);
    }
  });
  $("#rm-signout").addEventListener("click", async () => {
    if (await Go().Confirm("Sign out all browsers?", "Every signed-in browser is disconnected. Devices stay paired and can sign in again with their passkey.", "Sign out all")) {
      Go().SignOutAllDevices();
      refreshRemoteDialog();
    }
  });
  $("#rm-devices").addEventListener("change", async (e) => {
    const sel = e.target.closest("[data-access]");
    if (!sel) return;
    try {
      await Go().SetDeviceAccess(sel.dataset.access, sel.value);
      flash(`${sel.closest(".rm-dev").querySelector("b").textContent}: ${sel.selectedOptions[0].textContent}`);
    } catch (err) {
      flash(String(err));
    }
    refreshRemoteDialog();
  });
  $("#rm-devices").addEventListener("click", async (e) => {
    const b = e.target.closest("[data-remove]");
    if (!b) return;
    const name = b.closest(".rm-dev").querySelector("b").textContent;
    if (b.dataset.remove === "password") {
      if (!(await Go().Confirm("Turn off password sign-in?", "Browsers signed in with the password are signed out at once. You can set a password again later.", "Turn off"))) return;
    } else if (!(await Go().Confirm(`Remove ${name}?`, "It is signed out at once and its passkey stops working here. You can pair it again later.", "Remove"))) return;
    try {
      await Go().RemoveDevice(b.dataset.remove);
    } catch (err) {
      flash(String(err));
    }
    refreshRemoteDialog();
  });
  const pill = () => Go().RemoteStatus().then(renderRemotePill).catch(() => {});
  pill();
  setInterval(() => !rmd.open && pill(), 5000);
}

// ---------- settings: optional features, switched on and off ----------

// on reports whether an optional feature is switched on (Settings).
const on = (key) => {
  const f = state.settings && state.settings.features.find((x) => x.key === key);
  return !!(f && f.on);
};

// What this browser may do (remote access: a device can be watch-only).
const access = () => (REMOTE ? remoteMod.remote.access || "full" : "full");
const canType = () => access() !== "view";
const canChange = () => access() === "full";
let watchNoted = 0;
function watchOnly() {
  if (Date.now() - watchNoted < 4000) return;
  watchNoted = Date.now();
  flash("this device can only watch (change it on the Mac: Remote Access)");
}

async function loadSettings() {
  try {
    state.settings = await Go().Settings();
  } catch {
    return;
  }
  document.body.classList.toggle("no-prompts", !on("prompts"));
  document.body.classList.toggle("no-keybar", !on("keybar"));
  if (!on("dev_links")) state.links = [];
  lastProjectsHTML = "";
  render();
  renderStats();
  if (!activeTab()) renderStart(project(state.current));
  if (set.open) renderSettings(false);
  if (REMOTE) syncPush();
}

const set = { open: false, prompts: [] };

async function openSettings() {
  await loadSettings();
  if (!state.settings) return;
  set.open = true;
  set.prompts = (state.settings.prompts || []).map((p) => ({ ...p }));
  $("#set-error").textContent = "";
  renderSettings(true);
  $("#settings-dialog").classList.remove("hidden");
  $("#set-close").focus();
}

function closeSettings() {
  set.open = false;
  $("#settings-dialog").classList.add("hidden");
  if (state.focus === "terminal") activeTab()?.term.focus();
}

function renderSettings(full) {
  const v = state.settings;
  $("#set-path").textContent = v.configPath;
  $("#set-features").innerHTML = v.features
    .map((f) => {
      const locked = (f.macOnly && REMOTE) || !canChange();
      const note = f.key === "keep_awake" && v.awake ? ' <span class="green">· keeping it awake now</span>' : f.key === "hotkey" && f.on ? ` <span class="soft">· ${esc(v.hotkey)}</span>` : "";
      return `<label class="set-row${locked ? " locked" : ""}"><span class="set-text"><b>${esc(f.title)}</b><span class="muted">${esc(f.about)}${note}${f.macOnly && REMOTE ? " · change it on the Mac" : ""}</span></span>
        <input type="checkbox" class="switch" data-feature="${f.key}"${f.on ? " checked" : ""}${locked ? " disabled" : ""}></label>`;
    })
    .join("");
  $("#set-yolo").checked = v.yolo;
  $("#set-yolo").disabled = REMOTE;
  if (full) {
    $("#set-alert").value = v.usageAlertAt;
    $("#set-hotkey").value = v.hotkey;
    renderPromptRows();
  }
  $("#set-alert").disabled = !canChange();
  $("#set-save").disabled = !canChange();
  $("#set-prompt-add").disabled = !canChange();
  $("#set-hotkey-note").innerHTML = v.hotkeyError ? `<span class="red">${esc(v.hotkeyError)}</span>` : "brings Quarters to the front from any app (again: hides it)";
}

function renderPromptRows() {
  $("#set-prompts").innerHTML =
    set.prompts
      .map((p, i) => `<div class="sp-row" data-p="${i}">
        <input type="text" data-f="name" value="${esc(p.name)}" placeholder="name" spellcheck="false">
        <textarea data-f="text" rows="2" placeholder="what to type, e.g. run the tests and fix what fails" spellcheck="false">${esc(p.text)}</textarea>
        <label class="ac-check" title="Press Enter after typing it"><input type="checkbox" data-f="send"${p.send ? " checked" : ""}> send</label>
        <button type="button" class="ac-del" data-del-prompt title="Delete">✕</button></div>`)
      .join("") || '<div class="muted">No saved prompts yet.</div>';
}

async function saveSettings() {
  const v = state.settings;
  $("#set-error").textContent = "";
  try {
    const at = Number($("#set-alert").value);
    if (at !== v.usageAlertAt) await Go().SetUsageAlertAt(at);
    const hk = $("#set-hotkey").value.trim();
    if (!REMOTE && hk !== v.hotkey) await Go().SetHotkey(hk);
    await Go().SavePrompts(set.prompts);
    await loadSettings();
    closeSettings();
    flash("settings saved");
  } catch (e) {
    $("#set-error").textContent = String(e).replace(/^Error: /, "");
    loadSettings();
  }
}

function wireSettings() {
  const dlgEl = $("#settings-dialog");
  dlgEl.addEventListener("change", async (e) => {
    const key = e.target.dataset.feature;
    if (key) {
      $("#set-error").textContent = "";
      try {
        await Go().SetFeature(key, e.target.checked);
        flash(`${state.settings.features.find((f) => f.key === key)?.title.toLowerCase()} ${e.target.checked ? "on" : "off"}`);
      } catch (err) {
        e.target.checked = !e.target.checked;
        $("#set-error").textContent = String(err).replace(/^Error: /, "");
      }
      loadSettings();
    } else if (e.target.id === "set-yolo") {
      try {
        await Go().SetYolo(e.target.checked);
      } catch (err) {
        $("#set-error").textContent = String(err).replace(/^Error: /, "");
      }
      loadSettings();
    }
  });
  const box = $("#set-prompts");
  const update = (e) => {
    const row = e.target.closest("[data-p]"), f = e.target.dataset.f;
    if (!row || !f) return;
    set.prompts[Number(row.dataset.p)][f] = e.target.type === "checkbox" ? e.target.checked : e.target.value;
  };
  box.addEventListener("input", update);
  box.addEventListener("change", update);
  box.addEventListener("click", (e) => {
    if (!e.target.closest("[data-del-prompt]")) return;
    set.prompts.splice(Number(e.target.closest("[data-p]").dataset.p), 1);
    renderPromptRows();
  });
  $("#set-prompt-add").addEventListener("click", () => {
    set.prompts.push({ name: "", text: "", send: true });
    renderPromptRows();
    box.querySelector(".sp-row:last-child input")?.focus();
  });
  $("#set-save").addEventListener("click", saveSettings);
  $("#set-close").addEventListener("click", closeSettings);
  $("#set-config").addEventListener("click", () => onMenu("edit-config"));
  $("#set-remote").addEventListener("click", () => {
    closeSettings();
    openRemoteDialog();
  });
  dlgEl.addEventListener("keydown", (e) => e.key === "Escape" && closeSettings());
  dlgEl.addEventListener("mousedown", (e) => e.target === dlgEl && closeSettings());
}

// ---------- menus: saved prompts and worktrees ----------

// placeMenu shows a small menu under (or above) the element that opened it.
function placeMenu(menu, anchor) {
  menu.classList.remove("hidden");
  const r = anchor.getBoundingClientRect();
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.max(8, Math.min(window.innerWidth - w - 8, r.left)) + "px";
  menu.style.top = (r.bottom + h + 8 > window.innerHeight ? Math.max(8, r.top - h - 4) : r.bottom + 4) + "px";
}

function hideMenus() {
  $("#prompt-menu").classList.add("hidden");
  $("#wt-menu").classList.add("hidden");
}

function showPromptMenu(anchor) {
  const list = (state.settings && state.settings.prompts) || [];
  const menu = $("#prompt-menu");
  menu.innerHTML =
    `<div class="am-head">SAVED PROMPTS</div>` +
    (list.length
      ? list.map((p, i) => `<button class="am-item" type="button" data-prompt="${i}" title="${esc(p.text)}"><span class="accent">⚡</span><span class="grow">${esc(p.name)}</span><span class="muted">${p.send ? "↵" : "…"}</span></button>`).join("")
      : `<div class="am-empty muted">No prompts yet.</div>`) +
    `<button class="am-item am-edit" type="button" data-prompt-edit="1"><span class="muted">✎</span><span class="grow muted">Edit prompts…</span></button>`;
  placeMenu(menu, anchor);
}

// typePrompt types a saved prompt into the Claude tab you are looking at.
function typePrompt(p) {
  const tab = activeTab();
  if (!tab || tab.exited) return flash("open a Claude tab first");
  if (!canType()) return watchOnly();
  claimTab(tab);
  tab.lastInput = Date.now();
  tab.term.paste(p.text); // a paste: its new lines don't send it early
  if (p.send) setTimeout(() => Go().Write(tab.id, "\r"), 150);
  if (!isTouch()) tab.term.focus();
}

async function showWorktreeMenu(path, anchor) {
  const menu = $("#wt-menu");
  menu.dataset.path = path;
  menu.innerHTML = `<div class="am-head">WORKTREES · ${esc(project(path)?.name || "")}</div><div class="am-empty muted">loading…</div>`;
  placeMenu(menu, anchor);
  const list = ((await Go().Worktrees(path).catch(() => [])) || []).filter((w) => !w.main);
  if (menu.dataset.path !== path || menu.classList.contains("hidden")) return;
  const running = (dir) => tabsOf(path).filter((t) => t.startOpts?.cwd === dir && !t.exited).length;
  menu.innerHTML =
    `<div class="am-head">WORKTREES · ${esc(project(path)?.name || "")}</div>` +
    (list.length
      ? list.map((w) => `<div class="am-row"><button class="am-item" type="button" data-wt-open="${esc(w.dir)}" title="${esc(w.dir)} — new Claude tab here">
          <span class="violet">⎇</span><span class="grow">${esc(w.branch || w.name)}</span>${w.dirty ? `<span class="yellow">●${w.dirty}</span>` : ""}${running(w.dir) ? `<span class="green">${running(w.dir)} open</span>` : ""}</button>
          ${canChange() ? `<button class="am-stop" type="button" data-wt-del="${esc(w.dir)}" data-dirty="${w.dirty}" title="Delete this worktree (its branch stays)">✕</button>` : ""}</div>`).join("")
      : `<div class="am-empty muted">No worktrees yet. Each is a separate copy of the project on its own branch, so several Claudes can work at once.</div>`) +
    (canChange() ? `<form class="wt-new"><input type="text" id="wt-name" placeholder="new branch, e.g. fix-login" spellcheck="false" autocomplete="off"><button class="btn primary" type="submit">Create</button></form>` : "");
  placeMenu(menu, anchor);
}

function wireMenus() {
  $("#btn-prompts").addEventListener("click", (e) => ($("#prompt-menu").classList.contains("hidden") ? showPromptMenu(e.currentTarget) : hideMenus()));
  $("#btn-wt").addEventListener("click", (e) => {
    const path = state.current || state.selected;
    $("#wt-menu").classList.contains("hidden") ? showWorktreeMenu(path, e.currentTarget) : hideMenus();
  });
  $("#btn-history").addEventListener("click", () => openHistory());
  $("#prompt-menu").addEventListener("click", (e) => {
    const b = e.target.closest("[data-prompt], [data-prompt-edit]");
    if (!b) return;
    hideMenus();
    if (b.dataset.promptEdit) return openSettings();
    typePrompt(state.settings.prompts[Number(b.dataset.prompt)]);
  });
  // keep the terminal's focus (and the phone keyboard) while picking a prompt
  $("#prompt-menu").addEventListener("pointerdown", (e) => e.target.closest("button") && e.preventDefault());
  const wt = $("#wt-menu");
  wt.addEventListener("click", async (e) => {
    const path = wt.dataset.path;
    const open = e.target.closest("[data-wt-open]"), del = e.target.closest("[data-wt-del]");
    if (open) {
      hideMenus();
      if (!canChange()) return watchOnly();
      return newTab(path, "claude", { cwd: open.dataset.wtOpen });
    }
    if (del) {
      hideMenus();
      const dir = del.dataset.wtDel, dirty = Number(del.dataset.dirty);
      const name = dir.split("/").pop();
      const msg = dirty ? `It has ${dirty} uncommitted change(s), which will be lost. Its branch stays.` : "The folder is deleted; its branch and commits stay.";
      if (!(await Go().Confirm(`Delete worktree “${name}”?`, msg, "Delete"))) return;
      try {
        await Go().RemoveWorktree(path, dir, dirty > 0);
        flash(`deleted worktree ${name}`);
      } catch (err) {
        flash(String(err).replace(/^Error: /, ""));
      }
    }
  });
  wt.addEventListener("submit", async (e) => {
    e.preventDefault();
    const path = wt.dataset.path, name = $("#wt-name").value.trim();
    if (!name) return;
    const btn = wt.querySelector(".wt-new .btn");
    btn.disabled = true;
    btn.textContent = "creating…";
    try {
      const dir = await Go().CreateWorktree(path, name);
      hideMenus();
      flash(`worktree ${name} ready · Claude starts in it`);
      await newTab(path, "claude", { cwd: dir });
    } catch (err) {
      btn.disabled = false;
      btn.textContent = "Create";
      flash(String(err).replace(/^Error: /, ""));
    }
  });
  window.addEventListener("mousedown", (e) => {
    if (!e.target.closest("#prompt-menu, #wt-menu, #btn-prompts, #btn-wt, [data-key='prompts']")) hideMenus();
  }, true);
  window.addEventListener("keydown", (e) => e.key === "Escape" && hideMenus(), true);
}

// ---------- pick up where you left off (after Quarters restarts) ----------

async function checkRestore() {
  if (!on("restore") || !canChange()) return renderRestore([]);
  let items = [];
  try {
    items = (await Go().RestoreInfo()) || [];
  } catch {}
  renderRestore(items);
}

function renderRestore(items) {
  state.restore = items;
  const el = $("#restore");
  el.classList.toggle("hidden", !items.length);
  if (!items.length) return;
  const what = (it) => (it.kind === "auto" ? it.auto : (it.cwd ? `⎇ ${it.cwd.split("/").pop()} · ` : "") + (it.title || (it.conv ? "Claude conversation" : "last conversation")));
  el.innerHTML = `<div class="rs-title"><span class="accent">↻</span> Pick up where you left off?</div>
    <ul>${items.map((it) => `<li>${it.kind === "auto" ? '<span class="green">▶</span>' : '<span class="accent">✻</span>'} <b>${esc(it.name)}</b> <span class="muted">${esc(what(it))}</span></li>`).join("")}</ul>
    <div class="rs-actions"><button class="btn primary" type="button" data-restore="all">Reopen ${items.length === 1 ? "it" : `all ${items.length}`}</button><button class="btn" type="button" data-restore="no">Not now</button></div>`;
}

async function doRestore() {
  const items = state.restore;
  renderRestore([]);
  Go().DismissRestore();
  let first = null;
  for (const it of items) {
    const p = project(it.path);
    if (!p) continue;
    if (it.kind === "auto") {
      const a = (p.automations || []).find((x) => x.name === it.auto);
      if (a && !autoRunning(p.path, a)) await runAutomation(p.path, a);
    } else {
      const t = await newTab(p.path, it.conv ? "resume" : "continue", { resume: it.conv, cwd: it.cwd, background: true });
      first ||= t;
    }
  }
  if (first) {
    openProject(first.path);
    selectTab(tabsOf(first.path).indexOf(first));
  }
  flash(`reopened ${items.length}`);
}

// ---------- past conversations ----------

const hist = { open: false, path: "", list: [], sel: 0, timer: 0 };

function openHistory(path) {
  path = path || state.current || state.selected;
  if (!path || !on("history")) return;
  hist.open = true;
  hist.path = path;
  hist.sel = 0;
  hist.list = [];
  $("#hi-title").textContent = `CONVERSATIONS · ${project(path)?.name || ""}`;
  $("#hi-query").value = "";
  $("#hi-list").innerHTML = "";
  $("#history-dialog").classList.remove("hidden");
  if (!isTouch()) $("#hi-query").focus();
  searchHistory();
}

function closeHistory() {
  hist.open = false;
  $("#history-dialog").classList.add("hidden");
  if (state.focus === "terminal") activeTab()?.term.focus();
}

async function searchHistory() {
  const q = $("#hi-query").value, path = hist.path;
  $("#hi-count").textContent = "searching…";
  let list = [];
  try {
    list = (await Go().Conversations(path, q)) || [];
  } catch (e) {
    flash(String(e));
  }
  if (!hist.open || q !== $("#hi-query").value || path !== hist.path) return;
  hist.list = list;
  hist.sel = Math.min(hist.sel, Math.max(0, list.length - 1));
  renderHistory();
}

// mark wraps each case-insensitive hit of q in s.
function mark(s, q) {
  if (!q) return esc(s);
  const lower = s.toLowerCase(), ql = q.toLowerCase();
  let out = "", i = 0;
  for (let j; (j = lower.indexOf(ql, i)) >= 0; i = j + ql.length) out += esc(s.slice(i, j)) + `<span class="hit">${esc(s.slice(j, j + ql.length))}</span>`;
  return out + esc(s.slice(i));
}

function renderHistory() {
  const q = $("#hi-query").value.trim();
  const n = hist.list.length;
  $("#hi-count").textContent = `${n} conversation${n === 1 ? "" : "s"}`;
  const openIds = new Set(Object.values(state.byId).filter((t) => !t.exited).map((t) => t.startOpts?.resume).filter(Boolean));
  const when = (ms) => new Date(ms).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  $("#hi-list").innerHTML =
    hist.list
      .map((c, i) => `<button type="button" class="hi-item${i === hist.sel ? " sel" : ""}" data-i="${i}">
        <div class="row"><span class="hi-title">${esc(c.title || c.first || "untitled")}</span><span class="muted">${fmtAgo(c.last)}${fmtAgo(c.last) === "now" ? "" : " ago"}</span></div>
        ${c.match ? `<div class="hi-sub">${mark(c.match, q)}</div>` : c.title && c.first ? `<div class="hi-sub muted">❯ ${esc(c.first)}</div>` : ""}
        <div class="muted hi-meta">${c.prompts} prompt${c.prompts === 1 ? "" : "s"} · ${when(c.start)}${openIds.has(c.id) ? ' · <span class="green">open</span>' : ""}</div></button>`)
      .join("") || `<div class="empty">${q ? "Nothing matches." : "No conversations in this project yet."}</div>`;
  $("#hi-list .sel")?.scrollIntoView({ block: "nearest" });
}

function pickHistory(i) {
  const c = hist.list[i];
  if (!c) return;
  const path = hist.path;
  closeHistory();
  if (!canChange()) return watchOnly();
  const open = Object.values(state.byId).find((t) => t.startOpts?.resume === c.id && !t.exited && t.path === path);
  if (open) {
    openProject(path);
    return selectTab(tabsOf(path).indexOf(open));
  }
  newTab(path, "resume", { resume: c.id });
}

function wireHistory() {
  $("#hi-query").addEventListener("input", () => {
    hist.sel = 0;
    clearTimeout(hist.timer);
    hist.timer = setTimeout(searchHistory, 180);
  });
  $("#hi-query").addEventListener("keydown", (e) => {
    const n = hist.list.length;
    if (e.key === "Escape") closeHistory();
    else if (e.key === "ArrowDown") hist.sel = Math.min(n - 1, hist.sel + 1);
    else if (e.key === "ArrowUp") hist.sel = Math.max(0, hist.sel - 1);
    else if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      return pickHistory(hist.sel);
    } else return;
    e.preventDefault();
    renderHistory();
  });
  $("#hi-list").addEventListener("click", (e) => {
    const item = e.target.closest(".hi-item");
    if (item) pickHistory(Number(item.dataset.i));
  });
  const el = $("#history-dialog");
  el.addEventListener("mousedown", (e) => e.target === el && closeHistory());
  el.addEventListener("keydown", (e) => e.key === "Escape" && closeHistory());
}

// ---------- a file's changes ----------

async function showDiff(path, file) {
  const el = $("#diff-dialog");
  el.classList.remove("hidden");
  $("#df-file").textContent = file;
  $("#df-stat").textContent = "";
  $("#df-note").textContent = "loading…";
  $("#df-body").innerHTML = "";
  $("#df-close").focus();
  let d;
  try {
    d = await Go().Diff(path, file);
  } catch (e) {
    $("#df-note").textContent = String(e).replace(/^Error: /, "");
    return;
  }
  $("#df-title").textContent = d.file.split("/").pop().toUpperCase();
  $("#df-note").textContent = d.note || "";
  $("#df-note").classList.toggle("hidden", !d.note);
  let add = 0, del = 0;
  const lines = (d.diff || "").replace(/\n$/, "").split("\n");
  const html = lines
    .map((l) => {
      if (d.added) {
        add++;
        return `<span class="dl add">+${esc(l)}</span>`;
      }
      if (/^(diff --git|index |--- |\+\+\+ |new file mode|deleted file mode|similarity|rename )/.test(l)) return "";
      if (l.startsWith("@@")) return `<span class="dl hunk">${esc(l)}</span>`;
      if (l.startsWith("+")) return add++, `<span class="dl add">${esc(l)}</span>`;
      if (l.startsWith("-")) return del++, `<span class="dl del">${esc(l)}</span>`;
      return `<span class="dl">${esc(l)}</span>`;
    })
    .filter(Boolean)
    .join("");
  $("#df-body").innerHTML = d.diff ? html : "";
  $("#df-stat").innerHTML = d.diff ? `<span class="green">+${add}</span> <span class="red">−${del}</span>` : "";
}

function closeDiff() {
  $("#diff-dialog").classList.add("hidden");
}

function wireDiff() {
  const el = $("#diff-dialog");
  $("#df-close").addEventListener("click", closeDiff);
  el.addEventListener("mousedown", (e) => e.target === el && closeDiff());
  el.addEventListener("keydown", (e) => e.key === "Escape" && closeDiff());
}

// ---------- dev server links, skills and MCP servers (stats panel) ----------

async function refreshLinks() {
  if (!on("dev_links")) return;
  try {
    const list = (await Go().DevLinks()) || [];
    if (JSON.stringify(list) !== JSON.stringify(state.links)) {
      state.links = list;
      renderStats();
    }
  } catch {}
}

function openLink(url) {
  if (!REMOTE) return rt().BrowserOpenURL(url);
  if (!canChange()) return watchOnly();
  Go().OpenOnMac(url).then(() => flash(`opened ${url.replace(/^https?:\/\//, "")} on the Mac`), (e) => flash(String(e)));
}

async function refreshTools(force) {
  const path = state.current || state.selected;
  if (!path || !on("skills")) return;
  const t = state.tools[path];
  if (t && !force && Date.now() - t.at < 30000) return;
  state.tools[path] = { skills: [], mcp: [], ...t, at: Date.now() };
  try {
    const v = await Go().ProjectTools(path);
    state.tools[path] = { ...v, at: Date.now() };
    renderStats();
  } catch {}
}

function wireStatsExtras() {
  const el = $("#stats");
  el.addEventListener("click", (e) => {
    const d = e.target.closest("[data-diff]"), l = e.target.closest("[data-link]");
    if (d) showDiff(state.statsPath, d.dataset.diff);
    if (l) openLink(l.dataset.link);
  });
  el.addEventListener("toggle", (e) => {
    const sec = e.target.dataset?.sec;
    if (sec) state.openSec[sec] = e.target.open;
  }, true);
}

// ---------- which session you are looking at (no phone alert for it) ----------

let viewSent = { id: -1, at: 0 };
function reportViewing() {
  const cur = activeTab();
  const id = cur && cur.id && !cur.exited && document.visibilityState === "visible" && document.hasFocus() ? cur.id : 0;
  if (id === viewSent.id && Date.now() - viewSent.at < 30000) return;
  viewSent = { id, at: Date.now() };
  Promise.resolve(Go().SetViewing(id)).catch(() => {});
}

// ---------- phone alerts (Web Push) ----------

async function syncPush() {
  if (!REMOTE || !("Notification" in window) || Notification.permission !== "granted") return;
  if (on("web_push")) await remoteMod.enablePush().catch(() => {});
  else await remoteMod.disablePush().catch(() => {});
}

// ---------- polling ----------

async function loadProjects() {
  try {
    const [list0, cfgErr] = await Promise.all([Go().Projects(), Go().ConfigError()]);
    const list = list0 || [];
    const box = $("#config-error");
    box.classList.toggle("hidden", !cfgErr);
    if (cfgErr) box.innerHTML = `config.json has a mistake — ${esc(cfgErr)}<br><b>Click to fix it</b> (⌘,); Quarters reloads it when you save.`;
    state.projects = list;
    if (!state.selected || !project(state.selected)) state.selected = list[0]?.path || null;
    renderProjects();
    renderTitle();
  } catch (e) {
    flash(String(e));
  }
}

let statsBusy = false;
async function refreshStats() {
  const path = state.current || state.selected;
  if (!path || statsBusy) return;
  statsBusy = true;
  try {
    refreshTools();
    const s = await Go().Stats(path);
    if ((state.current || state.selected) === path) {
      state.stats = s;
      state.statsPath = path;
      renderStats();
    }
  } catch (e) {
    flash(String(e));
  } finally {
    statsBusy = false;
  }
}

// ---------- start ----------

// A failed call nobody waited for (a remote device's access, a config.json
// with a mistake…) is shown, not lost in the console.
window.addEventListener("unhandledrejection", (e) => {
  const msg = String(e.reason?.message || e.reason || "").replace(/^Error: /, "");
  if (msg && msg !== "connection lost") flash(msg);
});

async function main() {
  await Promise.all([
    document.fonts.load('13px "JetBrains Mono"'),
    document.fonts.load('bold 13px "JetBrains Mono"'),
    document.fonts.load('13px "Quarters Symbols"', "⏺"),
  ]).catch(() => {});

  const layout = await Go().Layout();
  state.collapsed = new Set(layout.collapsed || []);
  state.left = layout.left_width || DEFAULT_LEFT;
  state.right = layout.right_width || DEFAULT_RIGHT;
  applyLayout();

  wireEvents();
  wireResize();
  wireActiveList();
  wireProjects();
  wireTabs();
  wirePalette();
  wireDoubleShift();
  wireStart();
  wireProjectDialog();
  wireAutomations();
  wireFileDrop();
  wireProjectDrag();
  wireMobile();
  wireSettings();
  wireMenus();
  wireHistory();
  wireDiff();
  wireStatsExtras();
  $("#restore").addEventListener("click", (e) => {
    const b = e.target.closest("[data-restore]");
    if (!b) return;
    if (b.dataset.restore === "all") doRestore();
    else {
      renderRestore([]);
      Go().DismissRestore();
    }
  });
  for (const ev of ["focus", "blur"]) window.addEventListener(ev, () => setTimeout(reportViewing, 50));
  document.addEventListener("visibilitychange", reportViewing);
  if (REMOTE) wireRemoteUI();
  else wireRemoteDialog();
  window.addEventListener("focus", claimActive);
  preloadMascot();
  $("#mascot").addEventListener("click", () => {
    cheerUntil = Date.now() + 2500;
    renderMascot();
  });
  setFocus("projects");

  state.me = REMOTE ? remoteMod.remote.client : await Go().ClientID();
  await loadSettings();
  await loadProjects();
  render();
  await syncSessions();
  refreshStats();
  checkRestore();
  // opened by tapping a phone alert while the app was closed
  if (location.hash.startsWith("#open=")) {
    openNotifiedTab(decodeURIComponent(location.hash.slice(6)));
    history.replaceState(null, "", location.pathname);
  }

  renderMascot();
  setInterval(renderMascot, 500);
  setInterval(checkAttention, 500);
  setInterval(() => {
    renderTitle();
    renderProjects();
    if (Object.keys(state.tabs).length) renderTabs();
    renderActiveList();
  }, 1000);
  setInterval(refreshStats, 2000);
  setInterval(refreshLinks, 4000);
  setInterval(reportViewing, 10000);
  refreshAccount();
  setInterval(refreshAccount, 30000);
  setInterval(loadProjects, 3000);
}

main();

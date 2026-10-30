import { Terminal } from "./vendor/xterm.js";
import { FitAddon } from "./vendor/addon-fit.js";
import { WebglAddon } from "./vendor/addon-webgl.js";
import { WebLinksAddon } from "./vendor/addon-web-links.js";
import { Unicode11Addon } from "./vendor/addon-unicode11.js";
import { layoutFor, screenClass } from "./layout.js";

// In a browser (remote access) the Mac app's bridge comes over a WebSocket.
const REMOTE = !window.go && location.protocol.startsWith("http");
// APPLE: a Mac, iPhone or iPad keyboard, with ⌘ (see wireBrowserKeys)
const APPLE = /mac|iphone|ipad|ipod/i.test(navigator.userAgentData?.platform || navigator.platform || navigator.userAgent);
const remoteMod = REMOTE ? await import("./remote.js") : null;

const Go = () => window.go.main.App;
const rt = () => window.runtime;
const $ = (s) => document.querySelector(s);

const BUSY_MS = 1500;
const DEFAULT_LEFT = 290;
const DEFAULT_RIGHT = 330;
const DEFAULT_FILES = 260;
const FILES_KEY = "quarters.filesWidth";
const FONT_KEY = "quarters.fontSize";
// Which side panels show is remembered on each device.
const PANELS_KEY = "quarters.panels";
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
  beforeFocus: null, // focus mode: which panels were showing
  showRight: true,
  showFiles: true, // the files panel (⇧⌘E)
  files: Number(localStorage.getItem(FILES_KEY)) || DEFAULT_FILES, // its width
  fileTabs: {}, // path -> [file open in an editor tab] (this window only)
  fileOn: {}, // path -> key of the file tab in view ("" = the terminals)
  stats: null,
  statsPath: null,
  account: null,
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
  queues: {}, // session id -> prompts waiting to be sent
  usage: null, // tokens per project (Usage)
  instructions: {}, // path -> { files, notes, at }
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

// The window and its terminals follow macOS light / dark mode, unless
// Settings → Appearance picks one (on this device only).
const THEME_KEY = "quarters.theme";
const lightMode = matchMedia("(prefers-color-scheme: light)");
let theme = "system";
try {
  theme = localStorage.getItem(THEME_KEY) || "system";
} catch {}
const isLight = () => theme === "light" || (theme === "system" && lightMode.matches);
const termTheme = () => (isLight() ? lightTheme : darkTheme);

// the top bar's theme button goes round: system → light → dark
const THEME_NEXT = { system: "light", light: "dark", dark: "system" };
const THEME_ICONS = {
  system: '<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.5"/><path d="M8 2.5a5.5 5.5 0 0 1 0 11z" fill="currentColor" stroke="none"/></svg>',
  light: '<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="2.8"/><path d="M8 1.5v1.6M8 12.9v1.6M1.5 8h1.6M12.9 8h1.6M3.4 3.4l1.1 1.1M11.5 11.5l1.1 1.1M3.4 12.6l1.1-1.1M11.5 4.5l1.1-1.1"/></svg>',
  dark: '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M13.5 9.8A5.5 5.5 0 0 1 6.2 2.5a5.5 5.5 0 1 0 7.3 7.3z"/></svg>',
};

function applyTheme() {
  const root = document.documentElement;
  if (theme === "system") delete root.dataset.theme;
  else root.dataset.theme = theme;
  // the browser's own bar (phones, installed web app) takes the same colour
  for (const m of document.querySelectorAll('meta[name="theme-color"]')) {
    m.dataset.media ??= m.getAttribute("media");
    const forDark = m.dataset.media.includes("dark");
    m.setAttribute("media", theme === "system" ? m.dataset.media : (theme === "dark") === forDark ? "all" : "not all");
  }
  for (const list of Object.values(state.tabs)) for (const t of list) t.term.options.theme = termTheme();
  if (state.projects.length) renderTabs(); // project colours are lighter or darker
  const b = document.getElementById("btn-theme");
  if (b) {
    b.innerHTML = THEME_ICONS[theme];
    b.title = `Theme: ${theme === "system" ? "follows the system" : theme} · click for ${THEME_NEXT[theme] === "system" ? "the system's" : THEME_NEXT[theme]}`;
  }
}


function setTheme(v) {
  theme = ["light", "dark"].includes(v) ? v : "system";
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {}
  applyTheme();
}
lightMode.addEventListener("change", applyTheme);
applyTheme();

// ---------- helpers ----------

// setText and setHTML change an element only when the content differs:
// writing the same text again still makes the browser lay the page out.
function setText(el, text) {
  text = String(text);
  if (el && el.textContent !== text) el.textContent = text;
}

function setHTML(el, html) {
  if (el && el.dataset.html !== html) {
    el.innerHTML = html;
    el.dataset.html = html;
  }
}

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

// An icon is an image's full path, or an emoji (which has no "/").
const emojiIcon = (icon) => !!icon && !icon.includes("/");

function loadIcon(path, then) {
  if (!path || emojiIcon(path) || path in iconData) return;
  iconData[path] = "";
  Go().IconData(path)
    .then((url) => (iconData[path] = url))
    .catch(() => (iconData[path] = null))
    .finally(then || (() => { renderProjects(); renderTitle(); if (!activeTab()) renderStart(project(state.current)); }));
}

function nameHue(name) {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return h % 360;
}

// A letter tile in a colour picked from the name, for projects without an icon.
function monogram(name) {
  const letter = ([...name.replace(/[^\p{L}\p{N}]/gu, "")][0] || "?").toUpperCase();
  return `<span class="picon mono" style="background:hsl(${nameHue(name)} 38% 42%)">${esc(letter)}</span>`;
}

// A project's colour marks its tabs and the terminal's frame, so windows
// are told apart at a glance: the one chosen (✎ edit project), else the
// hue of its letter tile.
function projectColor(p) {
  if (!p) return "";
  if (p.color === "accent") return "var(--accent)";
  if (p.color) return p.color;
  return `hsl(${nameHue(p.name)} 50% ${isLight() ? 44 : 66}%)`;
}

const PROJECT_COLORS = ["#e0605a", "#e5b454", "#7cc48a", "#5fbfc2", "#6f9cf0", "#a98bf2", "#e27fb1", "#9c9aa8"];

function iconHTML(p) {
  if (emojiIcon(p.icon)) return `<span class="picon emo">${esc(p.icon)}</span>`;
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
  imageCache[key] ??= Go().PastedImage(tab.path, n, tab.started);
  imageCache[key].catch(() => delete imageCache[key]);
  popImage(key, `Image #${n}`, imageCache[key], e);
}

// popImage shows a picture (a promise of its data: URL) in a pop-up by the pointer.
function popImage(key, label, url, e) {
  imagePopFor = key;
  const pop = $("#img-pop");
  pop.innerHTML = `<div class="ip-cap muted">${esc(label)} · loading…</div>`;
  pop.classList.remove("hidden");
  placeImagePop(e.clientX, e.clientY);
  url.then(
    (src) => {
      if (imagePopFor !== key) return;
      const img = document.createElement("img");
      img.alt = "";
      img.onload = () => placeImagePop(e.clientX, e.clientY);
      img.src = src;
      const cap = document.createElement("div");
      cap.className = "ip-cap muted";
      cap.textContent = label;
      pop.replaceChildren(img, cap);
    },
    (err) => {
      if (imagePopFor === key) pop.innerHTML = `<div class="ip-cap muted">${esc(label)} · ${esc(String(err).replace(/^Error: /, ""))}</div>`;
    },
  );
}

// pathImageLinks makes the image files a terminal shows (an agent's
// "…/scratchpad/sample.png", or a shell's) into links: hovering shows the
// picture, a click (or a tap) opens it full screen. A path the program
// wrapped onto the next row is still one link.
function pathImageLinks(term, tabOf) {
  return {
    provideLinks(y, done) {
      const { text, at } = wrappedLine(term, y);
      const links = [];
      for (const m of text.matchAll(IMG_PATH)) {
        const start = at[m.index], end = at[m.index + m[0].length - 1];
        if (!start || !end || y < start.y || y > end.y) continue;
        const path = m[0];
        links.push({
          text: path,
          range: { start, end },
          decorations: { pointerCursor: true, underline: true },
          activate: () => openTabImage(tabOf(), path),
          hover: (e) => isTouch() || popImage(`${tabOf().id}|${path}`, baseName(path), tabImage(tabOf(), path), e),
          leave: hideImagePop,
        });
      }
      done(links);
    },
  };
}

// wrappedLine is row y of a terminal with the rows it wraps onto (one
// logical line), and the cell each of its characters is in.
function wrappedLine(term, y) {
  const buf = term.buffer.active;
  let top = y - 1, bottom = y - 1;
  while (top > 0 && buf.getLine(top)?.isWrapped) top--;
  while (buf.getLine(bottom + 1)?.isWrapped) bottom++;
  let text = "";
  const at = [];
  for (let r = top; r <= bottom; r++) {
    const line = buf.getLine(r);
    for (let x = 0; line && x < line.length; x++) {
      const cell = line.getCell(x);
      if (!cell || cell.getWidth() === 0) continue; // the right half of a wide character
      const ch = cell.getChars() || " ";
      for (let k = 0; k < ch.length; k++) at.push({ x: x + 1, y: r + 1 });
      text += ch;
    }
  }
  return { text, at };
}

// Where compilers, linters and stack traces say a problem is: path:line,
// path:line:col, path(line,col), and Python's File "path", line n.
const FILE_LINE = /(?:File "([^"\n]+)", line (\d+))|((?:[A-Za-z]:)?[\w@~.+\-\/]*[\w@+\-]\.[A-Za-z0-9]{1,10})(?::(\d+)(?::(\d+))?|\((\d+),(\d+)\))/g;
const fileSeen = new Map(); // "dir|rel" -> Promise<bool>: the file is in the project

// fileLinks makes file:line in a terminal a link (when the file is in the
// tab's folder): ⌘-click (a tap, on a phone) opens it in the editor at the line.
function fileLinks(term, tabOf) {
  return {
    provideLinks(y, done) {
      const tab = tabOf();
      const { text, at } = wrappedLine(term, y);
      const found = [];
      for (const m of text.matchAll(FILE_LINE)) {
        const start = at[m.index], end = at[m.index + m[0].length - 1];
        if (!start || !end || y < start.y || y > end.y) continue;
        const file = m[1] || m[3], line = +(m[2] || m[4] || m[6]);
        if (!line || /^https?:|:\/\//.test(file)) continue;
        found.push({ m, start, end, file, line });
      }
      if (!found.length || !tab) return done(undefined);
      const dir = tab.startOpts?.cwd || tab.path;
      Promise.all(found.map(async (f) => {
        let rel = f.file.replace(/^\.\//, "");
        if (rel.startsWith(dir + "/")) rel = rel.slice(dir.length + 1);
        if (rel.startsWith("/") || rel.startsWith("~")) return null; // outside the project
        const key = `${dir}|${rel}`;
        if (!fileSeen.has(key)) fileSeen.set(key, Go().FileStamp(dir, rel).then(() => true, () => false));
        if (fileSeen.size > 500) fileSeen.delete(fileSeen.keys().next().value);
        return (await fileSeen.get(key)) ? { ...f, rel } : null;
      })).then((list) =>
        done(list.filter(Boolean).map((f) => ({
          text: f.m[0],
          range: { start: f.start, end: f.end },
          decorations: { pointerCursor: true, underline: true },
          activate: (e) => (e.metaKey || e.ctrlKey || isTouch()) && openFileAt(tab.path, dir, f.rel, f.line),
          hover: (e) => isTouch() || showLinkTip(`${APPLE || !REMOTE ? "⌘" : "Ctrl"}-click to open ${baseName(f.rel)} at line ${f.line}`, e),
          leave: hideLinkTip,
        }))),
      );
    },
  };
}

function showLinkTip(text, e) {
  const tip = $("#link-tip");
  tip.textContent = text;
  tip.style.left = `${Math.min(e.clientX + 12, innerWidth - 260)}px`;
  tip.style.top = `${e.clientY + 16}px`;
  tip.classList.remove("hidden");
}

function hideLinkTip() {
  $("#link-tip").classList.add("hidden");
}

// openFileAt opens a file of a project (dir: the project or one of its
// worktrees) in the editor at line.
function openFileAt(path, dir, rel, line) {
  if (state.current !== path) openProject(path);
  return fileView().then((m) => m.openFile(path, dir, rel, line));
}

// ---------- send a selection to the agent (⇧⌘L) ----------

// agentTabOf is the agent tab a selection goes to: the one in view if it's
// an agent's, else the project's most recently used one.
function agentTabOf(path) {
  const cur = activeTab();
  if (cur && cur.path === path && cur.kind !== "shell" && !cur.autoKey && !cur.exited) return cur;
  return tabsOf(path).filter((t) => t.kind !== "shell" && !t.autoKey && !t.exited).sort((a, b) => (b.lastInput || 0) - (a.lastInput || 0))[0] || null;
}

// selectionToSend is what ⇧⌘L sends: the editor's selection (with its file
// and lines), else the terminal's.
async function selectionToSend() {
  const f = shownFile();
  if (f) {
    const sel = (await fileView()).selectionOf(f);
    if (sel) return `${sel.rel}:${sel.from}${sel.to > sel.from ? `-${sel.to}` : ""}\n\`\`\`\n${sel.text}\n\`\`\` `;
  }
  // no new line at the end: a program without bracketed paste would take it as Enter
  const t = activeTab();
  const text = t ? t.term.getSelection().trim() : "";
  return text ? `\`\`\`\n${text}\n\`\`\` ` : "";
}

async function sendToAgent() {
  const path = state.current;
  if (!path) return flash("open a project first");
  const text = await selectionToSend();
  if (!text) return flash("select some text first: output in a terminal, or lines in the editor");
  const tab = agentTabOf(path);
  if (!tab) return flash(`start ${shortAgent(project(path)?.agent)} first (C), then send it`);
  if (!canType()) return watchOnly();
  activeTab()?.term.clearSelection();
  goToTab(tab);
  claimTab(tab);
  tab.lastInput = Date.now();
  tab.term.paste(text); // typed in, not sent: say what to do with it
  if (!isTouch()) tab.term.focus();
  $("#send-pill").classList.add("hidden");
}

// The pill offers it while text is selected in a terminal and there's an
// agent tab to send it to.
function updateSendPill(tab) {
  const pill = $("#send-pill");
  const show = tab === activeTab() && tab.term.hasSelection() && tab.term.getSelection().trim().length > 0 && !!agentTabOf(tab.path) && canType();
  pill.classList.toggle("hidden", !show);
  if (show) {
    const r = tab.el.getBoundingClientRect();
    pill.style.left = `${r.right - 16}px`;
    pill.style.top = `${r.bottom - 16}px`;
  }
}

// tabImage is an image file a tab showed, as a data: URL (kept once loaded).
const tabImageCache = {}; // "tab id|path" -> Promise of a data: URL
function tabImage(tab, path) {
  const key = `${tab.id}|${path}`;
  return (tabImageCache[key] ??= Go().TabImage(tab.id, path));
}

// openTabImage shows an image file a tab showed full screen.
function openTabImage(tab, path) {
  hideImagePop();
  tabImage(tab, path).then(
    (url) => showImageView(url, baseName(path)),
    (err) => flash(`${baseName(path)}: ${String(err).replace(/^Error: /, "")}`),
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
    state.fileOn[path] = ""; // the new tab, not a file
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
  term.registerLinkProvider(pathImageLinks(term, () => tab));
  term.registerLinkProvider(fileLinks(term, () => tab));
  const unicode = new Unicode11Addon();
  term.loadAddon(unicode);
  term.unicode.activeVersion = "11";

  const meta = adopt ? adopt.meta : null;
  const autoOpts = opts.autoKey ? { title: opts.title, dir: opts.dir, command: opts.command, autoKey: opts.autoKey, group: opts.group } : null;
  const startOpts = { resume: opts.resume || "", cwd: opts.cwd || "", title: opts.title || "" };
  const harness = kind === "shell" || opts.autoKey ? "" : meta ? meta.harness || "claude" : kind === "resume" ? "claude" : (p && p.agent) || "claude";
  const tab = { key: meta ? meta.key : newKey(), started: meta ? meta.started : Date.now(), group: opts.group || null, head, autoOpts, startOpts, id: 0, kind, path, harness, term, fit, el, title: "", fixedTitle: opts.title || "", wt: opts.cwd && opts.cwd !== path ? opts.cwd.split("/").pop() : "", autoKey: opts.autoKey || "", lastData: 0, exited: false, code: 0, owner: adopt ? adopt.owner : state.me, pty: adopt && adopt.cols ? { cols: adopt.cols, rows: adopt.rows } : null, off: 0, synced: false, early: [] };
  state.byKey[tab.key] = tab;
  const list = (state.tabs[path] = tabsOf(path).concat(tab));
  if (!adopt) state.active[path] = list.length - 1;
  if (opts.splitOf) splitTree(tab.group, opts.splitOf, tab.key, opts.splitDir);
  if (opts.takeOver) tab.group.tree = renameLeaf(tab.group.tree, opts.takeOver, tab.key);
  showActive();

  term.open(body);
  if (el.classList.contains("active")) useGL(tab);
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
  term.onSelectionChange(() => updateSendPill(tab));
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
    if (REPORTS.test(d)) return canType() && !isPaused(tab) && !tab.exited && Go().Write(tab.id, d);
    if (!canType()) return watchOnly();
    tab.lastInput = Date.now();
    if (isPaused(tab)) claimTab(tab);
    if (!tab.exited) Go().Write(tab.id, stickyCtrl(d));
  });
  term.onBinary((d) => canType() && !isPaused(tab) && !tab.exited && Go().Write(tab.id, d)); // old-style mouse reports
  let resizing = 0;
  term.onResize(() => {
    clearTimeout(resizing); // dragging a panel resizes many times: send the size it settles on
    resizing = setTimeout(() => {
      if (tab.owner !== state.me || tab.exited) return;
      const size = `${term.cols}x${term.rows}`;
      if (tab.sent && tab.sent !== size && !mirrors(tab)) clearTerm(term); // Claude redraws at the new size
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

// clearTerm empties a terminal, its scrollback too, but keeps the modes the
// program set: a full-screen one (Claude Code scrolling its own history)
// turns on the alternate screen and mouse reporting once, as it starts, and
// term.reset() would lose them for good.
const clearTerm = (term) => term.write("\x1b[0m\x1b[r\x1b[H\x1b[2J\x1b[3J");

// REPORTS is what a terminal sends by itself, not typing: mouse reports
// (moves, clicks, the wheel, when the program asked for them) and focus in
// or out. They never take a session over from another window.
const REPORTS = /^(?:\x1b\[<\d+;\d+;\d+[Mm]|\x1b\[M[\s\S]{3}|\x1b\[[IO])+$/;

const newKey = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;

// What other windows need to rebuild a split automation's group.
const groupMeta = (g) => g && { id: g.id, name: g.name, autoName: g.autoName, path: g.path, layout: g.layout, cols: g.cols || null, rows: g.rows || null, tree: g.tree || null };

function groupFor(g) {
  if (!g || !g.id) return null;
  const have = state.groups[g.id];
  if (have && g.tree) have.tree = g.tree; // a split's newest pane knows the latest tree
  return have || (state.groups[g.id] = { ...g, focus: null });
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

// ---------- how the tab in view is shown (layout.js decides) ----------

// harnessOf is a tab's coding agent as Settings describes it (its adapter:
// interrupt key, menu marker, conversation reader); null for a shell or an
// automation.
const harnessOf = (tab) => (tab && !mirrors(tab) && (state.settings?.harnesses || []).find((h) => h.id === tab.harness)) || null;

// The transcript chosen on this device (▤ / 💬), per agent: "chat",
// "terminal" or "" (the default). Claude Code's keeps its old key.
const VIEW_KEY = "quarters.phoneView";
const viewKey = (agent) => (agent === "claude" ? VIEW_KEY : `${VIEW_KEY}.${agent}`);
function viewFor(tab) {
  try {
    return (tab && localStorage.getItem(viewKey(tab.harness))) || "";
  } catch {
    return "";
  }
}

function currentLayout(tab = activeTab()) {
  const t = tab && { agent: !mirrors(tab), reader: !!harnessOf(tab)?.reader };
  return layoutFor(t, screenClass({ narrow: isNarrow(), touch: isTouch() }), viewFor(tab));
}

// applySessionView shows the tab in view as layoutFor says: the message card
// (with the tab tools) or a plain terminal, and the chat or the terminal.
// The terminal refits by itself when the card comes or goes (#terms is
// observed).
function applySessionView() {
  const tab = activeTab(), l = currentLayout(tab);
  document.body.classList.toggle("card-on", l.card);
  // an agent whose menus can't be tapped needs the key bar's arrows and ↵,
  // whatever the key bar setting says (like a shell)
  document.body.classList.toggle("needs-keys", l.card && !harnessOf(tab)?.menuMarker);
  placeTools(l.toolsIn);
  $("#compose-text").placeholder = tab && !mirrors(tab) ? `Message ${shortAgent(tab.harness)}…` : "Type a command…";
  $("#btn-view").classList.toggle("hidden", !(l.card && harnessOf(tab)?.reader));
  updateSend(tab);
  renderChat();
}

// updateSend makes the card's button ■ (stop) while the agent works and
// nothing is typed, ↑ (send) otherwise.
function updateSend(tab = activeTab()) {
  const later = !!tab && !tab.exited && !mirrors(tab) && tabStatus(tab) === "busy" && !!$("#compose-text").value.trim() && on("prompt_queue");
  $("#compose-later").classList.toggle("hidden", !later);
  const stop = !!tab && !tab.exited && !mirrors(tab) && tabStatus(tab) === "busy" && !$("#compose-text").value;
  const b = $("#compose-send");
  if (b.dataset.stop === String(stop)) return;
  b.dataset.stop = String(stop);
  b.textContent = stop ? "■" : "↑";
  b.setAttribute("aria-label", stop ? `Stop ${tab ? shortAgent(tab.harness) : ""}` : "Send");
}

// Shells and automations mirror live in every window. A Claude tab is
// drawn by the window in use only; others pause, and catch up when used
// (Claude reprints the whole conversation for the new size).
const mirrors = (tab) => tab.kind === "shell" || !!tab.autoKey;
// Claude Code tabs get Claude's extras in the side panel (its plan,
// Remote Control, session stats); on a phone every agent looks the same.
const isClaude = (tab) => !!tab && !mirrors(tab) && tab.harness === "claude";
// agentName is what a project's agent (or a tab's) is called.
function agentName(id) {
  const h = (state.settings?.harnesses || []).find((x) => x.id === id);
  return h ? h.name : id === "claude" || !id ? "Claude Code" : id;
}
const shortAgent = (id) => (id === "claude" || !id ? "Claude" : agentName(id));
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
  // Claude redraws for this size (a claim always makes it): the whole
  // conversation, or in full-screen mode its screen, so start from empty.
  if (!mirrors(tab)) clearTerm(tab.term);
  setPaused(tab, false);
  tab.sent = `${tab.term.cols}x${tab.term.rows}`;
  Go().Claim(tab.id, tab.term.cols, tab.term.rows);
}

// On a touch screen, an agent's menus (trust this folder? allow this
// tool?) are answered by tapping, where Quarters knows the mark the agent
// puts on the chosen option (Claude Code's ❯): a tap on an option moves
// the mark to it, a tap on the chosen one presses Enter. Other agents'
// menus work with the key bar's arrows and ↵.
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
    const marker = harnessOf(tab)?.menuMarker;
    const keys = marker ? menuKeys(tab.term, t.clientY, marker) : null;
    if (!keys || !canType() || tab.exited) return;
    e.preventDefault(); // no click: the keyboard stays down
    claimTab(tab);
    tab.lastInput = Date.now();
    Go().Write(tab.id, keys);
  }, { passive: false });
}

// claudeBoxTop is the row of the rule above Claude Code's input box at the
// bottom of the screen (a rule, the ❯ line, a rule, its status lines), or
// -1 when it isn't there (Claude is showing a menu instead). The chat view
// uses it to tell a menu from the input box.
function claudeBoxTop(term) {
  const buf = term.buffer.active, end = lastLine(term);
  const text = (y) => buf.getLine(buf.baseY + y)?.translateToString(true) ?? "";
  for (let y = end; y >= Math.max(0, end - 8); y--)
    if (/^\s*─{8,}/.test(text(y)) && /^\s*[❯>]/.test(text(y + 1))) return y;
  return -1;
}

// lastLine is the last row of the screen with something on it: Claude
// draws its input box or a menu last, so that is where they end.
function lastLine(term) {
  const buf = term.buffer.active;
  for (let y = term.rows - 1; y > 0; y--) if ((buf.getLine(buf.baseY + y)?.translateToString(true) ?? "").trim()) return y;
  return 0;
}

// menuKeys is what to type for a tap at clientY: arrows to move the menu's
// marker (❯) to the tapped option, or Enter on the chosen one; null if the
// tap isn't on a menu option.
function menuKeys(term, clientY, marker) {
  const screen = term.element?.querySelector(".xterm-screen");
  if (!screen) return null;
  const r = screen.getBoundingClientRect();
  const row = Math.floor((clientY - r.top) / (r.height / term.rows));
  if (row < 0 || row >= term.rows) return null;
  const buf = term.buffer.active, top = buf.viewportY;
  const text = (y) => buf.getLine(top + y)?.translateToString(true) ?? "";
  // the chosen option: "❯ Yes, …" (the input box's bare "❯" has no text after it)
  const chosen = new RegExp(`^(\\s*)${marker.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s+(?=\\S)`, "u");
  let sel = -1, col = -1;
  for (let y = 0; y < term.rows; y++) {
    const m = chosen.exec(text(y));
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
  const group = groupFor(m.group);
  // a tab split after this window showed it joins the split
  if (isTree(group)) for (const k of leaves(group.tree)) if (state.byKey[k] && !state.byKey[k].group) state.byKey[k].group = group;
  await newTab(m.path, m.kind || "claude", { adopt: info, title: m.title, dir: m.dir, command: m.command, autoKey: m.autoKey, group, resume: m.resume, cwd: m.cwd, background: true });
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
  await loadQueues();
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
  const shown = list[Math.min(state.active[tab.path] || 0, list.length - 1)];
  if (tab.id && !gone) Go().CloseSession(tab.id);
  delete state.byId[tab.id];
  delete state.byKey[tab.key];
  dropGL(tab);
  tab.term.dispose();
  tab.el.remove();
  list.splice(i, 1);
  const near = isTree(tab.group) ? leaveTree(tab) : null;
  if (!list.length) delete state.tabs[tab.path];
  state.active[tab.path] = Math.max(0, Math.min(state.active[tab.path] || 0, list.length - 1));
  // a pane of a split hands the focus to the pane beside it; closing another tab doesn't move it
  const keep = shown === tab ? near : shown;
  if (keep && list.includes(keep)) state.active[tab.path] = list.indexOf(keep);
  if (!silent) showActive();
  render();
}

async function requestCloseEntry(k) {
  const n = entries(state.current).length;
  if (k >= n) return closeFileTab(fileTabsOf(state.current)[k - n]);
  const e = entries(state.current)[k];
  if (!e) return;
  if (!e.group) return requestCloseTab(e.tabs[0]);
  const live = e.tabs.filter((t) => !t.exited).length;
  const ask = isTree(e.group) ? ["Close all panes?", `This ends the ${live} session(s) running in them.`] : [`Close “${e.group.name}”?`, `This ends its ${live} running shell(s).`];
  if (live && !(await Go().Confirm(...ask, "Close all"))) return;
  for (const t of e.tabs) closeTab(t, true);
  showActive();
  render();
}

async function requestCloseTab(tab) {
  if (!tab) return;
  const name = tab.kind === "shell" || tab.autoKey ? "shell" : agentName(tab.harness);
  if (tab.exited || (await Go().Confirm("Close tab?", `This ends the ${name} session running in this tab.`, "Close"))) closeTab(tab);
}

async function restartTab(tab) {
  const { path, kind } = tab;
  if (isTree(tab.group)) {
    const fresh = await newTab(path, kind === "claude" || kind === "resume" ? "continue" : kind, { cwd: tab.startOpts.cwd, title: tab.startOpts.title, group: tab.group, takeOver: tab.key });
    if (fresh) {
      tab.group.focus = fresh;
      closeTab(tab);
    }
    return;
  }
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

// A terminal drawn with WebGL holds a GPU context, and WebKit allows about
// 16: past that it takes the oldest one away, and that terminal draws with
// the much slower DOM renderer from then on. So only the terminals on
// screen, and the few shown most recently (switching back stays instant),
// keep one. Phones and tablets always use the DOM renderer: dependable in
// mobile browsers and lighter on the battery.
const GL_KEEP = 6;
let glTabs = []; // tabs drawn with WebGL, the most recently shown last

function useGL(tab) {
  if (isTouch() || !tab.term.element) return;
  glTabs = glTabs.filter((t) => t !== tab).concat(tab);
  if (!tab.gl) {
    try {
      const gl = new WebglAddon();
      gl.onContextLoss(() => dropGL(tab));
      tab.term.loadAddon(gl);
      tab.gl = gl;
    } catch {
      /* falls back to the DOM renderer */
    }
  }
  for (const t of glTabs.slice(0, Math.max(0, glTabs.length - GL_KEEP)))
    if (!t.el.classList.contains("active")) dropGL(t);
}

function dropGL(tab) {
  glTabs = glTabs.filter((t) => t !== tab);
  if (!tab.gl) return;
  const gl = tab.gl;
  tab.gl = null;
  try {
    gl.dispose();
  } catch {}
}

function showActive() {
  const cur = activeTab();
  const panes = isSplit(cur) ? tabsOf(cur.path).filter((t) => t.group === cur.group) : cur ? [cur] : [];
  const terms = $("#terms");
  const split = panes.length > 1;
  const tree = split && isTree(cur.group) && !isNarrow();
  terms.classList.toggle("split", split);
  terms.classList.toggle("tree", tree);
  if (split) applySplitSizes(cur.group, panes.length);
  if (!split || tree) terms.style.gridTemplateColumns = terms.style.gridTemplateRows = "";
  document.querySelectorAll(".term").forEach((el) => {
    const i = panes.findIndex((t) => t.el === el);
    if (!tree || i < 0) el.style.left = el.style.top = el.style.width = el.style.height = "";
    el.classList.toggle("active", i >= 0);
    el.classList.toggle("focused-pane", split && !!cur && el === cur.el);
    el.style.order = i >= 0 ? String(i) : "";
  });
  const start = !cur && !shownFile() && state.current ? project(state.current) : null;
  const home = !cur && !start && !shownFile();
  const back = home && $("#welcome").classList.contains("hidden");
  if (back) newTagline(); // a new one each time home comes back
  $("#welcome").classList.toggle("hidden", !home);
  if (back) renderToday(true);
  renderStart(start);
  for (const t of panes) useGL(t);
  if (cur) {
    requestAnimationFrame(() => {
      for (const t of panes) {
        try {
          fitTab(t);
        } catch {}
      }
      if (state.focus === "terminal" && !$("#tab-list .tab-rename") && !isTouch() && !shownFile()) cur.term.focus();
      if (document.hasFocus()) claimTab(cur);
      placeSplitHandles();
    });
  } else placeSplitHandles();
  reportViewing();
  document.body.classList.toggle("has-tab", !!cur || fileTabsOf(state.current).length > 0);
  document.body.classList.toggle("one-tab", tabsOf(state.current).length + fileTabsOf(state.current).length <= 1); // phones: the title says which
  applySessionView();
  applyFileView();
  pollChat();
  renderTabs();
  renderHints();
}

// ---------- split panes: drag the lines between them ----------

const MIN_PANE = 140; // px

// track sizes: the saved ones when they fit the grid, else equal
const tracks = (saved, n) => (saved && saved.length === n ? saved : Array(n).fill(1));

function applySplitSizes(group, n) {
  if (isTree(group)) return void placeTree(group);
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
  if (isTree(cur.group)) return placeTreeHandles(cur.group);
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
  const [cols, rows] = gridFor(group.layout, panes.length);
  // current track sizes in px, read from the panes
  const px = axis === "col"
    ? Array.from({ length: cols }, (_, i) => panes[i].el.getBoundingClientRect().width)
    : Array.from({ length: rows }, (_, i) => panes[Math.min(i * cols, panes.length - 1)].el.getBoundingClientRect().height);
  dragLine(e, axis, px, k, (fr) => {
    if (axis === "col") group.cols = fr;
    else group.rows = fr;
    applySplitSizes(group, panes.length);
  }, () => saveSplitSizes(group));
}

// dragLine moves the line after track k (px: the tracks' sizes) with the
// mouse, handing set the new sizes (fr) as it goes and calling done at the end.
function dragLine(e, axis, px, k, set, done) {
  e.preventDefault();
  const start = axis === "col" ? e.clientX : e.clientY;
  document.body.classList.add("dragging", axis === "col" ? "drag-col" : "drag-row");
  const move = (ev) => {
    const pair = px[k] + px[k + 1];
    const a = Math.max(MIN_PANE, Math.min(pair - MIN_PANE, px[k] + (axis === "col" ? ev.clientX : ev.clientY) - start));
    const sizes = px.slice();
    sizes[k] = a;
    sizes[k + 1] = pair - a;
    const total = sizes.reduce((x, y) => x + y, 0);
    set(sizes.map((v) => Math.round((v / total) * sizes.length * 1000) / 1000));
    fitActive();
  };
  const up = () => {
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    document.body.classList.remove("dragging", "drag-col", "drag-row");
    placeSplitHandles();
    done();
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
}

function saveSplitSizes(group) {
  if (isTree(group)) return saveTree(group);
  if (!group.autoName) return;
  Go().SaveAutomationSizes(group.path, group.autoName, group.cols || [], group.rows || []).catch((err) => flash(String(err)));
  // keep the project's copy current, so the next run starts with these sizes
  const a = (project(group.path)?.automations || []).find((x) => x.name === group.autoName);
  if (a) {
    a.cols = group.cols;
    a.rows = group.rows;
  }
}

// ---------- splitting a tab by hand (⌘D, ⇧⌘D), as in iTerm ----------
//
// A split made by hand is a tree, kept in its group (layout "tree"): a pane
// is { key } (its tab's key), a split is { dir, kids, sizes } with dir "col"
// (side by side) or "row" (stacked). Every pane's session carries the tree,
// so a reloaded window puts the panes back where they were.

const GAP = 8; // px between panes and around them (#terms.split's padding)
const isTree = (g) => !!(g && g.layout === "tree");
const leaves = (n) => (!n ? [] : n.kids ? n.kids.flatMap(leaves) : [n.key]);

// splitPane opens a shell beside the tab in view (dir "col") or below it ("row").
async function splitPane(dir) {
  const cur = activeTab();
  if (!cur) return;
  if (cur.group && !isTree(cur.group)) return flash("an automation's panes are set in its settings");
  let g = cur.group;
  if (!g) {
    g = cur.group = { id: newKey(), name: "split", path: cur.path, layout: "tree", tree: { key: cur.key }, focus: cur };
    state.groups[g.id] = g;
  }
  const tab = await newTab(cur.path, "shell", { cwd: cur.startOpts.cwd, group: g, splitOf: cur.key, splitDir: dir });
  if (tab) g.focus = tab;
  saveTree(g);
}

// splitTree puts pane key next to pane of, in a split along dir: a new one,
// or the split of already shares that direction (of gives up half its room).
function splitTree(g, of, key, dir) {
  const visit = (n) => {
    if (!n.kids) return n.key === of ? { dir, kids: [n, { key }], sizes: null } : n;
    const i = n.kids.findIndex((k) => k.key === of);
    if (i >= 0 && n.dir === dir) {
      const sizes = tracks(n.sizes, n.kids.length).slice();
      sizes.splice(i, 1, sizes[i] / 2, sizes[i] / 2);
      n.kids.splice(i + 1, 0, { key });
      n.sizes = sizes;
      return n;
    }
    n.kids = n.kids.map(visit);
    return n;
  };
  g.tree = visit(g.tree || { key: of });
}

const renameLeaf = (n, from, to) => (!n ? n : n.kids ? { ...n, kids: n.kids.map((k) => renameLeaf(k, from, to)) } : n.key === from ? { key: to } : n);

// pruneTree keeps the panes in keys where they are, folding a split left
// with one part into that part. Untouched, it is the same tree.
function pruneTree(n, keys) {
  if (!n) return null;
  if (!n.kids) return keys.has(n.key) ? n : null;
  const sizes = tracks(n.sizes, n.kids.length);
  const kept = n.kids.map((k, i) => [pruneTree(k, keys), sizes[i]]).filter(([k]) => k);
  if (!kept.length) return null;
  if (kept.length === 1) return kept[0][0];
  if (kept.length === n.kids.length && kept.every(([k], i) => k === n.kids[i])) return n;
  return { dir: n.dir, kids: kept.map(([k]) => k), sizes: kept.map(([, f]) => f) };
}

// neighbour is the pane next to key in its split (the one before it, if any).
function neighbour(n, key) {
  if (!n || !n.kids) return null;
  const i = n.kids.findIndex((k) => k.key === key);
  if (i >= 0) return i > 0 ? leaves(n.kids[i - 1]).at(-1) : leaves(n.kids[1])[0];
  for (const k of n.kids) {
    const near = neighbour(k, key);
    if (near) return near;
  }
  return null;
}

// leaveTree takes a closed pane out of its split and returns the pane to
// show instead. With one pane left the split ends: it's a plain tab again.
function leaveTree(tab) {
  const g = tab.group;
  const rest = tabsOf(tab.path).filter((t) => t.group === g && t !== tab);
  const near = state.byKey[neighbour(g.tree, tab.key)] || rest[0] || null;
  g.tree = pruneTree(g.tree, new Set(rest.map((t) => t.key)));
  if (rest.length === 1) {
    rest[0].group = null;
    delete state.groups[g.id];
    if (rest[0].id) Go().SetSessionGroup(rest[0].id, null).catch(() => {});
  } else saveTree(g);
  return near;
}

// saveTree hands the split's tree to each of its panes' sessions.
function saveTree(g) {
  for (const t of tabsOf(g.path)) if (t.group === g && t.id) Go().SetSessionGroup(t.id, groupMeta(g)).catch(() => {});
}

// treePanes is the tree of the split's panes here, and the panes in its
// order. A pane the tree has not heard of yet goes at the right.
function treePanes(g) {
  const panes = tabsOf(g.path).filter((t) => t.group === g);
  let tree = pruneTree(g.tree, new Set(panes.map((t) => t.key)));
  const known = new Set(leaves(tree));
  const more = panes.filter((t) => !known.has(t.key)).map((t) => ({ key: t.key }));
  if (more.length) tree = !tree && more.length === 1 ? more[0] : { dir: "col", kids: (tree ? [tree] : []).concat(more), sizes: null };
  return { tree, panes: leaves(tree).map((k) => state.byKey[k]) };
}

// layoutTree gives each pane its box in #terms (px) and lists the lines
// between the parts of each split, for the handles.
function layoutTree(n, box, out) {
  if (!n.kids) return void out.boxes.set(n.key, box);
  const col = n.dir === "col";
  const sizes = tracks(n.sizes, n.kids.length), total = sizes.reduce((a, b) => a + b, 0);
  const room = (col ? box.w : box.h) - GAP * (n.kids.length - 1);
  const lens = sizes.map((f) => (room * f) / total);
  let at = col ? box.x : box.y;
  n.kids.forEach((kid, i) => {
    layoutTree(kid, col ? { x: at, y: box.y, w: lens[i], h: box.h } : { x: box.x, y: at, w: box.w, h: lens[i] }, out);
    at += lens[i];
    if (i < n.kids.length - 1) out.lines.push({ node: n, k: i, lens, box, at: at + GAP / 2 });
    at += GAP;
  });
}

function placeTree(g) {
  const terms = $("#terms");
  const { tree, panes } = treePanes(g);
  const out = { boxes: new Map(), lines: [] };
  if (tree) layoutTree(tree, { x: GAP, y: GAP, w: terms.clientWidth - 2 * GAP, h: terms.clientHeight - 2 * GAP }, out);
  if (!terms.classList.contains("tree")) return out; // a phone shows one pane at a time
  for (const t of panes) {
    const b = out.boxes.get(t.key);
    if (b) Object.assign(t.el.style, { left: `${b.x}px`, top: `${b.y}px`, width: `${b.w}px`, height: `${b.h}px` });
  }
  return out;
}

function placeTreeHandles(g) {
  const terms = $("#terms");
  if (!terms.classList.contains("tree")) return;
  for (const line of placeTree(g).lines) {
    const axis = line.node.dir, { box } = line;
    const h = document.createElement("div");
    h.className = `split-handle ${axis}`;
    h.title = "Drag to resize · double-click to make these equal";
    if (axis === "col") Object.assign(h.style, { left: `${line.at}px`, top: `${box.y}px`, height: `${box.h}px`, bottom: "auto" });
    else Object.assign(h.style, { top: `${line.at}px`, left: `${box.x}px`, width: `${box.w}px`, right: "auto" });
    h.addEventListener("mousedown", (e) => dragLine(e, axis, line.lens, line.k, (fr) => {
      line.node.sizes = fr;
      placeTree(g);
    }, () => saveTree(g)));
    h.addEventListener("dblclick", () => {
      line.node.sizes = null;
      placeTree(g);
      saveTree(g);
      fitActive();
    });
    terms.appendChild(h);
  }
}

// cyclePane goes to the next (d = 1) or previous (-1) pane of a split.
function cyclePane(d) {
  const cur = activeTab();
  if (!isSplit(cur)) return;
  const panes = isTree(cur.group) ? treePanes(cur.group).panes : tabsOf(cur.path).filter((t) => t.group === cur.group);
  const next = panes[(panes.indexOf(cur) + d + panes.length) % panes.length];
  if (next) selectTab(tabsOf(cur.path).indexOf(next));
}

function selectTab(i) {
  const list = tabsOf(state.current);
  if (i < 0 || i >= list.length) return;
  state.active[state.current] = i;
  state.fileOn[state.current] = "";
  if (list[i].group) list[i].group.focus = list[i];
  setFocus("terminal");
  showActive();
}

// goToTab shows a tab of any project.
function goToTab(t) {
  if (state.current !== t.path) openProject(t.path);
  selectTab(tabsOf(t.path).indexOf(t));
}

// selectEntry picks the k-th entry of the tab bar (a split group returns to the pane last used).
function selectEntry(k) {
  const n = entries(state.current).length;
  if (k >= n) return showFileTab(fileTabsOf(state.current)[k - n]);
  const e = entries(state.current)[k];
  if (!e) return;
  const t = e.group && e.tabs.includes(e.group.focus) ? e.group.focus : e.tabs[0];
  selectTab(tabsOf(state.current).indexOf(t));
}

function currentEntry() {
  const cur = activeTab(), f = shownFile();
  if (f) return entries(state.current).length + fileTabsOf(state.current).indexOf(f);
  return entries(state.current).findIndex((e) => e.tabs.includes(cur));
}

function cycleTab(d) {
  const n = entries(state.current).length + fileTabsOf(state.current).length;
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

// goHome shows the home page in the middle: every session at a glance. The
// tabs keep running; opening a project shows them again.
function goHome() {
  if (!state.current) return;
  state.current = null;
  setFocus("projects");
  showActive();
  render();
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
  if (autos.length) flash(`▶ started ${autos.map((a) => a.name).join(", ")} · ${keyText("⌘T")} for ${shortAgent(p.agent)}`);
}

// renderStart fills the start screen for p (a project with no tabs), or hides it.
function renderStart(p) {
  const el = $("#start");
  el.classList.toggle("hidden", !p);
  if (!p) return;
  const sig = `${p.path}|${on("history")}|${p.agent}`;
  const icon = emojiIcon(p.icon) || iconData[p.icon] ? p.icon : "";
  if (el.dataset.path !== sig || el.dataset.icon !== icon) {
    el.dataset.path = sig;
    el.dataset.icon = icon;
    const autos = (p.automations || []).map((a, i) =>
      `<button class="btn" type="button" data-start="auto" data-i="${i}">▶ ${esc(a.name)}</button>`).join("");
    el.innerHTML = `
      <div class="st-project">${iconHTML(p)}<span class="st-name">${esc(p.name)}</span></div>
      <div class="st-path muted">${esc(p.shortPath)}${p.branch ? ` · <span class="violet">⎇ ${esc(p.branch)}</span>` : ""}${p.missing ? ` · <span class="red">folder not found</span>` : ""}</div>
      <button class="st-press" type="button" data-start="claude">
        <span class="keys-only">Press</span><b class="st-key">C</b><span class="keys-only">to start ${esc(shortAgent(p.agent))}</span><span class="touch-only">Start ${esc(shortAgent(p.agent))}</span>
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
    if (e.target.closest?.("input, textarea, .overlay, .xterm, .cm-editor")) return;
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
  renderHints();
}

// renderHints: the status bar's keys, the ones that fit what's in front of
// you (a shell: splitting it; a split: moving between its panes; a file:
// saving it).
function renderHints() {
  const tab = activeTab(), f = shownFile();
  // a browser on a Mac: Chrome keeps ⌘T and ⌘W, so ⌃ is added
  const k = (keys, what) => `<span><b>${keyText(REMOTE && APPLE ? keys.replace(/^(⇧?)⌘([TW])$/, "⌃$1⌘$2") : keys)}</b> ${what}</span>`;
  let list;
  if (state.focus === "projects" && state.selected) list = [k("↑↓", "move"), k("↵", "open"), k("/", "filter"), k("⇥", "back to the terminal"), k("⌘T", "new tab"), k("⌘B", "sidebar")];
  else if (f) list = [k("⌘S", "save"), k("⌘W", "close the file"), k("⇧⌘E", "files panel"), k("⌘1-9", "tabs"), k("⌘B", "sidebar")];
  else if (tab && isTree(tab.group)) list = [k("⌘[ ⌘]", "panes"), k("⌘D", "split right"), k("⇧⌘D", "split down"), k("⌘W", "close the pane"), k("⌘K", "clear"), k("⇧⌘↵", "focus mode")];
  else if (tab?.kind === "shell") list = [k("⌘D", "split right"), k("⇧⌘D", "split down"), k("⌘K", "clear"), k("⌘T", "new tab"), k("⌘W", "close tab"), k("⌘1-9", "tabs")];
  else if (tab) list = [k("⇧↵", "new line"), k("⌘D", "split"), k("⌘J", "who's waiting"), k("⌘T", "new tab"), k("⌘W", "close tab"), k("⌘1-9", "tabs"), k("⌘B", "sidebar"), k("⌘I", "stats")];
  else list = [k("⇧⇧ / ⌘P", "projects"), k("⌘T", "new tab"), k("⇧⌘T", "continue"), k("⌘B", "sidebar"), k("⌘I", "stats"), k("⌘,", "settings")];
  setHTML($("#hints"), `<button type="button" class="hint-btn" data-shortcuts title="All keyboard shortcuts (${keyText("⌘/")})"><b>${keyText("⌘/")}</b> shortcuts</button>${list.join("")}`);
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
        // with prompts queued it isn't your turn yet: the next one goes in
        if (worked && !t.exited && !tabSeen(t) && !(state.queues[t.id] || []).length) {
          t.unseen = true;
          t.doneAt = t.workLast;
          const p = project(t.path);
          Go().Notify(`${p ? p.name : shortAgent(t.harness)} · your turn`, t.title || `${agentName(t.harness)} is waiting for you`, tabKey(t));
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
  rt().EventsOn("queue", (q) => {
    if (q.items && q.items.length) state.queues[q.id] = q.items;
    else delete state.queues[q.id];
    renderStats();
  });
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
    if (!tab) return;
    // a pane of a split that ended cleanly closes, as in iTerm; a failure stays to be read
    if (!code && isTree(tab.group)) closeTab(tab);
    else markExited(tab, code);
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
  rt().EventsOn("projects", loadProjects);
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
  learned(action);
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
      if (shownFile()) closeFileTab(shownFile());
      else if (isSplit(cur) && !isTree(cur.group)) requestCloseEntry(currentEntry());
      else requestCloseTab(cur);
      break;
    case "split-right":
      splitPane("col");
      break;
    case "split-down":
      splitPane("row");
      break;
    case "next-pane":
      cyclePane(1);
      break;
    case "prev-pane":
      cyclePane(-1);
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
    case "home":
      goHome();
      break;
    case "search":
      ui("search.js").then((m) => m.openSearch());
      break;
    case "send-to-agent":
      sendToAgent();
      break;
    case "commit":
      if (!target) flash("open a project first");
      else if (!canChange()) watchOnly();
      else ui("commit.js").then((m) => m.openCommit(target));
      break;
    case "push":
      if (target && canChange()) Go().GitPush(target).then(flash, (e) => flash(String(e).replace(/^Error: /, ""))).finally(() => refreshStats());
      break;
    case "copy-summary":
      copySummary();
      break;
    case "palette":
      openPalette();
      break;
    case "add-project":
      addProject();
      break;
    case "edit-config":
      if (!REMOTE) Go().OpenConfig().catch((e) => flash(String(e)));
      else if (canChange()) ui("config.js").then((m) => m.openConfig());
      else flash("this device can't change settings");
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
      togglePanel("left");
      break;
    case "toggle-right":
      togglePanel("right");
      break;
    case "toggle-files":
      if (isNarrow()) openDrawer("files");
      else togglePanel("files");
      break;
    case "save-file":
      if (shownFile()) fileView().then((m) => m.saveFile(shownFile()));
      break;
    case "font-up":
      setFontSize(state.fontSize + 1);
      break;
    case "font-down":
      setFontSize(state.fontSize - 1);
      break;
    case "shortcuts":
      openShortcuts();
      break;
    case "next-waiting":
      nextWaiting();
      break;
    case "usage":
      if (on("usage")) openUsage();
      break;
    case "font-reset":
      setFontSize(defaultFontSize());
      break;
    case "commands":
      openPalette(">");
      break;
    case "focus-mode":
      toggleFocusMode();
      break;
    case "theme-system":
    case "theme-light":
    case "theme-dark":
      setTheme(action.slice(6));
      break;
    case "fold-sections":
    case "unfold-sections":
      for (const el of document.querySelectorAll("#stats .ps")) foldSection(el.dataset.ps, action === "fold-sections");
      break;
    default:
      if (action.startsWith("tab-")) selectEntry(Number(action.slice(4)) - 1);
  }
}

function setFontSize(n) {
  state.fontSize = Math.max(9, Math.min(24, n));
  localStorage.setItem(FONT_KEY, String(state.fontSize));
  for (const list of Object.values(state.tabs)) for (const t of list) t.term.options.fontSize = state.fontSize;
  document.documentElement.style.setProperty("--ed-fs", state.fontSize + "px");
  for (const list of Object.values(state.fileTabs)) for (const f of list) f.view.requestMeasure();
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

const dlg = { open: false, oldPath: "", autoName: true, icon: "", color: "", foundFor: "", autos: [] };

// ---- automations in the dialog ----

// What the dialog edits: each automation as written in config.json.
function autosForDialog(p) {
  return ((p && p.automations) || []).map((a) => ({
    name: a.name, layout: a.layout, auto: !!a.auto, cols: a.cols || null, rows: a.rows || null,
    at: (a.schedule && a.schedule.at) || "", days: (a.schedule && a.schedule.days) || [],
    was: `${a.layout}/${a.tabs.length}`, // sizes are kept only while the grid stays the same
    tabs: a.tabs.map((t) => ({ name: t.rawName || "", dir: t.rawDir || "", command: t.command || "" })),
  }));
}

// The automations to save; pane sizes are dropped when the grid changed.
function autosToSave(list = dlg.autos) {
  return list.map((a) => {
    const tabs = a.tabs.filter((t) => t.name.trim() || t.dir.trim() || t.command.trim());
    const same = a.was === `${a.layout}/${tabs.length}`;
    return { name: a.name, layout: a.layout, auto: a.auto, tabs, cols: same ? a.cols : null, rows: same ? a.rows : null, schedule: a.at ? { at: a.at, days: a.days } : null };
  });
}

// harnessOptions lists the coding agents for a <select>, marking the ones
// this machine doesn't have (see dropdown for data-sub and data-warn).
function harnessOptions() {
  return (state.settings?.harnesses || [])
    .map((h) => `<option value="${esc(h.id)}" data-sub="${esc(h.installed ? h.bin : "not installed")}"${h.installed ? "" : " data-warn"}>${esc(h.name)}</option>`)
    .join("");
}

// ---------- menus in place of a <select> ----------

// dropdown shows a <select> as the app's own menu, like the side panel's
// agent picker. The select stays, hidden: it holds the value and fires
// change, so the code around it doesn't change. An option's data-sub shows
// on its right (data-warn: in yellow, and on the button too); data-sep
// draws a line after it. Call it again after setting the select's options,
// value or disabled.
function dropdown(sel) {
  if (!sel.dd) {
    const box = document.createElement("div");
    box.className = "dd dd-select";
    box.innerHTML = `<button type="button" class="dd-btn" aria-haspopup="listbox" aria-expanded="false"><span class="dd-label"></span><span class="dd-sub warn"></span><span class="dd-chev">▾</span></button>`;
    sel.after(box);
    sel.classList.add("dd-native");
    sel.dd = box;
    const btn = box.firstElementChild;
    btn.addEventListener("click", () => (ddOpen?.sel === sel ? closeDropdown() : openDropdown(sel)));
    btn.addEventListener("keydown", (e) => {
      if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
      e.preventDefault();
      openDropdown(sel);
    });
  }
  const o = sel.selectedOptions[0], btn = sel.dd.firstElementChild;
  btn.querySelector(".dd-label").textContent = o ? o.textContent : "";
  btn.querySelector(".dd-sub").textContent = o && "warn" in o.dataset ? o.dataset.sub || "" : "";
  btn.disabled = sel.disabled;
  btn.title = sel.title;
}

let ddOpen = null; // { sel, menu, at: the button's top when it opened }

function openDropdown(sel) {
  closeDropdown();
  const btn = sel.dd.firstElementChild;
  const menu = document.createElement("div");
  menu.className = "dd-menu dd-float";
  menu.setAttribute("role", "listbox");
  menu.innerHTML = [...sel.options]
    .map((o, i) => {
      const on = o.selected;
      return `<button type="button" class="dd-opt${on ? " on" : ""}" role="option" aria-selected="${on}" data-i="${i}"${o.disabled ? " disabled" : ""}>
        <span class="dd-check">${on ? "✓" : ""}</span><span class="dd-name">${esc(o.textContent)}</span><span class="dd-sub${"warn" in o.dataset ? " warn" : ""}">${esc(o.dataset.sub || "")}</span></button>${"sep" in o.dataset ? '<div class="dd-sep"></div>' : ""}`;
    })
    .join("");
  document.body.appendChild(menu);
  // under the button (above it when there's more room there), at least as wide
  const r = btn.getBoundingClientRect(), w = Math.max(r.width, 290);
  const below = innerHeight - r.bottom - 12, above = r.top - 12, up = below < Math.min(menu.scrollHeight, 320) && above > below;
  Object.assign(menu.style, {
    width: `${w}px`,
    left: `${Math.max(8, Math.min(r.width >= w ? r.left : r.right - w, innerWidth - w - 8))}px`,
    maxHeight: `${up ? above : below}px`,
    ...(up ? { bottom: `${innerHeight - r.top + 4}px` } : { top: `${r.bottom + 4}px` }),
  });
  ddOpen = { sel, menu, at: r.top };
  sel.dd.classList.add("open");
  btn.setAttribute("aria-expanded", "true");
  (menu.querySelector(".dd-opt.on") || menu.querySelector(".dd-opt:not(:disabled)"))?.focus({ preventScroll: true });
  menu.addEventListener("click", (e) => {
    const b = e.target.closest(".dd-opt");
    if (b) pickDropdown(+b.dataset.i);
  });
  menu.addEventListener("keydown", (e) => {
    const opts = [...menu.querySelectorAll(".dd-opt:not(:disabled)")], i = opts.indexOf(document.activeElement);
    if (e.key === "Escape" || e.key === "Tab") closeDropdown(e.key === "Escape");
    else if ((e.key === "Enter" || e.key === " ") && i >= 0) pickDropdown(+opts[i].dataset.i); // the app's keys would take Enter
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") opts[(i + (e.key === "ArrowDown" ? 1 : -1) + opts.length) % opts.length]?.focus();
    else if (e.key === "Home" || e.key === "End") opts[e.key === "Home" ? 0 : opts.length - 1]?.focus();
    else return;
    e.preventDefault();
    e.stopPropagation(); // Esc closes the menu, not the dialog under it
  });
}

function pickDropdown(i) {
  const { sel } = ddOpen;
  closeDropdown(true);
  if (sel.selectedIndex === i) return;
  sel.selectedIndex = i;
  dropdown(sel);
  sel.dispatchEvent(new Event("change", { bubbles: true }));
}

function closeDropdown(focus) {
  if (!ddOpen) return;
  const { sel, menu } = ddOpen;
  ddOpen = null;
  menu.remove();
  sel.dd.classList.remove("open");
  sel.dd.firstElementChild.setAttribute("aria-expanded", "false");
  if (focus) sel.dd.firstElementChild.focus();
}

// a click elsewhere, a resize, or a scroll that moves the button closes it
// (it's placed for where the button was)
window.addEventListener("mousedown", (e) => ddOpen && !e.target.closest(".dd-float, .dd-select") && closeDropdown(), true);
window.addEventListener("scroll", () => ddOpen && Math.abs(ddOpen.sel.dd.getBoundingClientRect().top - ddOpen.at) > 1 && closeDropdown(), true);
window.addEventListener("resize", () => closeDropdown());

const baseName = (path) => path.replace(/\/+$/, "").split("/").pop() || "";

// ---------- layout + resizing ----------

function applyLayout() {
  const main = $("#main");
  const r = document.documentElement.style;
  r.setProperty("--left", state.left + "px");
  r.setProperty("--right", state.right + "px");
  r.setProperty("--files", state.files + "px");
  // a hidden panel and its handle leave the grid, so their columns go too
  r.setProperty("--cols", [
    state.showLeft && "var(--left) 10px",
    "minmax(0, 1fr)",
    state.showFiles && "10px var(--files)",
    state.showRight && "10px var(--right)",
  ].filter(Boolean).join(" "));
  main.classList.toggle("no-left", !state.showLeft);
  main.classList.toggle("no-right", !state.showRight);
  main.classList.toggle("no-files", !state.showFiles);
  document.body.classList.toggle("focus-mode", !!state.beforeFocus);
  for (const [side, shown, name, key] of [["left", state.showLeft, "projects", "⌘B"], ["right", state.showRight, "side", "⌘I"], ["files", state.showFiles, "files", "⇧⌘E"]]) {
    const b = $(`#btn-${side}`);
    b.classList.toggle("on", shown);
    b.setAttribute("aria-pressed", String(shown));
    b.title = `${shown ? "Hide" : "Show"} the ${name} panel (${key})`;
  }
  fitActive();
  if (state.showFiles) refreshFiles();
}

// Show or hide one side panel (⌘B, ⌘I, ⇧⌘E, or its title bar button).
function togglePanel(side) {
  learned(`toggle-${side}`);
  if (side === "left") state.showLeft = !state.showLeft;
  else if (side === "files") state.showFiles = !state.showFiles;
  else state.showRight = !state.showRight;
  state.beforeFocus = null;
  try {
    localStorage.setItem(PANELS_KEY, JSON.stringify({ left: state.showLeft, right: state.showRight, files: state.showFiles }));
  } catch {}
  applyLayout();
}

// Focus mode: only the terminal, both panels and the hints bar hidden; the
// same key brings back what was showing.
function toggleFocusMode() {
  if (state.beforeFocus) {
    ({ left: state.showLeft, right: state.showRight, files: state.showFiles } = state.beforeFocus);
    state.beforeFocus = null;
  } else {
    state.beforeFocus = { left: state.showLeft, right: state.showRight, files: state.showFiles };
    state.showLeft = state.showRight = state.showFiles = false;
    flash(keyText("focus mode · ⇧⌘↵ to bring the panels back"));
  }
  applyLayout();
}

let fitPending = false;
function fitActive() {
  if (fitPending) return;
  fitPending = true;
  requestAnimationFrame(() => {
    fitPending = false;
    const cur = activeTab();
    if (isSplit(cur) && isTree(cur.group)) placeTree(cur.group);
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
  const limits = { left: [200, 560], right: [260, 600], files: [180, 560] };
  const defaults = { left: DEFAULT_LEFT, right: DEFAULT_RIGHT, files: DEFAULT_FILES };
  const save = (side) => {
    if (side !== "files") return Go().SaveLayout(state.left, state.right);
    try {
      localStorage.setItem(FILES_KEY, String(state.files));
    } catch {}
  };
  for (const side of ["left", "right", "files"]) {
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
        save(side);
      };
      window.addEventListener("mousemove", move);
      window.addEventListener("mouseup", up);
    });
    handle.addEventListener("dblclick", () => {
      state[side] = defaults[side];
      applyLayout();
      save(side);
    });
  }
  new ResizeObserver(fitActive).observe($("#terms"));
}

// ---------- rendering ----------

function render() {
  renderTitle();
  renderHints();
  renderProjects();
  renderTabs();
  if (state.showFiles || document.body.classList.contains("drawer-files")) refreshFiles();
  renderActiveList();
  renderStats(); // automations started or stopped (redrawn only when it changed)
}

// One line under the name on the home page, a different one each time.
const TAGLINES = [
  "From developers, to developers.",
  "There's no place like 127.0.0.1.",
  "Home is where the shell is.",
  "Where your agents clock in.",
  "Works on my machine. Now on yours too.",
  "Tabs or spaces? Tabs. Lots of tabs.",
  "Keep calm and press C.",
  "Pair programming, minus the awkward silence.",
  "Your agents don't sleep. Your Mac doesn't have to either.",
  "Have you tried turning the agent off and on again?",
  "Merge conflicts sold separately.",
  "One window, every agent, zero yak shaving.",
  "Less tab hunting, more shipping.",
  "Ship it. Then ship it again.",
  "Hello, world. Again.",
  "sudo make me a sandwich.",
  "Built at midnight, shipped by morning.",
  "Every project gets its own quarters.",
  "Your rubber duck writes code now.",
  "It's not a bug, it's an agent's feature.",
  "Fewer windows, more done.",
  'git commit -m "it works, don\'t touch it"',
  "The coffee is yours. The typing is theirs.",
  "Talk to your agents. They're great listeners.",
  "Twelve agents walk into a terminal…",
  "It worked yesterday.",
  "There are 10 kinds of developers, and they're both here.",
  "Powered by coffee and stack traces.",
  "99 little bugs in the code. Patch one, 127 to go.",
  "It's always DNS.",
  "Deploying on a Friday? Bold. We respect it.",
  "The agent wrote the tests. They all pass. Suspicious.",
  "TODO: write a funnier tagline.",
  "Localhost, sweet localhost.",
  "We put the fun in function.",
  "// it's fine. everything is fine.",
  "No semicolons were harmed in the making of this app.",
  "Exit code 0. Life is good.",
  "Ctrl+C, Ctrl+V, Ctrl+Ship.",
  "Have you read the error? Neither has the agent.",
  "Your agents work the night shift. You take the credit.",
  "Code by agents, blame by git.",
  "rm -rf distractions/",
  "undefined is not a function. This is.",
  "Now with 100% fewer lost terminals.",
  "Merge first, ask questions later.",
  "Agents in the terminal, you in the hammock.",
  "Off by one? Not today.",
  "Two hard problems: naming, caching, and off-by-one errors.",
  "Stack Overflow, but it types for you.",
];

// ---------- the home page's "today" card ----------

// renderToday draws today's use across every project: the totals count up
// to their new values and the bars move to theirs. grow: the bars rise from
// nothing, one after another (home has just been shown).
let todayFirst = true;
async function renderToday(grow) {
  if ($("#welcome").classList.contains("hidden")) return;
  renderWork();
  let t;
  try {
    t = await Go().Today();
  } catch {
    return;
  }
  const act = t.activity || [];
  const card = $("#home-today");
  const any = t.sessions > 0 || act.some((v) => v > 0);
  // the first figures come from a scan in the background: ask again shortly
  if (!any && todayFirst) setTimeout(() => renderToday(true), 1500);
  todayFirst = false;
  card.classList.toggle("hidden", !any);
  if (!any) return;
  const u = t.usage || {};
  const prompt = (u.Input || 0) + (u.CacheRead || 0) + (u.CacheWrite || 0);
  countUp(card.querySelector('[data-n="sessions"]'), t.sessions, String);
  countUp(card.querySelector('[data-n="output"]'), u.Output || 0, fmtTokens);
  countUp(card.querySelector('[data-n="input"]'), prompt, fmtTokens);
  countUp(card.querySelector('[data-n="hit"]'), prompt ? Math.round((100 * (u.CacheRead || 0)) / prompt) : 0, (n) => `${n}%`);

  const box = $("#ht-bars"), peak = Math.max(1, ...act), now = Date.now();
  if (box.children.length !== act.length) {
    box.innerHTML = act.map((_, i) => `<div class="ht-bar" style="--i:${i}"><i></i></div>`).join("");
    grow = true;
  }
  const at = (i) => new Date(now - (act.length - 1 - i) * 5 * 60000).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  [...box.children].forEach((b, i) => {
    const v = act[i] || 0;
    b.dataset.tip = `${at(i)} · ${v ? `${fmtTokens(v)} tokens` : "quiet"}`;
    b.classList.toggle("zero", !v);
    b.classList.toggle("now", i === act.length - 1);
    b.classList.toggle("peak", v > 0 && v === peak);
    b.style.setProperty("--h", v ? `${Math.max(6, (100 * v) / peak).toFixed(1)}%` : "3px");
  });
  if (grow) {
    box.classList.remove("grow");
    void box.offsetWidth; // start the rise again
    box.classList.add("grow");
  }
}

// renderWork lists today's work on Home: per project, your commits, the
// agent conversations and what isn't committed yet.
let work = null;
async function renderWork() {
  try {
    work = await Go().DailySummary();
  } catch {
    return;
  }
  const list = work.projects || [];
  $("#home-work").classList.toggle("hidden", !list.length);
  const html = list
    .map((d) => {
      const p = project(d.path);
      const commits = d.commits.slice(0, 4).map((c) => `<li><span class="violet">●</span>${esc(c)}</li>`).join("") + (d.commits.length > 4 ? `<li class="muted">+ ${d.commits.length - 4} more commits</li>` : "");
      const chats = d.prompts ? `<li><span class="accent">✻</span>${d.prompts} prompt${d.prompts === 1 ? "" : "s"}${d.chats.length ? `: <span class="muted">${esc(d.chats.slice(0, 3).join(" · "))}</span>` : ""}</li>` : "";
      const wip = d.uncommitted ? `<li><span class="yellow">◐</span>${d.uncommitted} file${d.uncommitted === 1 ? "" : "s"} not committed yet</li>` : "";
      return `<div class="hw-proj"><button type="button" class="hw-name" data-open="${esc(d.path)}">${p ? iconHTML(p) : ""}<b>${esc(d.name)}</b>${d.commits.length ? `<span class="muted">${d.commits.length} commit${d.commits.length === 1 ? "" : "s"}</span>` : ""}</button><ul>${commits}${chats}${wip}</ul></div>`;
    })
    .join("");
  const box = $("#hw-list");
  if (box.dataset.html !== html) box.innerHTML = box.dataset.html = html;
}

// copySummary puts today's work on the clipboard as Markdown.
async function copySummary() {
  try {
    work = await Go().DailySummary();
    await (REMOTE || !rt().ClipboardSetText ? navigator.clipboard.writeText(work.markdown) : rt().ClipboardSetText(work.markdown));
    flash(`copied today's summary: ${work.projects.length} project${work.projects.length === 1 ? "" : "s"}`);
  } catch (e) {
    flash(`couldn't copy: ${String(e).replace(/^Error: /, "")}`);
  }
}

// countUp moves a number to n over a moment, eased.
function countUp(el, n, fmt) {
  const from = +el.dataset.v || 0;
  el.dataset.v = n;
  if (from === n || matchMedia("(prefers-reduced-motion: reduce)").matches) return void (el.textContent = fmt(n));
  const t0 = performance.now(), ms = 900;
  const step = (t) => {
    const k = Math.min(1, (t - t0) / ms), e = 1 - (1 - k) ** 3;
    el.textContent = fmt(Math.round(from + (n - from) * e));
    if (k < 1 && +el.dataset.v === n) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

function newTagline() {
  const el = $("#home-tagline"), now = el.textContent;
  let t = now;
  while (t === now) t = TAGLINES[Math.floor(Math.random() * TAGLINES.length)];
  el.textContent = t;
  el.style.animation = "none"; // play the fade-in again
  void el.offsetWidth;
  el.style.animation = "";
}

// renderActiveList shows, on the welcome screen, the projects with open
// sessions: working ones first, then those waiting for you.
function renderActiveList() {
  const el = $("#active-list");
  const live = activeProjects(orderedProjects()).map((p) => ({ p, st: projectStatus(p.path), w: waitingIn(p.path), n: tabsOf(p.path).filter((t) => !t.exited).length }));
  const html = live.length
    ? `<div class="al-head muted">Active now · ${live.length}</div>` +
      live.map(({ p, st, w, n }) => `<button type="button" class="al-row${w ? " attn" : ""}" data-open="${esc(p.path)}">${iconHTML(p)}<span class="al-name">${esc(p.name)}</span>
        <span class="al-st">${w ? `${STATUS_ICON.waiting} ${waitLabel(w)}` : `${STATUS_ICON[st]} ${st === "busy" ? "working" : "your turn"}`}${n > 1 ? ` · ${n} tabs` : ""}</span></button>`).join("")
    : "";
  if (el.dataset.html === html) return;
  el.dataset.html = html;
  el.innerHTML = html;
  el.classList.toggle("hidden", !live.length);
}

function renderTitle() {
  const p = project(state.current);
  setText($("#tb-name"), p ? p.name : "Home");
  $("#btn-home").classList.toggle("on", !p);
  const ic = p ? iconHTML(p) : "";
  if ($("#tb-icon").dataset.html !== ic) {
    $("#tb-icon").innerHTML = ic;
    $("#tb-icon").dataset.html = ic;
  }
  setText($("#tb-path"), p ? p.shortPath : "");
  // phones: what the tab you're looking at is doing, under the title
  const cur = activeTab();
  const st = cur && tabStatus(cur);
  const what = cur ? cur.fixedTitle || cur.title || (cur.kind === "shell" ? "shell" : shortAgent(cur.harness)) : "";
  const doing = { busy: "working", waiting: cur && (cur.kind === "shell" || cur.autoKey) ? "ready" : "your turn", ended: "ended" }[st] || "";
  setText($("#tb-sub"), cur ? `${what} · ${doing}` : p ? `press C to start ${shortAgent(p.agent)}` : "");
  let busy = 0;
  for (const list of Object.values(state.tabs)) for (const t of list) if (tabStatus(t) === "busy") busy++;
  const waiting = waitingTabs().length;
  const r = $("#tb-running"), w = $("#tb-waiting");
  r.classList.toggle("hidden", !busy);
  w.classList.toggle("hidden", !waiting);
  setText(r.children[1], busy);
  setText(w.children[1], waiting);
  const now = new Date();
  setText($("#tb-clock"), `${now.toLocaleDateString([], { weekday: "short", day: "numeric", month: "short" })} · ${now.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", hour12: true })}`);
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
// quiet: the row is also in ACTIVE above, which shows its status
function projectRow(p, pos, filtering, live, quiet) {
  const sel = p.path === state.selected;
  const st = projectStatus(p.path);
  const n = tabsOf(p.path).length;
  const gitBits = p.git ? `${p.dirty ? ` <span class="yellow gitn" title="${p.dirty} changed file${p.dirty === 1 ? "" : "s"}"><i>●</i>${p.dirty}</span>` : ""}${p.ahead ? ` <span class="green gitn" title="${p.ahead} commit(s) to push"><i>↑</i>${p.ahead}</span>` : ""}${p.behind ? ` <span class="red gitn" title="${p.behind} commit(s) to pull"><i>↓</i>${p.behind}</span>` : ""}` : "";
  const rc = !quiet && tabsOf(p.path).some((t) => rcOn(t));
  const branch = p.missing ? "folder missing" : p.branch ? `⎇ ${esc(p.branch)}${gitBits}` : esc(p.shortPath);
  const wait = live && waitingIn(p.path);
  const attn = tabsOf(p.path).some((t) => t.unseen);
  return `<div class="proj${sel ? " selected" : ""}${attn ? " attn" : ""}${p.path === state.current ? " current" : ""}${p.missing ? " missing" : ""}" data-path="${esc(p.path)}" data-raw="${esc(p.group || "")}"${filtering || live ? "" : ' draggable="true"'} role="button" tabindex="-1" title="${esc(p.path)}">
    <span class="l1"><span class="name"><span class="marker">${sel ? "▸" : ""}</span>${iconHTML(p)}<span class="nm">${pos.length ? highlight(p.name, pos) : esc(p.name)}</span></span>
      <span class="status">${wait ? `<span class="wait-age" title="${esc(shortAgent(wait.harness))} ${asking(wait) ? "is asking you something" : "finished"} ${fmtAgo(wait.doneAt) === "now" ? "just now" : fmtAgo(wait.doneAt) + " ago"}">${asking(wait) ? "?" : ""}${fmtAgo(wait.doneAt)}</span>` : ""}${rc ? `<span class="rc-ic" title="Claude Remote Control: continue in claude.ai/code or the Claude app">${RC_ICON}</span>` : ""}${!quiet && (p.automations || []).some((a) => autoRunning(p.path, a)) ? '<span class="tabs-count green" title="An automation is running">▶</span>' : ""}${!quiet && n > 1 ? `<span class="tabs-count">${n}×</span>` : ""}${quiet || st === "idle" ? "" : STATUS_ICON[st]}</span></span>
    <span class="l2"><span class="branch">${branch}</span><span>${fmtAgo(p.lastActive)}</span></span>
    <span class="actions">
      ${(p.automations || []).length ? `<button class="act act-auto" type="button" data-act="auto" title="Automations">▶</button>` : ""}
      <button class="act" type="button" data-act="claude" title="New ${esc(shortAgent(p.agent))} tab">+</button>
      <button class="act" type="button" data-act="continue" title="Continue last conversation">↻</button>
      <button class="act" type="button" data-act="shell" title="Shell tab">›_</button>
      <button class="act" type="button" data-act="reveal" title="Show in Finder">⌕</button>
      <button class="act" type="button" data-act="edit" title="Edit name, folder or group">✎</button>
    </span>
  </div>`;
}

// Claude Remote Control is on for a session in the project (a broadcast mark).
const RC_ICON = '<svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><circle cx="8" cy="8" r="1.7" fill="currentColor"/><path d="M5.2 10.8a4 4 0 0 1 0-5.6M10.8 5.2a4 4 0 0 1 0 5.6M3.1 12.9a7 7 0 0 1 0-9.8M12.9 3.1a7 7 0 0 1 0 9.8" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>';

// The ACTIVE section's key in the folded-groups list.
const ACTIVE_GROUP = "\u2605active";
const ACTIVE_FROM = 5; // projects before the ACTIVE section shows

function renderProjects() {
  const vis = visibleProjects();
  setText($("#project-count"), state.projects.length);
  let html = "";
  if (!state.projects.length) {
    html = `<div class="empty">No projects yet.<br><br>Press <b>⌘O</b> to add a folder, or <b>⌘,</b> to edit<br><span class="soft">${esc("~/.config/quarters/config.json")}</span></div>`;
  } else if (!vis.length) {
    html = `<div class="empty">No project matches.</div>`;
  }
  let group = null;
  const grouped = state.projects.some((p) => p.group);
  const filtering = !!$("#filter").value.trim();
  // ACTIVE: projects with open sessions, those waiting for you first (they
  // also stay in their group, where their status isn't said twice). Only
  // with 5 projects or more: fewer fit at a glance, and one listed twice
  // looked like two.
  const live = filtering || state.projects.length < ACTIVE_FROM ? [] : activeProjects(state.projects);
  const shownLive = new Set(state.collapsed.has(ACTIVE_GROUP) ? [] : live.map((p) => p.path));
  if (live.length) {
    const folded = state.collapsed.has(ACTIVE_GROUP);
    const gst = live.some((p) => projectStatus(p.path) === "busy") ? STATUS_ICON.busy : STATUS_ICON.waiting;
    html += `<div class="active-sec"><button class="group active-head${folded ? " folded" : ""}" type="button" data-group="${ACTIVE_GROUP}" title="${folded ? "Expand" : "Collapse"}">
      <span class="chev">▾</span>ACTIVE${folded ? ` <span class="gstat">${gst}</span>` : ""}<span class="gcount">${live.length}</span></button>`;
    if (!folded) for (const p of live) html += projectRow(p, [], false, true);
    html += `</div>`;
  }
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
    html += projectRow(p, pos, filtering, false, shownLive.has(p.path));
  }
  if (html !== lastProjectsHTML && !projDrag.path) {
    lastProjectsHTML = html;
    $("#project-list").innerHTML = html;
  }
}

let shownColor = "";
function renderTabs() {
  const pc = projectColor(project(state.current)) || "var(--accent)";
  if (pc !== shownColor) $("#center").style.setProperty("--pc", (shownColor = pc));
  document.body.classList.toggle("one-tab", tabsOf(state.current).length + fileTabsOf(state.current).length <= 1);
  if ($("#tab-list .tab-rename")) return; // don't wipe a rename in progress
  const cur = shownFile() ? null : activeTab();
  const label = (t) => (t.wt ? `⎇ ${t.wt} · ` : "") + (t.customTitle || t.fixedTitle || (t.kind === "shell" ? t.title || "shell" : t.title || (t.kind === "continue" || t.kind === "resume" ? `${t.harness} ↻` : t.harness)));
  const html = entries(state.current)
    .map((e, k) => {
      const on = e.tabs.includes(cur);
      if (e.group) {
        const st = e.tabs.some((t) => tabStatus(t) === "busy") ? "busy" : e.tabs.some((t) => !t.exited) ? "waiting" : "ended";
        return `<div class="tab${on ? " active" : ""}${e.tabs.some((t) => t.unseen) ? " attn" : ""}" data-e="${k}" role="button" title="${esc(e.group.name)}: ${esc(e.tabs.map(label).join(", "))}">
          <span class="muted">${k + 1}</span><span class="green">▦</span><span class="label">${esc(e.group.label || (isTree(e.group) ? label(treePanes(e.group).panes[0]) : e.group.name))}</span><span class="muted">${e.tabs.length}</span>${STATUS_ICON[st]}
          <button class="close" type="button" data-close="${k}" aria-label="Close all panes">×</button></div>`;
      }
      const t = e.tabs[0];
      return `<div class="tab${on ? " active" : ""}${t.unseen ? " attn" : ""}" data-e="${k}" role="button" title="${esc(label(t))}">
        <span class="muted">${k + 1}</span><span class="label">${esc(label(t))}</span>${STATUS_ICON[tabStatus(t)]}
        <button class="close" type="button" data-close="${k}" aria-label="Close tab">×</button></div>`;
    })
    .join("") + fileTabsHTML(entries(state.current).length);
  // Only touch the DOM when something changed, so double-clicks land on the same element.
  if (html !== $("#tab-list").dataset.html) {
    $("#tab-list").innerHTML = html;
    $("#tab-list").dataset.html = html;
  }
  // pane headers of split automations show each shell's status
  for (const t of tabsOf(state.current)) {
    if (!t.head) continue;
    setHTML(t.head.querySelector(".pane-status"), STATUS_ICON[tabStatus(t)]);
    if (isTree(t.group)) setText(t.head.querySelector(".pane-name"), label(t));
  }
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
  setText($("#tab-path"), p ? `\u200E${p.shortPath}\u200E` : "");
  $("#btn-new-tab").classList.toggle("hidden", !state.current && !state.selected);
  $("#btn-new-shell").classList.toggle("hidden", !state.current && !state.selected);
  const ap = project(state.current || state.selected);
  $("#btn-auto").classList.toggle("hidden", !(ap && (ap.automations || []).length));
  $("#btn-auto").classList.toggle("running", !!(ap && (ap.automations || []).some((a) => autoRunning(ap.path, a))));
  $("#btn-prompts").classList.toggle("hidden", !on("prompts") || !cur || cur.kind === "shell");
  $("#btn-history").classList.toggle("hidden", !on("history") || !ap);
  $("#btn-wt").classList.toggle("hidden", !on("worktrees") || !ap || !ap.git);
}

// Claude Code's own Remote Control (claude.ai/code and the Claude app).
// The switch is about the Claude tab in view: on when Claude Code has it
// connected (whoever turned it on); flipping it runs /remote-control in
// that session. Below it, whether new Claude tabs start with it.
let rcIds = new Set();
const rcOn = (t) => !!t && !t.exited && rcIds.has(t.id);

async function pollRemoteControl() {
  try {
    const ids = new Set(await Go().RemoteControlled());
    if ([...ids].join() !== [...rcIds].join()) {
      rcIds = ids;
      renderProjects();
      renderStats();
    }
  } catch {}
}

function renderClaudeRemote() {
  if (!state.settings) return "";
  const tab = activeTab();
  const claude = isClaude(tab) && !tab.exited && tab.path === state.statsPath;
  const on = claude && rcOn(tab);
  const always = !!state.settings.claudeRemote;
  const head = claude
    ? `<label class="cr-row" title="Runs /remote-control in this session">
        <span class="cr-text"><b>Claude Remote Control</b><span class="muted">${on ? "this session is open in claude.ai/code and the Claude app" : "continue this session in the Claude app"}</span></span>
        <input type="checkbox" class="switch" data-rc-session${on ? " checked" : ""}${canType() ? "" : " disabled"}></label>`
    : `<div class="cr-row"><span class="cr-text"><b>Claude Remote Control</b><span class="muted">No Claude session open here</span></span></div>`;
  return `<div class="cr-box">${head}<label class="cr-always"><input type="checkbox" data-claude-remote${always ? " checked" : ""}${canChange() ? "" : " disabled"}> Start new Claude tabs with it</label></div>`;
}

function renderStats(force) {
  const s = state.stats;
  const path = state.statsPath;
  const el = $("#stats");
  if (agentPick.open && !force) return; // the agent menu is open: redrawing would close it
  // Claude Code's plan, Remote Control, session stats, tools and skills
  // belong to Claude projects only: another agent's numbers aren't known,
  // and Claude's would mislead. The tab in view decides, else the project.
  const cur = activeTab();
  const p = path && project(path);
  const other = cur && cur.path === path && !mirrors(cur) ? !isClaude(cur) : !!p && p.agent !== "claude";
  const otherId = other ? (cur && cur.path === path && !mirrors(cur) ? cur.harness : p.agent) : "";
  const otherName = other ? agentName(otherId) : "";
  const plan = other ? sec("plan", renderAgentPlan(otherId)) : sec("plan", renderPlan()) + renderClaudeRemote();
  if (!path || !s) {
    setHTML(el, plan + sec("session", `<div class="sec-row"><span class="sec">SESSION</span></div><div class="muted">Select a project.</div>`));
    renderPanelNav();
    return;
  }
  const live = tabsOf(path).some((t) => !t.exited);
  const state_ = live ? '<span class="green">● live</span>' : s.has ? `<span class="muted">last ${fmtAgo(s.last)}</span>` : '<span class="muted">○ none yet</span>';
  let h = plan + sec("agent", renderPanelAgent(path)) + sec("automations", renderPanelAutomations(path));
  let ss = `<div class="sec-row"><span class="sec">SESSION</span>${state_}</div>`;
  if (cur && cur.path === path) ss += `<button class="end-btn" type="button" data-end-tab title="Ends the session in this tab">${cur.exited ? "Close tab" : cur.kind === "shell" || cur.autoKey ? "End this shell" : `End this ${esc(shortAgent(cur.harness))} session`}</button>`;

  if (other) {
    ss += `<div class="muted">${esc(otherName)} runs as it is here: its model, context and token use show in its own window, not in this panel.</div>`;
  } else if (s.has) {
    if (s.title) ss += `<div class="title-line" title="${esc(s.title)}">${esc(s.title)}</div>`;
    const end = live ? Date.now() : s.last;
    const frac = s.window ? s.context / s.window : 0;
    ss += `<div class="kv">
      <div class="row"><span class="muted">model</span><span>${esc(prettyModel(s.model))}</span></div>
      <div class="row"><span class="muted">${end - s.start > 86400000 ? "started" : "elapsed"}</span><span>${end - s.start > 86400000 ? fmtAgo(s.start) + " ago" : fmtDuration(end - s.start)}</span></div>
      <div class="row"><span class="muted">mode</span><span class="violet">${esc(MODES[s.mode] ?? s.mode)}</span></div>
      <div class="row"><span class="muted">prompts</span><span>${s.turns}</span></div></div>
      <div class="kv" style="gap:6px">
      <div class="row"><span class="muted">context</span><span>${fmtTokens(s.context)} / ${fmtTokens(s.window)}  <span class="accent">${Math.round(frac * 100)}%</span></span></div>
      <div class="meter${frac > 0.8 ? " hot" : ""}"><div style="width:${Math.min(100, frac * 100).toFixed(1)}%"></div></div></div>`;
  } else {
    ss += `<div class="muted">No Claude Code history in this folder yet.</div>`;
  }
  h += sec("session", ss);
  if (cur && cur.path === path) h += sec("queue", renderPanelQueue(cur));

  // tokens
  if (!other) {
    let t = "";
    const u = state.tokenView === "today" ? s.today : s.usage;
    const prompt = (u.Input || 0) + (u.CacheRead || 0) + (u.CacheWrite || 0);
    const hit = prompt ? Math.round((100 * (u.CacheRead || 0)) / prompt) : 0;
    t += `<div class="sec-row"><span class="sec">TOKENS</span>
        <span class="seg"><button type="button" data-view="session" class="${state.tokenView === "session" ? "on" : ""}">session</button><button type="button" data-view="today" class="${state.tokenView === "today" ? "on" : ""}">today</button></span></div>
      <div class="tiles">
        <div class="tile"><div class="t-label">input</div><div class="t-value">${fmtTokens(u.Input)}</div></div>
        <div class="tile"><div class="t-label">output</div><div class="t-value">${fmtTokens(u.Output)}</div></div>
        <div class="tile"><div class="t-label">cache read</div><div class="t-value">${fmtTokens(u.CacheRead)}</div></div>
        <div class="tile"><div class="t-label">cache hit</div><div class="t-value green">${hit}%</div></div>
      </div>`;
    if (state.tokenView === "today") t += `<div class="muted" style="margin-top:-6px;font-size:12px">${s.todaySessions} sessions today, all projects</div>`;

    // activity
    const act = s.activity || [];
    const peak = Math.max(1, ...act);
    t += `<div class="kv" style="gap:6px"><div class="row" style="font-size:12px"><span class="muted">tokens / 5 min</span><span class="muted">last 2h</span></div>
      <div class="chart">${act
        .map((v, i) => {
          const cls = !v ? "zero" : i === act.length - 1 ? "now" : "";
          const hgt = v ? Math.max(4, Math.round((44 * v) / peak)) : 2;
          return `<div class="${cls}" style="height:${hgt}px" title="${fmtTokens(v)} tokens"></div>`;
        })
        .join("")}</div></div>`;
    h += sec("tokens", t);
  }

  h += sec("usage", renderPanelUsage(path));

  // git
  const g = s.git;
  if (g && g.IsRepo) {
    const sync = g.Ahead || g.Behind ? ` <span class="muted">↑${g.Ahead} ↓${g.Behind}</span>` : "";
    let t = `<div class="sec-row"><span class="sec">GIT</span><span><span class="violet">⎇ ${esc(g.Branch)}</span>${sync}</span></div><div class="files">`;
    const files = g.Files || [];
    if (!files.length) t += `<div class="green">✓ working tree clean</div>`;
    for (const f of files.slice(0, 8)) {
      const col = f.Status === "A" || f.Status === "??" ? "green" : f.Status === "D" ? "red" : "yellow";
      const diff = `${f.Added ? `<span class="green">+${f.Added}</span>` : ""}${f.Del ? ` <span class="red">−${f.Del}</span>` : ""}`;
      const st = f.Status === "??" ? "U" : f.Status;
      const cut = f.Path.replace(/\/$/, "").lastIndexOf("/");
      const dir = cut >= 0 ? f.Path.slice(0, cut + 1) : "", base = f.Path.slice(cut + 1);
      t += `<button type="button" class="row file-row" data-diff="${esc(f.Path)}"><span class="path" title="${esc(f.Path)} — show changes"><span class="${col} st">${esc(st)}</span><span class="muted">${esc(dir)}</span>${esc(base)}</span><span>${diff}</span></button>`;
    }
    if (files.length > 8) t += `<div class="muted">+ ${files.length - 8} more</div>`;
    h += sec("git", t + `</div>` + renderGitActions(path, g));
  }

  // files Claude edited this session
  const edited = (s.edited || []).filter((f) => f.startsWith(path + "/")).map((f) => f.slice(path.length + 1));
  if (!other && on("changes") && edited.length) {
    let t = `<div class="sec-row"><span class="sec">CLAUDE CHANGED</span><span class="muted note">${edited.length} file${edited.length === 1 ? "" : "s"} · tap for diff</span></div><div class="files">`;
    for (const f of edited.slice(-12).reverse()) {
      const g = ((s.git && s.git.Files) || []).find((x) => x.Path === f);
      const cut = f.lastIndexOf("/");
      const diff = g ? `${g.Added ? `<span class="green">+${g.Added}</span>` : ""}${g.Del ? ` <span class="red">−${g.Del}</span>` : ""}${!g.Added && !g.Del ? `<span class="${g.Status === "??" ? "green" : "yellow"}">${g.Status === "??" ? "new" : esc(g.Status)}</span>` : ""}` : s.git && s.git.IsRepo ? '<span class="muted">committed</span>' : "";
      t += `<button type="button" class="row file-row" data-diff="${esc(f)}"><span class="path" title="${esc(f)}"><span class="violet st">✎</span><span class="muted">${esc(cut >= 0 ? f.slice(0, cut + 1) : "")}</span>${esc(f.slice(cut + 1))}</span><span>${diff}</span></button>`;
    }
    if (edited.length > 12) t += `<div class="muted">+ ${edited.length - 12} earlier</div>`;
    h += sec("changed", t + `</div>`);
  }

  // local servers the terminals started
  const links = state.links.filter((l) => l.path === path);
  if (on("dev_links") && links.length) {
    let t = `<div class="sec-row"><span class="sec">SERVERS</span><span class="muted note">${REMOTE ? "tap to open on the Mac" : "click to open"}</span></div><div class="kv" style="gap:4px;font-size:12px">`;
    for (const l of links)
      t += `<button type="button" class="row link-row" data-link="${esc(l.url)}" title="${esc(l.url)} · from ${esc(l.title)}"><span>${l.up ? '<span class="green">●</span>' : '<span class="muted">○</span>'} ${esc(l.url.replace(/^https?:\/\//, ""))}</span><span class="muted">${l.up ? esc(l.title) : "not answering"}</span></button>`;
    h += sec("servers", t + `</div>`);
  }

  h += sec("scripts", renderPanelScripts(path));
  h += sec("ports", renderPanelPorts(path));
  if (g && g.IsRepo) h += sec("checkpoints", renderPanelCheckpoints(path));

  // tools
  if (!other && s.has && s.tools && s.tools.length) {
    const top = s.tools[0].Count || 1;
    let t = `<div class="sec-row"><span class="sec">TOOLS</span><span class="muted note">calls this session</span></div><div class="kv">`;
    for (const x of s.tools)
      t += `<div class="tool"><div class="row"><span>${esc(x.Name)}</span><span class="soft">${x.Count}</span></div><div class="tool-bar" style="width:${Math.round((100 * x.Count) / top)}%"></div></div>`;
    h += sec("tools", t + `</div>`);
  }

  h += sec("instructions", renderPanelInstructions(path));

  // skills and MCP servers (with what each is), when switched on
  const tools = on("skills") ? state.tools[path] : null;
  if (other) {
    /* Claude Code's skills and MCP servers */
  } else if (tools) {
    const calls = Object.fromEntries((s.mcp || []).map((m) => [m.name, m.calls]));
    let t = `<div class="sec-row"><span class="sec">MCP SERVERS</span><span class="muted">${tools.mcp.length}</span></div><div class="kv tools-list">`;
    if (!tools.mcp.length) t += `<div class="muted">None configured.</div>`;
    for (const m of tools.mcp)
      t += `<div class="tool-item"><div class="row"><span><b>${esc(m.name)}</b> <span class="badge-s">${esc(m.scope)}</span></span>${calls[m.name] ? `<span class="green">● ${calls[m.name]} calls</span>` : '<span class="muted">○</span>'}</div><div class="muted ti-sub">${esc(m.type)} · ${esc(m.target || "—")}</div></div>`;
    h += sec("mcp", t + `</div>`);
    t = `<div class="sec-row"><span class="sec">SKILLS</span><span class="muted">${tools.skills.length}</span></div><div class="kv tools-list">`;
    if (!tools.skills.length) t += `<div class="muted">No skills.</div>`;
    for (const sk of tools.skills)
      t += `<div class="tool-item" title="${esc(sk.about)}"><div class="row"><span><b>${esc(sk.name)}</b></span><span class="badge-s">${esc(sk.scope)}</span></div>${sk.about ? `<div class="muted ti-sub ti-about">${esc(sk.about)}</div>` : ""}</div>`;
    h += sec("skills", t + `</div>`);
  } else if (s.mcp && s.mcp.length) {
    let t = `<div class="sec-row"><span class="sec">MCP SERVERS</span><span class="muted">${s.mcp.length}</span></div><div class="kv" style="font-size:12px">`;
    for (const m of s.mcp)
      t += `<div class="row"><span>${esc(m.name)}</span>${m.calls ? `<span class="green">● ${m.calls} calls</span>` : '<span class="muted">○ configured</span>'}</div>`;
    h += sec("mcp", t + `</div>`);
  }
  // keep the scroll position and only redraw when something changed
  setHTML(el, h);
  renderPanelNav();
}

function fmtResets(ms) {
  if (!ms) return "";
  const d = Math.max(0, (ms - Date.now()) / 1000);
  if (d < 3600) return `resets in ${Math.ceil(d / 60)}m`;
  if (d < 86400) return `resets in ${Math.floor(d / 3600)}h ${Math.floor((d % 3600) / 60)}m`;
  return "resets " + new Date(ms).toLocaleDateString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
}

// renderPlan draws the Claude account's plan limits (what /usage shows).
function renderPlan(a = state.account, title = "CLAUDE PLAN") {
  if (!a) return "";
  let h = `<div class="sec-row"><span class="panel-title">${title}</span>${a.plan ? `<span class="badge">${esc(a.plan)}</span>` : ""}</div>`;
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

// Another agent's plan and quotas, where Quarters can read them (Antigravity,
// from its IDE while it's open); fetched when shown, at most twice a minute.
const agentAcct = { id: "", at: 0, busy: false, val: null };

function renderAgentPlan(id) {
  if (id !== "antigravity") return "";
  if (!agentAcct.busy && (agentAcct.id !== id || Date.now() - agentAcct.at > 30000)) refreshAgentAccount(id);
  const a = agentAcct.id === id ? agentAcct.val : null;
  return a ? renderPlan(a, `${esc(agentName(id).toUpperCase())} PLAN`) : "";
}

async function refreshAgentAccount(id) {
  agentAcct.busy = true;
  try {
    agentAcct.val = await Go().AgentAccount(id);
    agentAcct.id = id;
  } catch {
    /* an older server: nothing to show */
  }
  agentAcct.at = Date.now();
  agentAcct.busy = false;
  renderStats();
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

// The mascot's SVGs animate with CSS, which the browser redraws about 60
// times a second even when nothing on screen changes: that was most of what
// an idle Quarters window cost. So it plays when something happens (its
// mood changes, you point at it) and for a moment every minute, and
// otherwise stands still; while Quarters isn't in front, it always does.
const MASCOT_PLAY_MS = 20000;
const MASCOT_BURST_MS = 6000;
const MASCOT_BURST_EVERY = 60000;
const mascotSrc = {}; // mood → { play, rest }: object URLs of the SVG, animated and still
let mascotPlayUntil = Date.now() + MASCOT_PLAY_MS, mascotBurst = Date.now() + MASCOT_BURST_EVERY;

async function preloadMascot() {
  const url = (svg) => URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
  await Promise.all(
    MOODS.map(async (m) => {
      try {
        const svg = await (await fetch(`mascot/${m}.svg`)).text();
        mascotSrc[m] = { play: url(svg), rest: url(svg.replace("</style>", "* { animation: none !important; }\n</style>")) };
      } catch {
        /* the plain file, animated */
      }
    }),
  );
  renderMascot();
}

function wakeMascot(ms = MASCOT_PLAY_MS) {
  mascotPlayUntil = Math.max(mascotPlayUntil, Date.now() + ms);
}

function setMascot(img, mood, play) {
  if (!img) return;
  const s = mascotSrc[mood];
  const src = s ? (play ? s.play : s.rest) : `mascot/${mood}.svg`;
  if (img.getAttribute("src") !== src) img.src = src;
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
  const shell = tab.kind === "shell" || !!tab.autoKey;
  if (working) return ["thinking", shell ? "Busy…" : `${shortAgent(tab.harness)} is working…`, name];
  return ["happy", shell ? "Shell ready" : "Your turn", shell ? name : `${shortAgent(tab.harness)} is waiting · ${name}`];
}

function renderMascot() {
  const [mood, title, sub] = mascotState();
  const now = Date.now();
  if (mood !== mascotMood) {
    mascotMood = mood;
    wakeMascot();
  }
  if (now > mascotBurst) {
    mascotBurst = now + MASCOT_BURST_EVERY;
    wakeMascot(MASCOT_BURST_MS);
  }
  // working: it keeps thinking while you watch
  const play = document.visibilityState === "visible" && document.hasFocus() && (mood === "thinking" || now < mascotPlayUntil);
  setMascot($("#mascot-img"), mood, play);
  setMascot($(".welcome-mascot"), "excited", play);
  setText($("#mascot-title"), title);
  setText($("#mascot-sub"), sub);
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

function openPalette(query = "") {
  learned("palette");
  state.palette.open = true;
  state.palette.sel = 0;
  $("#pal-query").value = query;
  $("#palette").classList.remove("hidden");
  renderPalette();
  $("#pal-query").focus();
}

function closePalette() {
  state.palette.open = false;
  $("#palette").classList.add("hidden");
  if (shownFile()) shownFile().view.focus();
  else if (state.focus === "terminal") activeTab()?.term.focus();
}

// Every command, for the palette's ">" mode (⇧⌘P). mac: only in the Mac app.
const COMMANDS = [
  ["home", "Home: every session at a glance", ""],
  ["search", "Search every project", "⇧⌘K"],
  ["send-to-agent", "Send the selection to the agent", "⇧⌘L"],
  ["commit", "Commit the project's changes…", ""],
  ["push", "Push the project's branch", ""],
  ["copy-summary", "Copy today's summary (Markdown)", ""],
  ["new-claude", "New agent tab", "⌘T"],
  ["continue", "Continue the last conversation", "⇧⌘T"],
  ["new-shell", "New shell tab", "⌥⌘T"],
  ["close-tab", "Close the tab (or the pane, in a split)", "⌘W"],
  ["split-right", "Split: a shell beside this one", "⌘D"],
  ["split-down", "Split: a shell below this one", "⇧⌘D"],
  ["next-pane", "Next pane of the split", "⌘]"],
  ["prev-pane", "Previous pane of the split", "⌘["],
  ["next-tab", "Next tab", "⇧⌘]"],
  ["prev-tab", "Previous tab", "⇧⌘["],
  ["next-waiting", "Go to the session waiting for you", "⌘J"],
  ["clear", "Clear the scrollback", "⌘K"],
  ["add-project", "Add a project…", "⌘O"],
  ["history", "Search past conversations…", "⇧⌘F"],
  ["worktrees", "Worktrees…", "⇧⌘G"],
  ["usage", "Usage by project…", "⇧⌘U"],
  ["toggle-left", "Show or hide the projects panel", "⌘B"],
  ["toggle-right", "Show or hide the side panel", "⌘I"],
  ["toggle-files", "Show or hide the files panel", "⇧⌘E"],
  ["save-file", "Save the file", "⌘S"],
  ["focus-mode", "Focus mode: only the terminal", "⇧⌘↵"],
  ["fold-sections", "Fold every side panel section", ""],
  ["unfold-sections", "Unfold every side panel section", ""],
  ["theme-system", "Theme: follow the system", ""],
  ["theme-light", "Theme: light", ""],
  ["theme-dark", "Theme: dark", ""],
  ["font-up", "Bigger text", "⌘="],
  ["font-down", "Smaller text", "⌘-"],
  ["font-reset", "Normal text size", "⌘0"],
  ["settings", "Settings…", "⌘,"],
  ["shortcuts", "Keyboard shortcuts", "⌘/"],
  ["reveal", "Show the project in Finder", "⇧⌘R", "mac"],
  ["edit-config", "Edit config.json…", "⌥⌘,"],
  ["remote", "Remote access…", "", "mac"],
  ["remote-toggle", "Start or stop the remote-access server", "⌥⌘R", "mac"],
].filter(([, , , mac]) => !mac || !REMOTE).map(([action, title, keys]) => ({ action, title, keys }));

function renderPalette() {
  const raw = $("#pal-query").value.trim();
  if (raw.startsWith(">")) return renderCommands(raw.slice(1).trim());
  $("#palette").classList.remove("commands");
  const q = raw;
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

// scriptCommands are the open project's scripts, as commands ("run dev").
function scriptCommands() {
  const path = state.current || state.selected;
  const list = (on("scripts") && dev[path]?.scripts) || [];
  return list.map((s, i) => ({ action: `run-script:${i}`, title: `run ${s.name}`, keys: s.command }));
}

function renderCommands(q) {
  $("#palette").classList.add("commands");
  const matches = [...COMMANDS, ...scriptCommands()].map((c) => ({ c, m: q ? fuzzy(q, c.title) : { score: 0, pos: [] } })).filter((x) => x.m);
  if (q) matches.sort((x, y) => y.m.score - x.m.score);
  state.palette.matches = matches;
  state.palette.sel = Math.min(state.palette.sel, Math.max(0, matches.length - 1));
  $("#pal-count").textContent = `${matches.length} commands`;
  $("#pal-list").innerHTML =
    matches
      .map(({ c, m }, i) => `<button class="pal-item${i === state.palette.sel ? " sel" : ""}" type="button" data-i="${i}">
        <span class="marker">${i === state.palette.sel ? "▸" : ""}</span><span class="grow"><span class="nm">${highlight(c.title, m.pos || [])}</span></span>
        <span class="meta">${esc(keyText(c.keys))}</span></button>`)
      .join("") || `<div class="empty">No matching command.</div>`;
  $("#pal-list .sel")?.scrollIntoView({ block: "nearest" });
}

function pickPalette(i, newTabToo) {
  const m = state.palette.matches[i];
  closePalette();
  if (!m) return;
  if (m.c && m.c.action.startsWith("run-script:")) {
    const path = state.current || state.selected;
    return runScript(path, (dev[path]?.scripts || [])[+m.c.action.slice(11)]);
  }
  if (m.c) return onMenu(m.c.action);
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
    if (e.target.closest?.("input, textarea, .overlay, .cm-editor, #files")) return;
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
  // add, settings and the rest sit in the top bar (and at the foot of the drawer on a phone), with the theme
  document.addEventListener("click", (e) => {
    const act = e.target.closest?.("[data-act]")?.dataset.act;
    if (act === "add") addProject();
    else if (act === "settings") onMenu("settings");
    else if (act === "theme") onMenu(`theme-${THEME_NEXT[theme]}`);
    else if (act === "awake") $("#awake-menu").classList.contains("hidden") ? showAwakeMenu() : hideMenus();
    else if (act === "github") REMOTE ? window.open(REPO, "_blank", "noopener") : openLink(REPO); // a browser's own tab, not one on the Mac
  });
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
    if (e.target.closest(".active-sec")) return void (projDrag.target = null); // ACTIVE fills itself
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
    if (tab && !tab.classList.contains("file") && !e.target.closest("[data-close]")) renameEntry(Number(tab.dataset.e));
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

// ---------- files: the files panel and editor tabs (ui/files.js, ui/fileview.js) ----------
//
// A file opens in an editor tab of its project, after the terminal tabs.
// Editor tabs belong to this window: the files themselves are the shared
// state, and an editor notices when an agent changes the one it shows.

const fileTabsOf = (path) => state.fileTabs[path] || [];
const shownFile = (path = state.current) => fileTabsOf(path).find((f) => f.key === state.fileOn[path]) || null;
const fileView = () => ui("fileview.js");

// filesRoot is the folder the files panel shows: the project in view, or
// the worktree its tab runs in.
function filesRoot() {
  const path = state.current || state.selected;
  const p = path && project(path);
  if (!p || p.missing) return null;
  const t = state.current === path ? activeTab() : null;
  return { path, dir: (t && t.startOpts && t.startOpts.cwd) || path };
}

function refreshFiles(force) {
  ui("files.js").then((m) => m.refreshFiles(force)).catch(() => {});
}

// showFileTab puts a file tab in view (null: back to the terminals).
function showFileTab(f) {
  if (!f) return;
  state.fileOn[f.project] = f.key;
  if (state.current !== f.project) openProject(f.project);
  showActive();
  render();
  fileView().then((m) => m.checkDisk(f));
}

function applyFileView() {
  const f = shownFile();
  $("#file-view").classList.toggle("hidden", !f);
  document.body.classList.toggle("file-open", !!f);
  for (const list of Object.values(state.fileTabs)) for (const x of list) x.el.classList.toggle("hidden", x !== f);
  if (!f) return;
  f.view.requestMeasure();
  if (!isTouch() && !$("#tab-list .tab-rename")) requestAnimationFrame(() => f.view.focus());
}

async function closeFileTab(f) {
  if (!f) return;
  if (f.dirty && !(await Go().Confirm(`Close ${f.name}?`, "Your changes are not saved.", "Close without saving"))) return;
  const list = fileTabsOf(f.project), i = list.indexOf(f);
  if (i < 0) return;
  list.splice(i, 1);
  f.view.destroy();
  f.el.remove();
  if (state.fileOn[f.project] === f.key) state.fileOn[f.project] = (list[i - 1] || list[i] || { key: "" }).key;
  showActive();
  render();
}

// fileTabsHTML is the tab bar's file tabs, numbered after the n terminal ones.
function fileTabsHTML(n) {
  const on = shownFile();
  return fileTabsOf(state.current).map((f, i) => {
    const wt = f.dir !== f.project ? `⎇ ${baseName(f.dir)} · ` : "";
    return `<div class="tab file${f === on ? " active" : ""}" data-e="${n + i}" role="button" title="${esc(wt + f.rel)}">
      <span class="muted">${n + i + 1}</span><span class="file-ico" aria-hidden="true"></span><span class="label">${esc(wt + f.name)}</span>${f.dirty ? '<span class="accent" title="Not saved">●</span>' : ""}
      <button class="close" type="button" data-close="${n + i}" aria-label="Close file">×</button></div>`;
  }).join("");
}

// ---------- phones and tablets ----------

const isTouch = () => matchMedia("(hover: none) and (pointer: coarse)").matches;
const isNarrow = () => matchMedia("(max-width: 760px)").matches;

function openDrawer(side) {
  for (const s of ["left", "right", "files"]) document.body.classList.toggle(`drawer-${s}`, side === s && !document.body.classList.contains(`drawer-${s}`));
  if (side === "right") refreshStats();
  if (side === "files") refreshFiles();
}

function closeDrawers() {
  document.body.classList.remove("drawer-left", "drawer-right", "drawer-files");
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
  // The page is exactly the visible part of the window: iOS shrinks (and
  // may pan) only the visual viewport for the keyboard or a browser
  // toolbar, so a page sized any other way ends up under the keyboard or
  // short of the bottom.
  // An iPhone home-screen app whose page area stops above the home
  // indicator (it is shorter than the screen by the status bar) needs no
  // bottom inset under the message box.
  const iosApp = navigator.standalone === true;
  const probe = document.createElement("div");
  probe.style.cssText = "position:fixed;top:0;left:0;width:0;height:env(safe-area-inset-top);visibility:hidden;pointer-events:none";
  document.body.appendChild(probe);
  let last = "";
  const fit = () => {
    if (iosApp) {
      const screenH = window.innerWidth > window.innerHeight ? screen.width : screen.height;
      document.body.classList.toggle("short-web", probe.offsetHeight > 0 && screenH - window.innerHeight >= probe.offsetHeight - 3);
    }
    const key = `${Math.round(vv.height)}:${Math.round(vv.offsetTop)}`;
    if (key !== last) {
      last = key;
      const root = document.documentElement.style;
      root.setProperty("--vvh", Math.round(vv.height) + "px");
      root.setProperty("--vvtop", Math.round(vv.offsetTop) + "px");
      fitActive();
    }
  };
  vv.addEventListener("resize", fit);
  vv.addEventListener("scroll", fit);
  window.addEventListener("resize", fit);
  document.addEventListener("focusin", () => setTimeout(fit, 50));
  document.addEventListener("focusout", () => setTimeout(fit, 50));
  every(400, fit, { behind: 1 }); // toolbars and the keyboard don't always say when they move
  fit();
}

// ---------- the project's automations in the side panel ----------

// renderPanelAutomations lists the open project's automations with run,
// stop, edit and delete buttons ("" when it has none).
function renderPanelAutomations(path) {
  const p = project(path);
  const autos = (p && p.automations) || [];
  if (!autos.length) return "";
  const rows = autos
    .map((a, i) => {
      const run = autoRunning(path, a);
      const what = a.tabs.map((t) => `${t.name}: ${t.command || "shell"}`).join("\n");
      const when = a.schedule ? ` · ${esc(a.schedule.at)}` : a.auto ? " · on open" : "";
      return `<div class="pa-row${run ? " running" : ""}" data-pa="${i}">
        <button class="pa-run" type="button" data-pa-run title="${run ? "Restart" : "Run"}: ${esc(a.name)}\n${esc(what)}">
          <span class="${run ? "yellow" : "green"}">${run ? "↻" : "▶"}</span>
          <span class="pa-name">${esc(a.name)}</span>
          <span class="muted pa-meta">${a.tabs.length} tab${a.tabs.length === 1 ? "" : "s"}${when}</span>
        </button>
        ${run ? `<button class="pa-btn pa-stop" type="button" data-pa-stop title="Stop: close its tabs">■</button>` : ""}
        <button class="pa-btn" type="button" data-pa-edit title="Edit">✎</button>
        <button class="pa-btn pa-del" type="button" data-pa-del title="Delete">×</button>
      </div>`;
    })
    .join("");
  return `<div class="pa-box"><div class="sec-row"><span class="panel-title">AUTOMATIONS</span><button class="pa-add" type="button" data-pa-add title="New automation">＋ new</button></div>
    <div class="pa-list">${rows}</div></div>`;
}

// The open project's coding agent, picked from a menu and saved with Apply.
const agentPick = { path: "", value: null, open: false };

function renderPanelAgent(path) {
  const p = project(path);
  if (!p || !state.settings) return "";
  if (agentPick.path !== path) Object.assign(agentPick, { path, value: null, open: false });
  const saved = p.harness || "";
  const value = agentPick.value ?? saved;
  const def = agentName(state.settings.harness);
  const list = state.settings.harnesses || [];
  const h = list.find((x) => x.id === value);
  const missing = h && !h.installed;
  const opt = (id, name, sub, warn) => `<button type="button" class="dd-opt${id === value ? " on" : ""}" role="option" aria-selected="${id === value}" data-dd-opt="${esc(id)}">
      <span class="dd-check">${id === value ? "✓" : ""}</span><span class="dd-name">${esc(name)}</span><span class="dd-sub${warn ? " warn" : ""}">${esc(sub)}</span></button>`;
  const menu = agentPick.open
    ? `<div class="dd-menu" role="listbox" aria-label="Coding agent">${opt("", "Default", def)}<div class="dd-sep"></div>${list.map((x) => opt(x.id, x.name, x.installed ? x.bin : "not installed", !x.installed)).join("")}</div>`
    : "";
  const changed = value !== saved;
  return `<div class="pa-agent"><div class="sec-row"><span class="panel-title">AGENT</span>${REMOTE ? '<span class="muted note">change it on the Mac</span>' : ""}</div>
    <div class="dd${agentPick.open ? " open" : ""}">
      <button type="button" class="dd-btn" data-dd-toggle aria-haspopup="listbox" aria-expanded="${agentPick.open}"${REMOTE ? " disabled" : ""}>
        <span class="dd-label">${esc(h ? h.name : `Default · ${def}`)}</span>${missing ? '<span class="dd-sub warn">not installed</span>' : ""}<span class="dd-chev">▾</span>
      </button>${menu}
    </div>
    ${missing ? `<div class="dd-note">${esc(h.bin)} isn't on this Mac · <a href="${esc(h.site)}" target="_blank" rel="noopener">get ${esc(h.name)}</a></div>` : ""}
    ${changed ? `<div class="dd-apply"><span class="muted">New tabs start ${esc(h ? h.name : def)}</span><button class="btn" type="button" data-dd-cancel>Cancel</button><button class="btn primary" type="button" data-dd-apply>Apply</button></div>` : ""}
  </div>`;
}

function setAgentMenu(open) {
  agentPick.open = open;
  renderStats(true);
  if (open) ($("#stats .dd-opt.on") || $("#stats .dd-opt"))?.focus();
  else $("#stats [data-dd-toggle]")?.focus();
}

function wirePanelAgent() {
  const panel = $("#stats");
  panel.addEventListener("click", async (e) => {
    if (e.target.closest("[data-dd-toggle]")) return setAgentMenu(!agentPick.open);
    const o = e.target.closest("[data-dd-opt]");
    if (o) {
      agentPick.value = o.dataset.ddOpt;
      return setAgentMenu(false);
    }
    if (e.target.closest("[data-dd-cancel]")) {
      agentPick.value = null;
      return renderStats(true);
    }
    if (!e.target.closest("[data-dd-apply]")) return;
    const p = project(agentPick.path);
    if (!p) return;
    try {
      await Go().SetProjectHarness(p.path, agentPick.value);
      flash(`${p.name}: new tabs start ${agentName(agentPick.value || state.settings.harness)}`);
      agentPick.value = null;
    } catch (err) {
      flash(String(err).replace(/^Error: /, ""));
    }
    await loadProjects();
    render();
  });
  panel.addEventListener("keydown", (e) => {
    if (!agentPick.open) return;
    const opts = [...panel.querySelectorAll(".dd-opt")];
    const i = opts.indexOf(document.activeElement);
    if (e.key === "Escape") setAgentMenu(false);
    else if ((e.key === "Enter" || e.key === " ") && i >= 0) {
      agentPick.value = opts[i].dataset.ddOpt;
      setAgentMenu(false);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") opts[(i + (e.key === "ArrowDown" ? 1 : -1) + opts.length) % opts.length]?.focus();
    else return;
    e.preventDefault();
    e.stopPropagation();
  });
  window.addEventListener("mousedown", (e) => {
    if (agentPick.open && !e.target.closest("#stats .dd")) setAgentMenu(false);
  }, true);
}

// editAutomation opens the project dialog at automation i (-1: a new one).
function editAutomation(p, i) {
  openProjectDialog(p);
  if (i < 0) $("#pd-auto-add").click();
  const cards = document.querySelectorAll("#pd-autos .auto-card");
  const card = i < 0 ? cards[cards.length - 1] : cards[i];
  if (!card) return;
  card.scrollIntoView({ block: "center" });
  card.querySelector("input")?.focus();
}

async function deleteAutomation(p, i) {
  const a = p.automations[i];
  const running = autoTabs(p.path, a).filter((t) => !t.exited).length;
  const also = running ? ` Its ${running} running tab${running === 1 ? "" : "s"} close too.` : "";
  if (!(await Go().Confirm(`Delete “${a.name}”?`, `This removes the automation from ${p.name}.${also}`, "Delete"))) return;
  const keep = autosForDialog(p);
  keep.splice(i, 1);
  try {
    await Go().SaveProject(p.path, p.name, p.path, p.group, p.icon, autosToSave(keep), p.yolo || "", p.harness || "", p.color || "");
  } catch (err) {
    return flash(String(err).replace(/^Error: /, ""));
  }
  if (running) await stopAutomation(p.path, a);
  await loadProjects();
  render();
  flash(`deleted ${a.name}`);
}

function wirePanelAutomations() {
  $("#stats").addEventListener("click", (e) => {
    const add = e.target.closest("[data-pa-add]"), row = e.target.closest("[data-pa]");
    if (!add && !row) return;
    const p = project(state.statsPath);
    if (!p) return;
    if (!canChange()) return watchOnly();
    if (add) return editAutomation(p, -1);
    const i = Number(row.dataset.pa), a = p.automations[i];
    if (!a) return;
    closeDrawers();
    if (e.target.closest("[data-pa-stop]")) stopAutomation(p.path, a);
    else if (e.target.closest("[data-pa-edit]")) editAutomation(p, i);
    else if (e.target.closest("[data-pa-del]")) deleteAutomation(p, i);
    else if (e.target.closest("[data-pa-run]")) runAutomation(p.path, a);
  });
}

function wireEndTab() {
  $("#stats").addEventListener("click", async (e) => {
    if (!e.target.closest("[data-end-tab]")) return;
    if (!canChange()) return watchOnly();
    closeDrawers();
    await requestCloseTab(activeTab());
  });
}

function wireClaudeRemote() {
  $("#stats").addEventListener("change", async (e) => {
    const sw = e.target.closest("[data-rc-session]");
    if (sw) {
      const tab = activeTab();
      if (!tab || tab.exited) return;
      if (!canType()) return watchOnly();
      claimTab(tab);
      tab.lastInput = Date.now();
      Go().Write(tab.id, "/remote-control\r");
      flash(sw.checked ? "turning on Remote Control for this session…" : "Claude asks to confirm: choose “Disconnect this session”");
      sw.checked = !sw.checked; // shows the real state once Claude has changed it
      setTimeout(pollRemoteControl, 1500);
      return;
    }
    const box = e.target.closest("[data-claude-remote]");
    if (!box) return;
    try {
      await Go().SetClaudeRemote(box.checked);
      state.settings.claudeRemote = box.checked;
      flash(box.checked ? "new Claude tabs start with Remote Control" : "new Claude tabs start without Remote Control (unless Claude Code's own setting turns it on)");
    } catch (err) {
      box.checked = !box.checked;
      flash(String(err));
    }
    renderStats();
  });
}

function wireActiveList() {
  $("#hw-list").addEventListener("click", (e) => {
    const b = e.target.closest("[data-open]");
    if (b) openProject(b.dataset.open);
  });
  $(".home-acts").addEventListener("click", (e) => {
    const b = e.target.closest("[data-home]");
    if (b) onMenu(b.dataset.home);
  });
  $("#active-list").addEventListener("click", (e) => {
    const b = e.target.closest("[data-open]");
    if (b) openProject(b.dataset.open);
  });
}

// On a touch screen you type into a real text field (autocorrect,
// dictation, selection, a backspace that repeats) and send it with ↑ or
// return; the terminal's own hidden input is never used for typing.
// placeTools puts the tab tools (new tab, shell, automations, prompts,
// past conversations, worktrees, ▤/💬) in the message card ("card") or the
// tab bar ("tabs"), as the layout says.
function placeTools(where) {
  const tools = ["#btn-new-tab", "#btn-new-shell", "#btn-auto", "#btn-prompts", "#btn-history", "#btn-wt", "#btn-view"].map((s) => $(s));
  const inCard = where === "card";
  const home = inCard ? $("#compose-tools") : $("#tabs");
  if (tools[0].parentElement === home) return;
  if (inCard) home.append(...tools);
  else $("#tab-path").before(...tools);
}

function wireCompose() {
  const form = $("#compose"), box = $("#compose-text");
  // a phone turned into a wider screen (or back) changes the layout
  matchMedia("(max-width: 760px)").addEventListener("change", applySessionView);
  const grow = () => {
    box.style.height = "auto";
    box.style.height = Math.min(box.scrollHeight, 132) + "px";
  };
  const send = () => {
    const tab = activeTab();
    if (!tab || tab.exited) return;
    if (!canType()) return watchOnly();
    const text = box.value;
    claimTab(tab);
    tab.lastInput = Date.now();
    if (!text && !mirrors(tab) && tabStatus(tab) === "busy") return Go().Write(tab.id, harnessOf(tab)?.interrupt || "\x1b"); // ■: stop the agent
    if (text.trim() && chatOn(tab)) {
      tab.pending = { text: text.trim(), at: Date.now() };
      setTimeout(renderChat, 0);
    }
    if (text.includes("\n")) tab.term.paste(text); // several lines arrive as one paste
    else if (text) Go().Write(tab.id, text);
    setTimeout(() => Go().Write(tab.id, "\r"), text ? 60 : 0);
    box.value = "";
    grow();
    updateSend(tab);
  };
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    send();
  });
  box.addEventListener("input", () => {
    grow();
    updateSend();
  });
  // the key bar's ctrl, then a letter: Ctrl+letter to the agent, not a
  // letter in the card (^R, ^L, …)
  box.addEventListener("beforeinput", (e) => {
    if (!ctrlArmed || !e.data || e.data.length !== 1) return;
    const tab = activeTab();
    e.preventDefault();
    const d = stickyCtrl(e.data);
    if (tab && !tab.exited && canType()) {
      claimTab(tab);
      tab.lastInput = Date.now();
      Go().Write(tab.id, d);
    }
  });
  box.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    } else if (e.key === "Backspace" && box.value === "") {
      // nothing left here: delete in the agent's own input instead
      const tab = activeTab();
      if (tab && !tab.exited && canType()) Go().Write(tab.id, "\x7f");
    }
  });
  // the send button keeps the keyboard up
  $("#compose-send").addEventListener("pointerdown", (e) => e.preventDefault());
  $("#compose-later").addEventListener("pointerdown", (e) => e.preventDefault());
  $("#compose-later").addEventListener("click", async () => {
    const tab = activeTab();
    if (!tab || tab.exited || !box.value.trim()) return;
    if (!canType()) return watchOnly();
    try {
      await Go().QueueAdd(tab.id, box.value);
    } catch (err) {
      return flash(String(err).replace(/^Error: /, ""));
    }
    box.value = "";
    grow();
    updateSend(tab);
    flash(`Queued: it goes in when ${shortAgent(tab.harness)} finishes`);
  });
  // a tap on the terminal opens the text field's keyboard, not the terminal's
  document.addEventListener("focusin", (e) => {
    if (e.target.classList?.contains("xterm-helper-textarea") && getComputedStyle(form).display !== "none") box.focus();
  });
}

// Swiping a terminal scrolls its history, with momentum (the terminal
// itself only scrolls with a mouse wheel).
function wireTouchScroll() {
  const terms = $("#terms");
  let last = 0, acc = 0, v = 0, t0 = 0, coast = 0, moved = false;
  const lineHeight = (tab) => {
    const s = tab.term.element?.querySelector(".xterm-screen");
    return s ? s.getBoundingClientRect().height / tab.term.rows : 20;
  };
  const scrollBy = (tab, px) => {
    acc += px;
    const lh = lineHeight(tab), n = Math.trunc(acc / lh);
    if (!n) return;
    acc -= n * lh;
    // A full-screen program (Claude Code) has no scrollback here: it scrolls
    // its own history on the mouse wheel, so the swipe turns into wheel turns.
    if (tab.term.buffer.active.type === "alternate" && tab.term.modes.mouseTrackingMode !== "none") return wheel(tab, n);
    tab.term.scrollLines(-n);
  };
  const wheel = (tab, n) => {
    const s = tab.term.element?.querySelector(".xterm-screen");
    if (!s) return;
    const r = s.getBoundingClientRect();
    for (let i = 0; i < Math.abs(n); i++) {
      s.dispatchEvent(new WheelEvent("wheel", { deltaY: n > 0 ? -1 : 1, deltaMode: WheelEvent.DOM_DELTA_LINE, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2, bubbles: true, cancelable: true }));
    }
  };
  terms.addEventListener("touchstart", (e) => {
    cancelAnimationFrame(coast);
    if (e.touches.length !== 1 || e.target.closest?.("#chat")) return void (moved = false, last = NaN);
    last = e.touches[0].clientY;
    t0 = performance.now();
    acc = v = 0;
    moved = false;
  }, { passive: true, capture: true });
  terms.addEventListener("touchmove", (e) => {
    const tab = activeTab();
    if (e.touches.length !== 1 || !tab || Number.isNaN(last)) return; // the chat scrolls by itself
    const y = e.touches[0].clientY, dy = y - last, now = performance.now();
    if (!moved && Math.abs(dy) < 6) return;
    moved = true;
    e.preventDefault();
    e.stopPropagation(); // the terminal's own gesture handler would scroll too
    scrollBy(tab, dy);
    v = dy / Math.max(1, now - t0); // px per ms
    last = y;
    t0 = now;
  }, { passive: false, capture: true });
  terms.addEventListener("touchend", () => {
    const tab = activeTab();
    if (!moved || !tab || Math.abs(v) < 0.2) return;
    let prev = performance.now();
    const step = (now) => {
      const dt = now - prev;
      prev = now;
      scrollBy(tab, v * dt);
      v *= Math.pow(0.995, dt); // slows down like a native list
      if (Math.abs(v) > 0.03) coast = requestAnimationFrame(step);
    };
    coast = requestAnimationFrame(step);
  }, { capture: true });
}

// Pinching a terminal with two fingers changes the text size (phones,
// tablets); it is kept per device, like ⌘+ and ⌘- on the Mac.
function wirePinch() {
  const terms = $("#terms");
  let start = 0, base = 0, last = 0;
  const gap = (t) => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
  terms.addEventListener("touchstart", (e) => {
    if (e.touches.length !== 2 || e.target.closest?.("#chat")) return;
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
  $("#btn-files-m").addEventListener("click", () => openDrawer("files"));
  $("#btn-left").addEventListener("click", () => togglePanel("left"));
  $("#btn-home").addEventListener("click", () => onMenu("home"));
  $("#send-pill").addEventListener("mousedown", (e) => e.preventDefault()); // keep the selection
  $("#send-pill").addEventListener("click", sendToAgent);
  $("#hw-copy").addEventListener("click", copySummary);
  $("#btn-right").addEventListener("click", () => togglePanel("right"));
  $("#btn-files").addEventListener("click", () => togglePanel("files"));
  $("#scrim").addEventListener("click", closeDrawers);
  $("#pane-bar").addEventListener("click", (e) => {
    const b = e.target.closest("[data-pane]");
    if (b) selectTab(Number(b.dataset.pane));
  });
  wireKeybar();
  wireViewport();
  wirePinch();
  wireCompose();
  wireTouchScroll();
  wireChat();
  // The key bar shows while a terminal has the keyboard.
  const typing = () => document.body.classList.toggle("typing", !!document.activeElement?.closest?.(".xterm, #compose"));
  document.addEventListener("focusin", typing);
  document.addEventListener("focusout", () => setTimeout(typing, 0));
  matchMedia("(max-width: 760px)").addEventListener("change", () => {
    closeDrawers();
    showActive(); // a split lays out its panes again (phones show one at a time)
  });
}

// ---------- remote access ----------

// In a browser: sign out, and ask to be notified when Claude needs you.
function wireRemoteUI() {
  document.body.classList.add("remote");
  document.addEventListener("click", async (e) => {
    if (!e.target.closest?.('[data-act="signout"]')) return;
    if (await Go().Confirm("Sign out?", "You will need your passkey to sign in again.", "Sign out")) remoteMod.signOut();
  });
  const bells = document.querySelectorAll('[data-act="notify"]');
  const showBell = () => bells.forEach((b) => b.classList.toggle("hidden", !("Notification" in window) || Notification.permission !== "default"));
  document.addEventListener("click", async (e) => {
    if (!e.target.closest?.('[data-act="notify"]')) return;
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

function renderRemotePill(v) {
  const pill = $("#btn-remote");
  const online = (v.devices || []).reduce((n, d) => n + (d.online || 0), 0);
  pill.classList.remove("hidden");
  pill.classList.toggle("bad", v.enabled && !v.running);
  pill.classList.toggle("off", !v.enabled);
  pill.innerHTML = v.running ? `<span class="green live">●</span> remote${online ? ` · ${online}` : ""}` : v.enabled ? '<span class="red">●</span> remote' : "○ remote";
  pill.title = v.running ? `Remote server running (${v.address || v.url})${online ? ` · ${online} connected` : ""} — click to manage` : v.error || "Remote server stopped — click to start it";
}

// The remote pill in the title bar (the Mac app): it opens the dialog.
function wireRemotePill() {
  $("#btn-remote").addEventListener("click", openRemoteDialog);
  const pill = () => Go().RemoteStatus().then(renderRemotePill).catch(() => {});
  pill();
  every(5000, () => !rmd.open && pill());
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

// ---------- menus: saved prompts and worktrees ----------

// placeMenu shows a small menu under (or above) the element that opened it.
function placeMenu(menu, anchor) {
  menu.classList.remove("hidden");
  const r = anchor.getBoundingClientRect();
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.max(8, Math.min(window.innerWidth - w - 8, r.left)) + "px";
  menu.style.top = (r.bottom + h + 8 > window.innerHeight ? Math.max(8, r.top - h - 4) : r.bottom + 4) + "px";
}

// ---------- keep the machine awake (the cup in the top bar) ----------

// A hold keeps it awake whatever it's doing, like Amphetamine; the Keep
// awake setting does it only while agents work or a browser is connected.
const AWAKE_TIMES = [[30, "30m"], [60, "1h"], [120, "2h"], [180, "3h"], [240, "4h"], [360, "6h"], [480, "8h"], [600, "10h"], [720, "12h"], [1440, "24h"]];
let awake = { on: false, until: 0, auto: false, can: true };
let hostName = "the Mac"; // or "the server" (checkUpdate knows)

const fmtLeft = (ms) => {
  const m = Math.max(1, Math.round(ms / 60e3)), h = Math.floor(m / 60);
  return h ? `${h}h ${String(m % 60).padStart(2, "0")}m` : `${m}m`;
};

function awakeText(v) {
  const Host = hostName[0].toUpperCase() + hostName.slice(1);
  if (!v.can) return `${Host} can't be kept awake: it needs systemd.`;
  if (v.until === -1) return `Keeping ${hostName} awake until you stop it.`;
  if (v.until > 0) return `Keeping ${hostName} awake · ${fmtLeft(v.until - Date.now())} left (until ${new Date(v.until).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", hour12: true })}).`;
  if (v.on) return `${Host} is awake while agents work.`;
  return v.auto ? `${Host} may sleep: no agent is working.` : `${Host} sleeps as usual.`;
}

async function refreshAwake() {
  const v = await Promise.resolve(Go().Awake()).catch(() => null);
  if (v) renderAwake(v);
}

function renderAwake(v) {
  awake = v;
  const b = $("#btn-awake");
  b.classList.toggle("on", v.on);
  b.classList.toggle("hold", !!v.until);
  b.title = awakeText(v);
  if (!$("#awake-menu").classList.contains("hidden")) showAwakeMenu();
}

function showAwakeMenu() {
  const menu = $("#awake-menu");
  menu.innerHTML =
    `<div class="am-head">KEEP ${esc(hostName.toUpperCase())} AWAKE</div><div class="aw-status">${esc(awakeText(awake))}</div>` +
    (!awake.can
      ? ""
      : (awake.until ? `<button class="am-item" type="button" data-awake="0"><span class="red">■</span><span class="grow">Let it sleep again</span></button>` : "") +
        `<button class="am-item" type="button" data-awake="-1"><span class="accent">∞</span><span class="grow">Until I stop it</span></button>` +
        `<div class="aw-sub">FOR</div><div class="aw-times">${AWAKE_TIMES.map(([m, l]) => `<button class="chip" type="button" data-awake="${m}">${l}</button>`).join("")}</div>` +
        `<label class="aw-auto"><input type="checkbox" class="switch" id="aw-auto"${awake.auto ? " checked" : ""}><span>While agents work or a browser is connected<br><span class="muted">the Keep awake setting</span></span></label>`);
  placeMenu(menu, $("#btn-awake"));
}

function wireAwake() {
  const menu = $("#awake-menu");
  menu.addEventListener("click", (e) => {
    const b = e.target.closest("[data-awake]");
    if (!b) return;
    if (!canChange()) return watchOnly();
    Promise.resolve(Go().SetAwake(Number(b.dataset.awake))).then((v) => {
      renderAwake(v);
      hideMenus();
      flash(v.until ? awakeText(v).replace(/ \(until .*\)/, "") : `${hostName} can sleep again`);
    }, (err) => flash(String(err)));
  });
  menu.addEventListener("change", (e) => {
    if (e.target.id !== "aw-auto") return;
    if (!canChange()) return watchOnly();
    Promise.resolve(Go().SetFeature("keep_awake", e.target.checked)).then(() => setTimeout(refreshAwake, 300), (err) => flash(String(err)));
  });
  refreshAwake();
  every(15e3, refreshAwake);
}

function hideMenus() {
  $("#prompt-menu").classList.add("hidden");
  $("#wt-menu").classList.add("hidden");
  $("#awake-menu").classList.add("hidden");
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
    if (!e.target.closest("#prompt-menu, #wt-menu, #awake-menu, #btn-prompts, #btn-wt, #btn-awake, [data-key='prompts']")) hideMenus();
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

// mark wraps each case-insensitive hit of q in s.
function mark(s, q) {
  if (!q) return esc(s);
  const lower = s.toLowerCase(), ql = q.toLowerCase();
  let out = "", i = 0;
  for (let j; (j = lower.indexOf(ql, i)) >= 0; i = j + ql.length) out += esc(s.slice(i, j)) + `<span class="hit">${esc(s.slice(j, j + ql.length))}</span>`;
  return out + esc(s.slice(i));
}

// ---------- a file's changes ----------

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
    const b = e.target.closest("[data-dev]");
    if (b) devAction(b);
  });
  // a section's header folds it (its buttons and switches do their own thing)
  el.addEventListener("click", (e) => {
    const ps = e.target.closest(".ps");
    const head = ps && ps.querySelector(".sec-row");
    if (!head || !head.contains(e.target) || e.target.closest("button, input, label, a, select, .seg")) return;
    foldSection(ps.dataset.ps, !ps.classList.contains("folded"));
  });
  $("#ps-nav").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    if (b.dataset.foldAll !== undefined) {
      const keys = [...document.querySelectorAll("#stats .ps")].map((x) => x.dataset.ps);
      const fold = keys.some((k) => !folds[k]);
      for (const k of keys) foldSection(k, fold);
      return;
    }
    foldSection(b.dataset.jump, false);
    const target = $(`#stats .ps[data-ps="${b.dataset.jump}"]`);
    const box = $("#stats");
    if (target) box.scrollTo({ top: target.offsetTop - box.offsetTop - 8, behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  });
}

// ---------- developer tools in the side panel ----------
//
// Scripts to run, the ports the project's tabs listen on, checkpoints taken
// before each prompt, and commit / push / pull request in the GIT section.

const dev = {}; // project path -> { scripts, ports, checkpoints, pr, at: { what: ms } }

// refreshDev fetches a project's scripts (every 30 s), ports (each refresh:
// Go keeps them 2 s), checkpoints (every 8 s) and pull request (Go asks gh
// once a minute). now: checkpoints too, whatever their age.
async function refreshDev(path, now) {
  const d = (dev[path] ||= { scripts: null, ports: [], checkpoints: null, pr: null, at: {} });
  const due = (k, ms) => {
    if (!now && Date.now() - (d.at[k] || 0) < ms) return false;
    d.at[k] = Date.now();
    return true;
  };
  const jobs = [Go().PullRequest(path).then((v) => (d.pr = v))];
  if (on("scripts")) {
    if (due("scripts", 30000)) jobs.push(Go().Scripts(path).then((v) => (d.scripts = v || [])));
    jobs.push(Go().Ports(path).then((v) => (d.ports = v || [])));
  }
  if (on("checkpoints") && due("cp", 8000)) jobs.push(Go().Checkpoints(path).then((v) => (d.checkpoints = v || [])));
  await Promise.allSettled(jobs);
  if ((state.current || state.selected) === path) renderStats();
}

// The commit, push and pull request buttons under the changed files, and
// the branch's pull request with its checks.
function renderGitActions(path, g) {
  const pr = dev[path]?.pr;
  let t = "";
  if (canChange()) {
    const files = (g.Files || []).length;
    const main = /^(main|master|trunk|develop)$/.test(g.Branch);
    const acts = [];
    if (files) acts.push(`<button type="button" class="btn dev-btn primary" data-dev="commit">Commit…</button>`);
    if (g.Ahead) acts.push(`<button type="button" class="btn dev-btn" data-dev="push">Push ↑${g.Ahead}</button>`);
    if (pr && !pr.has && !pr.noGh && !pr.note && !main && g.Branch !== "HEAD") acts.push(`<button type="button" class="btn dev-btn" data-dev="pr">Open a pull request</button>`);
    if (acts.length) t += `<div class="dev-acts">${acts.join("")}</div>`;
  }
  // gh is there but can't say (not signed in, no GitHub remote…)
  if (pr && pr.note) t += `<div class="muted pr-note" title="${esc(pr.note)}">gh: ${esc(pr.note)}</div>`;
  if (pr && pr.has) {
    const failed = pr.failed || [];
    const checks = failed.length
      ? `<span class="red" title="${esc(failed.join(", "))}">✕ ${failed.length} failed: ${esc(failed.slice(0, 2).join(", "))}${failed.length > 2 ? "…" : ""}</span>`
      : pr.pending
        ? `<span class="yellow">● ${pr.pending} running</span>${pr.passed ? ` <span class="muted">· ${pr.passed} passed</span>` : ""}`
        : pr.passed
          ? `<span class="green">✓ ${pr.passed} passed</span>`
          : '<span class="muted">no checks</span>';
    const state_ = pr.state === "MERGED" ? '<span class="violet">merged</span>' : pr.state === "CLOSED" ? '<span class="muted">closed</span>' : pr.draft ? '<span class="muted">draft</span>' : "";
    t += `<button type="button" class="row link-row pr-row" data-link="${esc(pr.url)}" title="${esc(pr.title)} — open on GitHub"><span class="pr-title"><span class="violet">#${pr.number}</span> ${esc(pr.title)}</span><span class="pr-checks">${state_ ? state_ + " " : ""}${checks}</span></button>`;
  }
  return t;
}

const scriptKey = (path, s) => `${path}::script:${s.command}`;
const scriptTab = (path, s) => tabsOf(path).find((t) => t.autoKey === scriptKey(path, s) && !t.exited);

function renderPanelScripts(path) {
  const list = on("scripts") && dev[path]?.scripts;
  if (!list || !list.length) return "";
  let t = `<div class="sec-row"><span class="sec">SCRIPTS</span><span class="muted note">click to run in a tab</span></div>`;
  let from = "";
  list.forEach((s, i) => {
    if (s.from !== from) {
      t += `${from ? "</div>" : ""}<div class="sc-from muted">${esc(s.from)}</div><div class="scripts">`;
      from = s.from;
    }
    const run = scriptTab(path, s);
    t += `<button type="button" class="script${run ? " on" : ""}" data-dev="script" data-i="${i}" title="${esc(s.command)}${run ? " · running: go to its tab" : ""}">${run ? '<span class="green">●</span>' : '<span class="accent">▶</span>'}${esc(s.name)}</button>`;
  });
  return t + "</div>";
}

function renderPanelPorts(path) {
  const list = on("scripts") ? dev[path]?.ports || [] : [];
  if (!list.length) return "";
  let t = `<div class="sec-row"><span class="sec">PORTS</span><span class="muted note">listening now</span></div><div class="kv ports">`;
  for (const p of list)
    t += `<div class="port-row"><span class="port">:${p.port}</span><span class="grow" title="pid ${p.pid}"><b>${esc(p.process)}</b> <span class="muted">${p.tab ? `· ${esc(p.title || "a tab")}` : "· not from Quarters"}</span></span>
      <button type="button" class="pt-btn" data-dev="port-open" data-port="${p.port}" title="Open http://localhost:${p.port}">open</button>
      ${p.tab ? `<button type="button" class="pt-btn" data-dev="port-tab" data-tab="${p.tab}" title="Go to the tab it runs in">tab</button>` : ""}
      ${canChange() ? `<button type="button" class="pt-btn stop" data-dev="port-stop" data-pid="${p.pid}" data-port="${p.port}" data-mine="${p.tab ? 1 : 0}" title="End the process (SIGTERM, then SIGKILL)">stop</button>` : ""}</div>`;
  return t + "</div>";
}

function renderPanelCheckpoints(path) {
  if (!on("checkpoints")) return "";
  const list = dev[path]?.checkpoints;
  if (!list) return "";
  let t = `<div class="sec-row"><span class="sec">CHECKPOINTS</span><span class="muted note">${list.length ? `${list.length} · ` : ""}before each prompt</span></div>`;
  if (!list.length) return t + `<div class="muted">None yet: one is taken each time you send an agent a prompt, so a turn that goes wrong can be undone.</div>`;
  t += `<div class="kv cps">`;
  for (const c of list.slice(0, 8)) {
    const when = new Date(c.at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    const what = c.files ? `<span class="green">+${c.added}</span> <span class="red">−${c.del}</span> <span class="muted">· ${c.files} file${c.files === 1 ? "" : "s"}</span>` : '<span class="muted">no changes after</span>';
    t += `<div class="cp-row"><button type="button" class="cp-main" data-dev="cp-open" data-id="${c.id}" title="${esc(c.label)} — see what changed since"><span class="cp-when">${when}</span><span class="cp-label muted">${esc(c.label.replace(/^Before a prompt in /, "→ "))}</span><span class="cp-what">${what}</span></button>
      ${canChange() ? `<button type="button" class="pt-btn" data-dev="cp-restore" data-id="${c.id}" title="Put the files back as they were then (how they are now is saved first)">restore</button>` : ""}</div>`;
  }
  if (list.length > 8) t += `<div class="muted">+ ${list.length - 8} older</div>`;
  return t + "</div>";
}

// armed: a button that does something hard to take back asks for a second
// click within 4 s.
function armed(b, label) {
  if (b.dataset.armed) return true;
  b.dataset.armed = "1";
  const was = b.textContent;
  b.textContent = label;
  b.classList.add("armed");
  setTimeout(() => {
    delete b.dataset.armed;
    b.textContent = was;
    b.classList.remove("armed");
  }, 4000);
  return false;
}

async function devAction(b) {
  const path = state.statsPath;
  const d = dev[path] || {};
  const err = (e) => flash(String(e).replace(/^Error: /, ""));
  switch (b.dataset.dev) {
    case "commit":
      return ui("commit.js").then((m) => m.openCommit(path));
    case "push":
      b.disabled = true;
      b.textContent = "pushing…";
      return Go().GitPush(path).then(flash, err).finally(() => refreshStats());
    case "pr":
      b.disabled = true;
      b.textContent = "opening…";
      return Go().CreatePullRequest(path).then((url) => {
        flash("pull request opened");
        openLink(url);
      }, err).finally(() => refreshDev(path, true));
    case "script":
      return runScript(path, (d.scripts || [])[+b.dataset.i]);
    case "port-open":
      return openLink(`http://localhost:${b.dataset.port}`);
    case "port-tab": {
      const t = tabsOf(path).find((x) => x.id === +b.dataset.tab);
      return t && goToTab(t);
    }
    case "port-stop":
      if (b.dataset.mine !== "1" && !armed(b, "sure?")) return;
      b.disabled = true;
      return Go().StopPort(+b.dataset.pid, +b.dataset.port).then(() => flash(`stopped :${b.dataset.port}`), err).finally(() => refreshDev(path, true));
    case "cp-open": {
      const c = (d.checkpoints || []).find((x) => x.id === b.dataset.id);
      return c && ui("checkpoint.js").then((m) => m.openCheckpoint(path, c));
    }
    case "cp-restore":
      if (!armed(b, "sure?")) return;
      b.disabled = true;
      return Go().RestoreCheckpoint(path, b.dataset.id).then(() => flash("restored · how things were is a checkpoint too, so this can be undone"), err).finally(() => {
        refreshDev(path, true);
        refreshStats();
      });
  }
}

// runScript runs a project's script in a tab of its own, or shows the tab
// it already runs in.
function runScript(path, s) {
  if (!s) return;
  const run = scriptTab(path, s);
  if (run) return goToTab(run);
  if (!canChange()) return watchOnly();
  newTab(path, "shell", { title: s.name, dir: path, command: s.command, autoKey: scriptKey(path, s) });
}

// ---------- side panel sections: each folds to its header ----------

// Which sections are folded is remembered on each device (a phone's
// drawer may want less than the Mac).
const FOLDS_KEY = "quarters.folded";
const FOLD_DEFAULTS = { skills: true };
const SECTION_NAMES = {
  plan: "Plan", agent: "Agent", automations: "Automations", session: "Session", queue: "Queue", tokens: "Tokens", usage: "Usage",
  git: "Git", changed: "Changed", servers: "Servers", scripts: "Scripts", ports: "Ports", checkpoints: "Checkpoints", tools: "Tools", instructions: "Instructions", mcp: "MCP", skills: "Skills",
};
const folds = (() => {
  try {
    return { ...FOLD_DEFAULTS, ...JSON.parse(localStorage.getItem(FOLDS_KEY) || "{}") };
  } catch {
    return { ...FOLD_DEFAULTS };
  }
})();

// sec wraps a section's HTML (its header row first); dividers between
// sections come from the stylesheet.
function sec(key, html) {
  if (!html) return "";
  return `<section class="ps${folds[key] ? " folded" : ""}" data-ps="${key}">${html.replaceAll('<div class="divider"></div>', "")}</section>`;
}

function foldSection(key, fold) {
  folds[key] = fold;
  try {
    localStorage.setItem(FOLDS_KEY, JSON.stringify(folds));
  } catch {}
  $(`#stats .ps[data-ps="${key}"]`)?.classList.toggle("folded", fold);
  $("#stats").dataset.html = ""; // the next redraw writes the new state
  renderPanelNav();
}

// The bar above the panel jumps to a section (unfolding it), or folds them all.
function renderPanelNav() {
  const keys = [...document.querySelectorAll("#stats .ps")].map((x) => x.dataset.ps).filter((k) => SECTION_NAMES[k]);
  const open = keys.some((k) => !folds[k]);
  const html = keys.length < 4 ? "" :
    `<div class="ps-chips">${keys.map((k) => `<button type="button" class="${folds[k] ? "folded" : ""}" data-jump="${k}" title="${folds[k] ? "Unfold and show" : "Show"} ${SECTION_NAMES[k]}">${SECTION_NAMES[k]}</button>`).join("")}</div>` +
    `<button type="button" class="ps-all" data-fold-all title="${open ? "Fold every section to its header" : "Unfold every section"}">${open ? "fold all" : "open all"}</button>`;
  setHTML($("#ps-nav"), html);
  $("#ps-nav").classList.toggle("hidden", !html);
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

// ---------- the version (bottom right) ----------

const REPO = "https://github.com/amithgc/quarters";
const ABOUT_LINKS = {
  website: "https://quarters.amithgc.com",
  github: REPO,
  issues: `${REPO}/issues/new/choose`,
  developer: "https://github.com/amithgc",
  license: `${REPO}/blob/main/LICENSE`,
};

// Which Quarters this is; a click shows the about popup above it.
async function showVersion() {
  const v = await Promise.resolve(Go().Version()).catch(() => "");
  const el = $("#version");
  if (!v) return;
  el.textContent = v === "dev" ? "dev build" : `v${v}`;
  el.title = "About Quarters";
  el.hidden = false;
  el.onclick = () => ($("#about-pop").classList.contains("hidden") ? showAbout(v) : hideAbout());
  checkUpdate();
  every(30 * 60e3, checkUpdate);
}

// checkUpdate: a pill beside the version, red when a newer release is out,
// grey when this is the newest; a click says how to update (Quarters asks
// GitHub at most every six hours). No answer from GitHub yet: no pill.
async function checkUpdate() {
  const u = await Promise.resolve(Go().CheckUpdate()).catch(() => null);
  if (u) hostName = u.server ? "the server" : "the Mac";
  const pill = $("#update-pill");
  pill.hidden = !u?.latest;
  if (!u?.latest) return;
  pill.classList.toggle("current", !u.version);
  pill.textContent = u.version ? "update available" : "up to date";
  pill.title = u.version ? `Quarters ${u.version} is out · how to update` : `${u.latest} is the newest · how to update`;
  pill.onclick = () => openUpdate(u);
}

// showAbout: the version, who makes Quarters and where to find it.
function showAbout(v) {
  const pop = $("#about-pop");
  const dev = v === "dev";
  const link = (key, text) => `<button type="button" class="ab-link" data-url="${key}">${text}</button>`;
  pop.innerHTML = `
    <div class="ab-head">
      <img src="mascot/happy.svg" alt="" draggable="false">
      <div class="ab-name"><b>Quarters</b><span class="muted">A home for terminal coding agents</span></div>
    </div>
    <dl class="ab-rows">
      <dt>Version</dt><dd>${dev ? "dev build, from source" : `${esc(v)} · ${link("release", "what's new")}`}</dd>
      <dt>Running</dt><dd>${REMOTE ? "in this browser, from the Mac" : "Mac app"}</dd>
      <dt>Developer</dt><dd>Amith · ${link("developer", "@amithgc")}</dd>
      <dt>Licence</dt><dd>MIT, open source · ${link("license", "read")}</dd>
    </dl>
    <div class="ab-actions">
      <button type="button" class="btn" data-url="website">◆ Website</button>
      <button type="button" class="btn" data-url="github">GitHub</button>
      <button type="button" class="btn" data-url="issues">Report an issue</button>
    </div>
    <div class="ab-foot muted">© 2026 Amith · quarters.amithgc.com</div>`;
  pop.classList.remove("hidden");
  const r = $("#version").getBoundingClientRect();
  pop.style.left = Math.max(8, r.right - pop.offsetWidth) + "px";
  pop.style.top = Math.max(8, r.top - pop.offsetHeight - 8) + "px";
}

function hideAbout() {
  $("#about-pop").classList.add("hidden");
}

function wireAbout() {
  const pop = $("#about-pop");
  pop.addEventListener("click", (e) => {
    const b = e.target.closest("[data-url]");
    if (!b) return;
    const v = $("#version").textContent.replace(/^v/, "");
    rt().BrowserOpenURL(b.dataset.url === "release" ? `${REPO}/releases/tag/v${v}` : ABOUT_LINKS[b.dataset.url]);
    hideAbout();
  });
  window.addEventListener("mousedown", (e) => {
    if (!e.target.closest("#about-pop, #version")) hideAbout();
  }, true);
  window.addEventListener("keydown", (e) => e.key === "Escape" && hideAbout(), true);
}

// ---------- timers ----------

// Everything that runs on a timer goes through every(). Nothing runs while
// the window is hidden (minimized, on another Space, a phone's browser in
// the background) unless it has to, and most things run less often while
// Quarters isn't the app in front. Coming back runs whatever is due at once.
const timers = [];

function every(ms, fn, { hidden = false, behind = 3 } = {}) {
  timers.push({ ms, fn, hidden, behind, last: Date.now() });
}

function runTimers() {
  const now = Date.now();
  const away = document.visibilityState !== "visible";
  const front = document.hasFocus();
  for (const t of timers) {
    if (away && !t.hidden) continue;
    if (now - t.last < (front || away ? t.ms : t.ms * t.behind)) continue;
    t.last = now;
    try {
      t.fn();
    } catch (e) {
      console.error(e);
    }
  }
}

function wireTimers() {
  setInterval(runTimers, 250);
  document.addEventListener("visibilitychange", runTimers);
  window.addEventListener("focus", runTimers);
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
    refreshUsage();
    refreshInstructions(path);
    refreshDev(path);
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
  try {
    const shown = JSON.parse(localStorage.getItem(PANELS_KEY) || "{}");
    state.showLeft = shown.left !== false;
    state.showRight = shown.right !== false;
    state.showFiles = shown.files !== false;
  } catch {}
  document.documentElement.style.setProperty("--ed-fs", state.fontSize + "px");
  applyLayout();

  wireEvents();
  wireResize();
  wireActiveList();
  wireShortcuts();
  wireClaudeRemote();
  wireEndTab();
  wirePanelAutomations();
  wirePanelAgent();
  wirePanelExtras();
  every(3000, pollRemoteControl);
  pollRemoteControl();
  wireProjects();
  wireTabs();
  wirePalette();
  wireDoubleShift();
  wireStart();
  wireAutomations();
  wireFileDrop();
  wireProjectDrag();
  wireMobile();
  wireMenus();
  wireStatsExtras();
  wireAwake();
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
  if (REMOTE) {
    wireRemoteUI();
    wireBrowserKeys();
  }
  else wireRemotePill();
  window.addEventListener("focus", claimActive);
  preloadMascot();
  $("#mascot").addEventListener("click", () => {
    cheerUntil = Date.now() + 2500;
    renderMascot();
  });
  for (const el of [$("#mascot"), $(".welcome-mascot")])
    el?.addEventListener("pointerenter", () => {
      wakeMascot();
      renderMascot();
    });
  $("#home-tagline").textContent = "";
  newTagline(); // any of them, the first time
  setFocus("projects");

  state.me = REMOTE ? remoteMod.remote.client : await Go().ClientID();
  showVersion();
  wireAbout();
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
  every(500, renderMascot);
  if (tipsDue()) setTimeout(() => startTips().catch(() => {}), 3000);
  // Mac notifications and the prompt queue matter most when you're elsewhere
  every(500, checkAttention, { hidden: true, behind: 1 });
  every(1000, runQueues, { hidden: true, behind: 1 });
  every(1000, () => {
    renderTitle();
    renderProjects();
    if (Object.keys(state.tabs).length) renderTabs();
    renderActiveList();
  });
  every(2000, refreshStats);
  every(4000, refreshLinks);
  every(10000, reportViewing, { hidden: true, behind: 1 });
  refreshAccount();
  every(30000, refreshAccount, { behind: 2 });
  // Go says when the list changes ("projects"); this only catches a missed event
  every(30000, loadProjects, { behind: 1 });
  every(15000, () => renderToday());
  renderToday(true);
  wireTimers();
}

main();

// ---------- phones: an agent's conversation as a chat ----------
//
// Where Quarters can read an agent's conversation (Claude Code's log
// today; the agent's "reader"), its tab on a phone can read like a chat
// app: the conversation comes from the log (Chat), the agent's menus and
// questions are read from the terminal, which keeps running underneath,
// and become buttons. ▤ switches to the terminal itself. The message card
// below is the same either way, and the same for every agent.

const chatOn = (tab) => !!tab && currentLayout(tab).transcript === "chat";

async function pollChat() {
  const tab = activeTab();
  if (!chatOn(tab) || document.hidden || tab.chatBusy) return;
  tab.chatBusy = true;
  try {
    const v = await Go().Chat(tab.id, tab.chatVersion || "");
    if (v && !v.same) {
      tab.chat = v.items || [];
      tab.chatVersion = v.version;
    }
  } catch {
    /* not a Claude tab, or offline: the terminal still works */
  } finally {
    tab.chatBusy = false;
  }
  renderChat();
}

// md turns Claude's Markdown into HTML (escaped first; links only http(s)).
function md(src) {
  const inline = (s) =>
    esc(s)
      .replace(/`([^`]+)`/g, "<code>$1</code>")
      .replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>")
      .replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, "$1<i>$2</i>")
      .replace(/(^|[\s(])_([^_\s][^_]*)_(?=[\s.,;:!?)]|$)/g, "$1<i>$2</i>")
      .replace(/\[([^\]]+)\]\((https?:[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  const out = [];
  const lines = src.replace(/\r/g, "").split("\n");
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    if (/^```/.test(l)) {
      const code = [];
      while (++i < lines.length && !/^```/.test(lines[i])) code.push(lines[i]);
      out.push(`<pre><code>${esc(code.join("\n"))}</code></pre>`);
    } else if (/^\s*\|/.test(l)) {
      const rows = [l];
      while (i + 1 < lines.length && /^\s*\|/.test(lines[i + 1])) rows.push(lines[++i]);
      out.push(`<pre class="tbl">${esc(rows.join("\n"))}</pre>`);
    } else if (/^#{1,6}\s/.test(l)) {
      out.push(`<h4>${inline(l.replace(/^#+\s*/, ""))}</h4>`);
    } else if (/^\s*([-*•]|\d+\.)\s+/.test(l)) {
      const ordered = /^\s*\d+\./.test(l), items = [];
      i--;
      while (i + 1 < lines.length && /^\s*([-*•]|\d+\.)\s+/.test(lines[i + 1])) items.push(`<li>${inline(lines[++i].replace(/^\s*([-*•]|\d+\.)\s+/, ""))}</li>`);
      out.push(ordered ? `<ol>${items.join("")}</ol>` : `<ul>${items.join("")}</ul>`);
    } else if (l.trim() === "") {
      out.push("");
    } else {
      const para = [inline(l)];
      while (i + 1 < lines.length && lines[i + 1].trim() !== "" && !/^(```|#{1,6}\s|\s*([-*•|]|\d+\.)\s)/.test(lines[i + 1])) para.push(inline(lines[++i]));
      out.push(`<p>${para.join("<br>")}</p>`);
    }
  }
  return out.join("");
}

// toolsSummary is "Ran 2 commands, edited 1 file" for a run of tool calls.
function toolsSummary(tools) {
  const n = {};
  for (const t of tools) {
    const k = t.name === "Bash" ? "ran" : /^(Edit|Write|MultiEdit|NotebookEdit)$/.test(t.name) ? "edited" : /^(Read)$/.test(t.name) ? "read" : /^(Grep|Glob|WebSearch)$/.test(t.name) ? "searched" : "used";
    n[k] = (n[k] || 0) + 1;
  }
  const s = (k, one, many) => (n[k] ? `${n[k] === 1 ? one : `${n[k]} ${many}`}` : "");
  const parts = [s("ran", "ran a command", "commands"), s("edited", "edited a file", "files edited"), s("read", "read a file", "files read"), s("searched", "searched", "searches"), s("used", "used a tool", "tools used")].filter(Boolean);
  const text = parts.join(", ").replace(/^(\d+) commands/, "ran $1 commands");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function toolRow(t) {
  let body = "";
  if (t.old || t.new) {
    const lines = (s, sign, cls) => (s ? s.split("\n").map((l) => `<div class="${cls}">${sign} ${esc(l)}</div>`).join("") : "");
    body += `<div class="ct-diff">${lines(t.old, "-", "del")}${lines(t.new, "+", "add")}</div>`;
  }
  if (t.result && !(t.name === "Edit" || t.name === "Write") ) body += `<pre class="ct-out${t.error ? " err" : ""}">${esc(t.result)}</pre>`;
  return `<div class="ct"><div class="ct-head"><span class="${t.error ? "red" : "green"}">●</span> <b>${esc(t.name)}</b> <span class="ct-detail">${esc(t.detail || "")}</span></div>${body}</div>`;
}

// chatMenu is Claude's menu on screen (a ❯ option outside its input box):
// the question above it and each option's distance from the chosen one.
function chatMenu(term) {
  if (claudeBoxTop(term) >= 0) return null; // Claude shows its input box or a menu, never both
  const buf = term.buffer.active, top = buf.baseY;
  const text = (y) => buf.getLine(top + y)?.translateToString(true) ?? "";
  const rule = (y) => /^\s*[─━]{3,}/.test(text(y));
  let sel = -1, col = 0;
  for (let y = 0; y < term.rows; y++) {
    const m = /^(\s*)❯\s+(?=\S)/.exec(text(y));
    if (m && !rule(y - 1) && !rule(y + 1)) {
      sel = y;
      col = m[0].length;
    }
  }
  const isOpt = (y) => text(y).search(/\S/) === col || y === sel;
  let first = sel, last = sel;
  for (let y = sel - 1; y >= 0 && (isOpt(y) || text(y).trim() === "") && sel - y < 12; y--) if (isOpt(y)) first = y;
  for (let y = sel + 1; y < term.rows && (isOpt(y) || text(y).trim() === "") && y - sel < 12; y++) if (isOpt(y)) last = y;
  if (lastLine(term) - last > 4) return null; // a menu is the last thing on screen (a hint line may follow)
  const options = [];
  let idx = 0, selIdx = 0;
  for (let y = first; y <= last; y++) {
    if (!isOpt(y)) continue;
    if (y === sel) selIdx = idx;
    options.push({ label: text(y).replace(/^\s*❯?\s*/, "").trim(), idx: idx++ });
  }
  const q = [];
  for (let y = first - 1; y >= 0 && q.length < 6 && !rule(y); y--) {
    const t = text(y).trim();
    if (/[▐▛█▜▌▝▘▀▄]/.test(t)) break; // Claude's logo: not part of the question
    if (t) q.unshift(t);
    else if (q.length) break;
  }
  return { question: q.join(" "), options: options.map((o) => ({ ...o, delta: o.idx - selIdx })) };
}

function renderChat() {
  const tab = activeTab(), el = $("#chat"), on = chatOn(tab);
  document.body.classList.toggle("chat-on", on);
  el.classList.toggle("hidden", !on);
  $("#btn-view").textContent = on ? "▤" : "💬";
  $("#btn-view").title = on ? "Show the terminal" : "Show the chat";
  if (!on) return;
  const busy = tabStatus(tab) === "busy";
  const items = tab.chat || [];
  if (tab.pending && items.some((it) => it.kind === "user" && it.time >= tab.pending.at - 5000 && it.text.startsWith(tab.pending.text.slice(0, 30)))) tab.pending = null;
  let h = "";
  if (!items.length && !tab.pending) h += `<div class="chat-empty">${tab.chat ? `Say hello to ${esc(shortAgent(tab.harness))} below.` : "Loading the conversation…"}</div>`;
  items.forEach((it, i) => {
    if (it.kind === "user") h += `<div class="cm-user">${esc(it.text).replace(/\n/g, "<br>")}</div>`;
    else if (it.kind === "text") h += `<div class="cm-text">${md(it.text)}${chatImagesHTML(it.text)}</div>`;
    else h += `<details class="cm-tools" data-k="${i}-${it.tools.length}"><summary><span class="ct-ic">⋮</span>${esc(toolsSummary(it.tools))}<span class="ct-chev">›</span></summary>${it.tools.map(toolRow).join("")}</details>`;
  });
  if (tab.pending) h += `<div class="cm-user pending">${esc(tab.pending.text).replace(/\n/g, "<br>")}</div>`;
  const menu = !busy && !tab.exited ? chatMenu(tab.term) : null;
  if (menu && menu.options.length > 1) {
    h += `<div class="cm-ask">${menu.question ? `<div class="cm-q">${esc(menu.question)}</div>` : ""}${menu.options.map((o) => `<button type="button" class="cm-opt" data-delta="${o.delta}">${esc(o.label)}</button>`).join("")}</div>`;
  }
  if (busy) h += `<div class="cm-status"><span class="spin">✻</span> Working…</div>`;
  if (tab.exited) h += `<div class="cm-status muted">Session ended</div>`;
  if (h === el.dataset.html) return;
  const list = $("#chat-list");
  const stick = el.scrollTop + el.clientHeight >= el.scrollHeight - 80;
  const open = new Set([...list.querySelectorAll("details[open]")].map((d) => d.dataset.k));
  el.dataset.html = h;
  list.innerHTML = h;
  for (const d of list.querySelectorAll("details")) if (open.has(d.dataset.k)) d.open = true;
  if (stick) el.scrollTop = el.scrollHeight;
  fillChatImages(tab);
}

// ---- images the agent sends: shown in the chat ----

// An image file the agent names in a reply ("…/scratchpad/sample.png"): a
// full path (or ~/…), not part of a web address.
const IMG_PATH = /(?<![\w:/.~-])(?:~\/|\/)[^\s`'"<>()[\]*|,;]+?\.(?:png|jpe?g|gif|webp)(?![\w/-]|\.\w)/gi;
const imagePaths = (text) => [...new Set(text.match(IMG_PATH) || [])].slice(0, 6);

// chatImagesHTML is a place for each image a reply names; fillChatImages
// loads them (the Mac reads only paths the agent itself wrote).
function chatImagesHTML(text) {
  const paths = imagePaths(text);
  if (!paths.length) return "";
  return `<div class="cm-imgs">${paths
    .map((p) => `<button type="button" class="cm-img" data-img="${esc(p)}" aria-label="${esc(baseName(p))}"><span class="cm-img-note">Loading ${esc(baseName(p))}…</span></button>`)
    .join("")}</div>`;
}

function fillChatImages(tab) {
  const chat = $("#chat");
  for (const b of $("#chat-list").querySelectorAll(".cm-img:not([data-done])")) {
    const path = b.dataset.img;
    b.dataset.done = "1";
    tabImage(tab, path).then(
      (url) => {
        // a picture makes the chat taller: stay at the bottom if we were there
        const stick = chat.scrollTop + chat.clientHeight >= chat.scrollHeight - 80;
        const img = new Image();
        img.alt = baseName(path);
        img.decoding = "async";
        img.onload = () => stick && (chat.scrollTop = chat.scrollHeight);
        img.src = url;
        b.replaceChildren(img);
        b.classList.add("ready");
        b.title = path;
      },
      (err) => {
        b.classList.add("failed");
        b.disabled = true;
        b.innerHTML = `<span class="cm-img-note">🖼 ${esc(baseName(path))} · ${esc(String(err).replace(/^Error: /, ""))}</span>`;
      },
    );
  }
}

// showImageView opens a picture from the chat full screen; a tap closes it.
function showImageView(src, name) {
  let v = $("#img-view");
  if (!v) {
    v = document.createElement("div");
    v.id = "img-view";
    v.setAttribute("role", "dialog");
    v.setAttribute("aria-label", "Image");
    v.innerHTML = `<img alt=""><button type="button" class="iv-close" aria-label="Close">✕</button><div class="iv-name"></div>`;
    v.addEventListener("click", () => v.classList.add("hidden"));
    // Esc closes it, before the terminal underneath can take the key
    window.addEventListener("keydown", (e) => {
      if (e.key !== "Escape" || v.classList.contains("hidden")) return;
      e.preventDefault();
      e.stopPropagation();
      v.classList.add("hidden");
    }, true);
    document.body.appendChild(v);
  }
  v.querySelector("img").src = src;
  v.querySelector("img").alt = name;
  v.querySelector(".iv-name").textContent = name;
  v.classList.remove("hidden");
}

function wireChat() {
  $("#chat-list").addEventListener("click", (e) => {
    const pic = e.target.closest(".cm-img.ready img");
    if (pic) return showImageView(pic.src, pic.alt);
    const b = e.target.closest(".cm-opt");
    const tab = activeTab();
    if (!b || !tab) return;
    if (!canType()) return watchOnly();
    const d = Number(b.dataset.delta);
    claimTab(tab);
    tab.lastInput = Date.now();
    Go().Write(tab.id, (d > 0 ? "\x1b[B".repeat(d) : "\x1b[A".repeat(-d)) + "\r");
    b.classList.add("chosen");
  });
  $("#btn-view").addEventListener("click", () => {
    const tab = activeTab();
    if (!tab || mirrors(tab)) return;
    try {
      localStorage.setItem(viewKey(tab.harness), chatOn(tab) ? "terminal" : "chat");
    } catch {}
    $("#chat").dataset.html = "";
    applySessionView();
    pollChat();
  });
  every(1000, () => {
    updateSend();
    pollChat();
  }, { behind: 1 });
}

// ---------- keyboard shortcuts (Help → Keyboard Shortcuts, ⌘/) ----------

function wireShortcuts() {
  $("#keys-close").addEventListener("click", closeShortcuts);
  $("#keys-dialog").addEventListener("mousedown", (e) => e.target.id === "keys-dialog" && closeShortcuts());
  $("#keys-dialog").addEventListener("keydown", (e) => e.key === "Escape" && closeShortcuts());
  document.addEventListener("click", (e) => e.target.closest("[data-shortcuts]") && openShortcuts());
}

// ---------- the same keys in a browser ----------

// The Mac app's shortcuts are its menus (main.go); a browser has no menus,
// so the page listens for the same keys itself. A PC has no ⌘ and Ctrl
// belongs to the terminal, so Alt stands in for ⌘: ⌘P is Alt+P, ⇧⌘P
// Alt+Shift+P and ⌥⌘T Ctrl+Alt+T. On a Mac, Chrome keeps ⌘T, ⌘W and
// ⇧⌘[ ⇧⌘] for its own tabs; with ⌃ added (⌃⌘T…) they reach Quarters.

// keyText writes the shortcuts in s for this keyboard.
function keyText(s) {
  if (!REMOTE || APPLE) return s;
  return s.replace(/([⌥⇧]?)⌘/g, (_, m) => (m === "⇧" ? "Alt+Shift+" : m === "⌥" ? "Ctrl+Alt+" : "Alt+"));
}

const KEY_CODES = { BracketLeft: "[", BracketRight: "]", Equal: "=", Minus: "-", Comma: ",", Slash: "/", Enter: "↵", NumpadEnter: "↵", NumpadAdd: "=", NumpadSubtract: "-" };

// comboOf is a key press as the menus write it (⇧⌘T), or "" if it has no ⌘.
function comboOf(e) {
  if (APPLE ? !e.metaKey : !e.altKey || e.metaKey) return "";
  const opt = APPLE ? e.altKey : e.ctrlKey;
  const k = /^[a-z]$/i.test(e.key) ? e.key.toUpperCase() : KEY_CODES[e.code] || (/^(Key[A-Z]|Digit\d)$/.test(e.code) ? e.code.slice(-1) : "");
  return k && `${opt ? "⌥" : ""}${e.shiftKey ? "⇧" : ""}⌘${k}`;
}

function wireBrowserKeys() {
  const keys = {};
  for (const c of COMMANDS) if (c.keys) keys[c.keys] = c.action;
  Object.assign(keys, { "⌘P": "palette", "⇧⌘P": "commands" });
  for (let n = 1; n <= 9; n++) keys[`⌘${n}`] = `tab-${n}`;
  // the capture phase, so the key never reaches the terminal (Alt+P would type ESC p)
  window.addEventListener("keydown", (e) => {
    const action = keys[comboOf(e)];
    if (!action) return;
    if (action === "save-file" && e.target.closest?.(".overlay")) return; // the dialog's own ⌘S
    e.preventDefault();
    e.stopPropagation();
    if (!e.repeat || action.startsWith("font-")) onMenu(action);
  }, true);
  // a PC: the labels say Alt, not ⌘
  if (APPLE) return;
  const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let n = walk.nextNode(); n; n = walk.nextNode()) if (n.data.includes("⌘")) n.data = keyText(n.data);
  for (const el of document.querySelectorAll("[title*='⌘']")) el.title = keyText(el.title);
}

// ---------- who's waiting for you (⌘J) ----------

// waitingTabs are the agent tabs that finished while you weren't looking
// (see checkAttention), the one waiting longest first.
function waitingTabs() {
  const out = [];
  for (const list of Object.values(state.tabs)) for (const t of list) if (t.unseen && !t.exited) out.push(t);
  return out.sort((a, b) => (a.doneAt || 0) - (b.doneAt || 0));
}

// waitingIn is a project's tab that has waited for you longest, if any.
const waitingIn = (path) => waitingTabs().find((t) => t.path === path) || null;

// activeProjects are the projects with open sessions: those waiting for
// you first (longest first), then the working ones, then the rest.
function activeProjects(list) {
  const rank = (p) => (waitingIn(p.path) ? 0 : projectStatus(p.path) === "busy" ? 1 : 2);
  return list
    .filter((p) => projectStatus(p.path) !== "idle")
    .map((p, i) => ({ p, i, r: rank(p), at: waitingIn(p.path)?.doneAt || 0 }))
    .sort((a, b) => a.r - b.r || a.at - b.at || a.i - b.i)
    .map((x) => x.p);
}

// asking: the agent shows a question or a permission prompt (Claude Code's
// menus; other agents' aren't recognised yet).
const asking = (tab) => isClaude(tab) && !!tab.term && !!chatMenu(tab.term);

const waitLabel = (t) => `${asking(t) ? "asks you" : "done"}${fmtAgo(t.doneAt) === "now" ? "" : ` · ${fmtAgo(t.doneAt)}`}`;

function nextWaiting() {
  const t = waitingTabs()[0];
  if (!t) return flash("Nobody is waiting for you");
  closeDrawers();
  openProject(t.path);
  selectTab(tabsOf(t.path).indexOf(t));
}

// ---------- the prompt queue: sent one by one as the agent finishes ----------

async function loadQueues() {
  try {
    const list = (await Go().Queues()) || [];
    state.queues = Object.fromEntries(list.map((q) => [q.id, q.items]));
  } catch {}
}

// readyForPrompt: the agent shows its own input box, empty: not a
// question, a permission prompt, or something you started typing.
function readyForPrompt(t) {
  if (!isClaude(t)) return true; // other agents: Quarters goes by their being quiet (QueueNext)
  const top = claudeBoxTop(t.term);
  if (top < 0) return false;
  const buf = t.term.buffer.active, line = buf.getLine(buf.baseY + top + 1);
  if (!line) return false;
  const cell = buf.getNullCell();
  let mark = false;
  for (let x = 0; x < line.length; x++) {
    line.getCell(x, cell);
    const ch = cell.getChars();
    if (!mark) mark = ch === "❯" || ch === ">";
    else if (ch.trim() && !cell.isDim()) return false; // your draft (the hint in an empty box is dim)
  }
  return mark;
}

// runQueues asks Quarters to send a session's next prompt when its agent
// is quiet and ready for it. Every window does this; Quarters sends each
// prompt once. A window that isn't drawing the session (in use on another
// device) can't see its screen, so it leaves it to that one.
const queueAsked = {};
function runQueues() {
  if (!on("prompt_queue") || !canType()) return;
  const now = Date.now();
  for (const [id, items] of Object.entries(state.queues)) {
    const t = state.byId[id];
    if (!t || !items.length || t.exited || !t.synced || isPaused(t)) continue;
    if (now - t.lastData < QUIET_MS || now - (t.lastInput || 0) < QUIET_MS || now - (queueAsked[id] || 0) < 1500) continue;
    if (!readyForPrompt(t)) continue;
    queueAsked[id] = now;
    Promise.resolve(Go().QueueNext(t.id)).catch(() => {});
  }
}

function renderPanelQueue(cur) {
  if (!on("prompt_queue") || mirrors(cur) || cur.exited || !cur.id) return "";
  const items = state.queues[cur.id] || [];
  const name = esc(shortAgent(cur.harness));
  const rows = items
    .map((p, i) => `<div class="pq-row" data-pq="${i}"><span class="pq-n muted">${i + 1}</span><span class="pq-text" title="${esc(p)}">${esc(p)}</span>
      <button class="pa-btn" type="button" data-pq-up title="Move up"${i ? "" : " disabled"}>↑</button><button class="pa-btn pa-del" type="button" data-pq-del title="Remove">×</button></div>`)
    .join("");
  const note = !items.length ? "" : tabStatus(cur) === "busy" ? `The next one goes in when ${name} finishes.` : isPaused(cur) ? "Goes on on the device using this session." : readyForPrompt(cur) ? "The next one goes in a moment." : `${name} is waiting for you: answer it, and the queue goes on.`;
  return `<div class="pq-box"><div class="sec-row"><span class="sec">QUEUE${items.length ? ` · ${items.length}` : ""}</span><button class="pa-add" type="button" data-pq-add title="A prompt to send when ${name} finishes">＋ add prompt</button></div>${rows ? `<div class="pa-list">${rows}</div>` : ""}${note ? `<div class="muted pq-note">${note}</div>` : ""}</div>`;
}

// ---------- usage by project (⇧⌘U) ----------

let usageAt = 0;
async function refreshUsage(force) {
  if (!on("usage") || (!force && Date.now() - usageAt < 60000)) return;
  usageAt = Date.now();
  try {
    state.usage = await Go().Usage();
    renderStats();
    if (!$("#usage-dialog").classList.contains("hidden")) renderUsage();
  } catch {}
}

function bars(days, n, height) {
  const list = days.slice(-n), peak = Math.max(1, ...list);
  return `<div class="chart us-chart" style="height:${height}px">${list
    .map((v, i) => `<div class="${!v ? "zero" : i === list.length - 1 ? "now" : ""}" style="height:${v ? Math.max(3, Math.round((height * v) / peak)) : 2}px" title="${esc(state.usage.days[state.usage.days.length - list.length + i])}: ${fmtTokens(v)} tokens"></div>`)
    .join("")}</div>`;
}

function renderPanelUsage(path) {
  const u = on("usage") && state.usage;
  const me = u && u.projects.find((p) => p.path === path);
  const p = project(path);
  if (!me || (!me.month && p && p.agent !== "claude")) return "";
  const ranked = u.projects.filter((x) => x.week > 0);
  const rank = ranked.indexOf(me) + 1;
  const share = u.total.week ? Math.round((100 * me.week) / u.total.week) : 0;
  return `<div class="divider"></div>
    <div class="sec-row"><span class="sec">USAGE</span><button class="pa-add" type="button" data-usage title="Every project's tokens (⇧⌘U)">all projects ›</button></div>
    <div class="tiles us-tiles">
      <div class="tile"><div class="t-label">today</div><div class="t-value">${fmtTokens(me.today)}</div></div>
      <div class="tile"><div class="t-label">7 days</div><div class="t-value">${fmtTokens(me.week)}</div></div>
      <div class="tile"><div class="t-label">30 days</div><div class="t-value">${fmtTokens(me.month)}</div></div>
    </div>
    <div class="kv" style="gap:6px"><div class="row" style="font-size:12px"><span class="muted">tokens / day</span><span class="muted">${me.week ? `${share}% of this week's · #${rank} of ${ranked.length}` : "none this week"}</span></div>${bars(me.daily, 14, 36)}</div>`;
}

// ---------- agent instructions (CLAUDE.md, AGENTS.md…) and notes ----------

async function refreshInstructions(path, force) {
  if (!path || !on("instructions")) return;
  const cur = state.instructions[path];
  if (cur && !force && Date.now() - cur.at < 15000) return;
  state.instructions[path] = { files: [], notes: "", ...cur, at: Date.now() };
  try {
    const v = await Go().Instructions(path);
    state.instructions[path] = { ...v, at: Date.now() };
    renderStats();
  } catch {}
}

function renderPanelInstructions(path) {
  const v = on("instructions") && state.instructions[path];
  if (!v || !v.at) return "";
  const own = project(path)?.agent;
  const rows = (v.files || [])
    .map((f) => `<button type="button" class="row file-row in-row" data-instr="${esc(f.name)}" title="${esc(f.name)}: read by ${esc(f.agents.join(", "))}">
      <span class="path"><span class="violet st">${f.exists ? "§" : "+"}</span>${esc(f.name)} <span class="muted">${esc(f.agents.length > 2 ? `${f.agents.length} agents` : f.agents.map((a) => a.replace(/ (Code|CLI)$/, "")).join(", "))}</span></span>
      <span class="muted">${f.exists ? `${f.lines} line${f.lines === 1 ? "" : "s"}` : `create`}</span></button>`)
    .join("");
  return `<div class="divider"></div><div class="sec-row"><span class="sec">INSTRUCTIONS</span><span class="muted note">${own ? `${esc(shortAgent(own))} reads ${esc((state.settings?.harnesses || []).find((h) => h.id === own)?.instructions || "")}` : ""}</span></div>
    <div class="files">${rows}
      <button type="button" class="row file-row in-row" data-notes title="Your notes on this project: kept by Quarters, not in the project folder">
        <span class="path"><span class="yellow st">✎</span>Notes <span class="muted">${esc(v.notes || "only yours, not the agent's")}</span></span><span class="muted">${v.notes ? "" : "write"}</span></button>
    </div>`;
}

// ---------- the editor: a queued prompt, an instructions file, notes ----------

function wirePanelExtras() {
  $("#stats").addEventListener("click", (e) => {
    const path = state.statsPath, cur = activeTab();
    if (e.target.closest("[data-usage]")) return openUsage();
    const instr = e.target.closest("[data-instr]");
    if (instr) return openEditor({ kind: "instructions", path, name: instr.dataset.instr });
    if (e.target.closest("[data-notes]")) return openEditor({ kind: "notes", path });
    if (!cur || cur.path !== path) return;
    if (e.target.closest("[data-pq-add]")) {
      if (!canType()) return watchOnly();
      return openEditor({ kind: "queue", path, tabId: cur.id, agent: shortAgent(cur.harness) });
    }
    const row = e.target.closest("[data-pq]");
    if (!row) return;
    if (!canType()) return watchOnly();
    const i = Number(row.dataset.pq);
    const done = (p) => Promise.resolve(p).catch((err) => flash(String(err).replace(/^Error: /, "")));
    if (e.target.closest("[data-pq-del]")) done(Go().QueueRemove(cur.id, i));
    else if (e.target.closest("[data-pq-up]")) done(Go().QueueMove(cur.id, i, -1));
  });
  $("#tb-waiting").addEventListener("click", nextWaiting);
}

// ---------- parts loaded when first used (ui/*.js) ----------

// Dialogs most visits never open live in their own files, so a phone
// doesn't download them up front. They import what they need from here.
const uiLoads = {};
function ui(file) {
  return (uiLoads[file] ||= import(`./ui/${file}`).catch((e) => {
    delete uiLoads[file];
    flash(`couldn't load ${file}: ${e.message || e}`);
    throw e;
  }));
}

function openEmoji(...args) {
  return ui("emoji.js").then((m) => m.openEmoji(...args));
}

function openUpdate(...args) {
  return ui("update.js").then((m) => m.openUpdate(...args));
}

function openShortcuts(...args) {
  return ui("shortcuts.js").then((m) => m.openShortcuts(...args));
}

function closeShortcuts(...args) {
  return ui("shortcuts.js").then((m) => m.closeShortcuts(...args));
}

function openHistory(...args) {
  return ui("history.js").then((m) => m.openHistory(...args));
}

function showDiff(...args) {
  return ui("diff.js").then((m) => m.showDiff(...args));
}

function openUsage(...args) {
  return ui("usage.js").then((m) => m.openUsage(...args));
}

function renderUsage(...args) {
  return ui("usage.js").then((m) => m.renderUsage(...args));
}

function openEditor(...args) {
  return ui("editor.js").then((m) => m.openEditor(...args));
}

function openSettings(...args) {
  return ui("settings.js").then((m) => m.openSettings(...args));
}

function renderSettings(...args) {
  return ui("settings.js").then((m) => m.renderSettings(...args));
}

function openProjectDialog(...args) {
  return ui("project-dialog.js").then((m) => m.openProjectDialog(...args));
}

function toggleRemote(...args) {
  return ui("remote-dialog.js").then((m) => m.toggleRemote(...args));
}

function openRemoteDialog(...args) {
  return ui("remote-dialog.js").then((m) => m.openRemoteDialog(...args));
}

// ---------- tips: always one at the bottom right (ui/tips.js) ----------

const TIPS_KEY = "quarters.tips";

function tipsState() {
  try {
    return JSON.parse(localStorage.getItem(TIPS_KEY) || "{}");
  } catch {
    return {};
  }
}

function saveTips(t) {
  try {
    localStorage.setItem(TIPS_KEY, JSON.stringify(t));
  } catch {}
}

// learned notes a key or a button you've used, so its tip comes last
function learned(id) {
  const t = tipsState();
  if (t.used?.includes(id)) return;
  (t.used ||= []).push(id);
  saveTips(t);
  document.dispatchEvent(new CustomEvent("tips:learned", { detail: id }));
}

// tipsDue: the screen has keys (a phone's status bar is for messages)
const tipsDue = () => !isTouch();

function startTips(...args) {
  return ui("tips.js").then((m) => m.startTips(...args));
}

export {
  $,
  APPLE,
  Go,
  PROJECT_COLORS,
  REMOTE,
  activeTab,
  agentName,
  autosForDialog,
  autosToSave,
  bars,
  baseName,
  canChange,
  canType,
  claimTab,
  closeDrawers,
  closeFileTab,
  closeTab,
  dlg,
  dropdown,
  emojiIcon,
  esc,
  fileTabsOf,
  filesRoot,
  flash,
  fmtAgo,
  fmtTokens,
  fuzzy,
  harnessOptions,
  iconData,
  iconHTML,
  isNarrow,
  isTouch,
  keyText,
  learned,
  loadIcon,
  loadProjects,
  loadSettings,
  mark,
  monogram,
  newKey,
  newTab,
  on,
  onMenu,
  openEmoji,
  openFileAt,
  refreshDev,
  refreshStats,
  openDrawer,
  openLink,
  openProject,
  openRemoteDialog,
  project,
  projectColor,
  refreshInstructions,
  refreshUsage,
  render,
  renderRemotePill,
  renderTabs,
  rmd,
  saveTips,
  selectTab,
  set,
  setFocus,
  setTheme,
  shellQuote,
  showActive,
  showFileTab,
  showImageView,
  shownFile,
  state,
  tabsOf,
  theme,
  tipsDue,
  tipsState,
  watchOnly,
};

// The files panel (⇧⌘E): the project's folders and files, git's marks on
// them, and a filter that finds any file. A click opens a file in an editor
// tab (ui/fileview.js). Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  REMOTE,
  activeTab,
  baseName,
  canType,
  claimTab,
  closeDrawers,
  esc,
  filesRoot,
  flash,
  fuzzy,
  isNarrow,
  learned,
  shellQuote,
  state,
} from "../app.js";

// What the panel knows about each folder it showed (projects and worktrees).
const roots = new Map(); // dir -> { open: Set(rel), dirs: Map(rel -> DirView), status: {}, changed: Set(rel), list, at }
let shown = ""; // the folder on screen
let sel = ""; // the row picked with the keyboard (a path), or ""
const REFRESH_MS = 8000;

const rootOf = (dir) => {
  if (!roots.has(dir)) roots.set(dir, { open: new Set([""]), dirs: new Map(), status: {}, changed: new Set(), list: null, at: 0 });
  return roots.get(dir);
};
const join = (dir, name) => (dir ? `${dir}/${name}` : name);
const errText = (e) => String(e).replace(/^Error: /, "");

// refreshFiles shows the folder of the project in view; it reads the disk
// again when forced, when the folder changed, or every few seconds.
export async function refreshFiles(force) {
  wire();
  const root = filesRoot();
  const dir = root ? root.dir : "";
  const p = root && state.projects.find((x) => x.path === root.path);
  $("#files-root").textContent = root ? (dir !== root.path ? `⎇ ${baseName(dir)}` : p ? p.name : "") : "";
  if (!root) {
    shown = "";
    $("#file-tree").innerHTML = '<div class="fl-empty muted">Pick a project to see its files.</div>';
    return;
  }
  const r = rootOf(dir);
  const fresh = dir === shown && Date.now() - r.at < REFRESH_MS;
  if (!force && fresh) return;
  if (dir !== shown) {
    shown = dir;
    sel = "";
    $("#files-filter").value = "";
    draw(); // what we have, at once
  }
  r.at = Date.now();
  if (force) r.list = null;
  const filtering = dir === shown && $("#files-filter").value.trim();
  await Promise.all([...r.open].map((rel) => loadDir(dir, rel)).concat(loadStatus(dir), filtering ? loadList(dir) : []));
  if (dir === shown) draw();
}

// loadList reads every file's path, for the filter (once, until a refresh).
async function loadList(dir) {
  const r = rootOf(dir);
  if (r.list) return;
  try {
    r.list = await Go().ListFiles(dir);
    if (r.list.more) flash("a big project: the filter looks at the first 20,000 files");
  } catch (e) {
    r.list = { files: [] };
    flash(errText(e));
  }
}

async function loadDir(dir, rel) {
  const r = rootOf(dir);
  try {
    r.dirs.set(rel, await Go().ListDir(dir, rel));
  } catch (e) {
    r.dirs.delete(rel);
    r.open.delete(rel); // gone, or never a folder
    if (rel === "") flash(errText(e));
  }
}

async function loadStatus(dir) {
  const r = rootOf(dir);
  try {
    r.status = (await Go().FileStatus(dir)) || {};
  } catch {
    r.status = {};
  }
  // a folder shows that something inside it changed
  r.changed = new Set();
  for (const path of Object.keys(r.status)) {
    const parts = path.split("/");
    for (let i = 1; i < parts.length; i++) r.changed.add(parts.slice(0, i).join("/"));
  }
}

const MARK = { M: ["M", "yellow", "changed"], A: ["A", "green", "added"], D: ["D", "red", "deleted"], "?": ["U", "green", "new: not in git yet"] };

function rowHTML(rel, name, e, depth, r) {
  const st = r.status[rel], m = MARK[st];
  const cls = ["fl-row", e.dir ? "dir" : "file", e.ignored ? "ignored" : "", rel === sel ? "sel" : "", st ? `st-${st === "?" ? "U" : st}` : ""].filter(Boolean).join(" ");
  const twisty = e.dir ? `<span class="fl-tw">${r.open.has(rel) ? "▾" : "▸"}</span>` : '<span class="fl-tw"></span>';
  const mark = m ? `<span class="fl-mark ${m[1]}" title="${m[2]}">${m[0]}</span>` : e.dir && r.changed.has(rel) ? '<span class="fl-mark yellow" title="changes inside">•</span>' : "";
  return `<div class="${cls}" role="treeitem" data-rel="${esc(rel)}" data-dir="${e.dir ? 1 : 0}" style="--depth:${depth}" title="${esc(rel)}${e.link ? " (link)" : ""}">
    ${twisty}<span class="fl-ico ${e.dir ? "dir" : "file"}" aria-hidden="true"></span><span class="fl-name">${esc(name)}</span>${mark}</div>`;
}

function treeHTML(r, rel, depth) {
  const v = r.dirs.get(rel);
  if (!v) return depth === 0 ? '<div class="fl-empty muted">Reading…</div>' : "";
  let html = "";
  for (const e of v.entries || []) {
    const path = join(rel, e.name);
    html += rowHTML(path, e.name, e, depth, r);
    if (e.dir && r.open.has(path)) html += treeHTML(r, path, depth + 1);
  }
  if (v.more) html += `<div class="fl-more muted" style="--depth:${depth}">… and ${v.more} more</div>`;
  if (depth === 0 && !(v.entries || []).length) html += '<div class="fl-empty muted">An empty folder.</div>';
  return html;
}

// the filter: every file whose path matches, best first
function matchesHTML(r, q) {
  if (!r.list) return '<div class="fl-empty muted">Finding files…</div>';
  const found = [];
  for (const path of r.list.files || []) {
    const m = fuzzy(q, path);
    if (!m) continue;
    // the name counts more than the folders it's in
    const name = fuzzy(q, path.split("/").pop());
    found.push({ path, score: m.score + (name ? name.score * 2 : 0) - path.length / 100 });
  }
  found.sort((a, b) => b.score - a.score);
  if (!found.length) return '<div class="fl-empty muted">No file matches.</div>';
  return found.slice(0, 200).map(({ path }) => {
    const i = path.lastIndexOf("/");
    const st = r.status[path], m = MARK[st];
    return `<div class="fl-row file found${path === sel ? " sel" : ""}" role="treeitem" data-rel="${esc(path)}" data-dir="0" style="--depth:0" title="${esc(path)}">
      <span class="fl-ico file" aria-hidden="true"></span><span class="fl-name">${esc(path.slice(i + 1))}</span><span class="fl-in muted">${esc(path.slice(0, i))}</span>${m ? `<span class="fl-mark ${m[1]}">${m[0]}</span>` : ""}</div>`;
  }).join("");
}

function draw() {
  const box = $("#file-tree");
  if (!shown) return;
  const r = rootOf(shown);
  const q = $("#files-filter").value.trim();
  const html = q ? matchesHTML(r, q) : treeHTML(r, "", 0);
  if (box.dataset.html === html) return;
  const top = box.scrollTop;
  box.innerHTML = html;
  box.dataset.html = html;
  box.scrollTop = top;
}

async function toggleDir(rel) {
  const r = rootOf(shown);
  if (r.open.has(rel)) r.open.delete(rel);
  else {
    r.open.add(rel);
    if (!r.dirs.has(rel)) {
      draw();
      await loadDir(shown, rel);
    }
  }
  draw();
}

function openFile(rel) {
  const root = filesRoot();
  if (!root) return;
  if (isNarrow()) closeDrawers();
  learned("open-file");
  import("./fileview.js").then((m) => m.openFile(root.path, root.dir, rel));
}

// ---------- the menu on a file (right-click) ----------

function showMenu(rel, isDir, x, y) {
  const menu = $("#file-menu");
  const full = `${shown}/${rel}`;
  const items = [
    !isDir && ["open", "Open", "↵"],
    !REMOTE && ["mac", isDir ? "Open in Finder" : "Open with its app", "↗"],
    !REMOTE && ["reveal", "Show in Finder", "⌕"],
    ["copy", "Copy path", "⧉"],
    ["copy-rel", "Copy relative path", "⧉"],
    activeTab() && canType() && ["type", "Type the path in the terminal", "›_"],
  ].filter(Boolean);
  menu.innerHTML = `<div class="am-head">${esc(rel)}</div>` + items.map(([act, label, ico]) =>
    `<button class="am-item" type="button" data-fact="${act}"><span class="muted">${ico}</span><span class="grow">${esc(label)}</span></button>`).join("");
  menu.dataset.rel = rel;
  menu.dataset.full = full;
  menu.classList.remove("hidden");
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.max(8, Math.min(innerWidth - w - 8, x)) + "px";
  menu.style.top = Math.max(8, Math.min(innerHeight - h - 8, y)) + "px";
}

const hideMenu = () => $("#file-menu").classList.add("hidden");

async function menuAction(act, rel, full) {
  hideMenu();
  const copy = (text) => navigator.clipboard?.writeText(text).then(() => flash("copied"), () => flash("couldn't copy"));
  try {
    if (act === "open") openFile(rel);
    else if (act === "mac") await Go().OpenFile(shown, rel);
    else if (act === "reveal") await Go().RevealFile(shown, rel);
    else if (act === "copy") copy(full);
    else if (act === "copy-rel") copy(rel);
    else if (act === "type") {
      const t = activeTab();
      if (!t || t.exited) return;
      claimTab(t);
      t.term.paste(shellQuote(full) + " ");
    }
  } catch (e) {
    flash(errText(e));
  }
}

// ---------- the keyboard: ↑ ↓ pick, → ← open and close folders, ↵ opens ----------

function rows() {
  return [...$("#file-tree").querySelectorAll(".fl-row")];
}

function pick(rel, scroll = true) {
  sel = rel;
  for (const el of rows()) el.classList.toggle("sel", el.dataset.rel === rel);
  delete $("#file-tree").dataset.html; // the next draw keeps the pick
  if (scroll) $("#file-tree").querySelector(".fl-row.sel")?.scrollIntoView({ block: "nearest" });
}

function onKey(e) {
  const list = rows();
  if (!list.length) return;
  const i = list.findIndex((el) => el.dataset.rel === sel);
  const row = list[i];
  const move = (d) => pick(list[Math.max(0, Math.min(list.length - 1, i < 0 ? 0 : i + d))].dataset.rel);
  switch (e.key) {
    case "ArrowDown":
      move(1);
      break;
    case "ArrowUp":
      if (i <= 0 && e.target === $("#files-filter")) return;
      if (i <= 0) $("#files-filter").focus();
      else move(-1);
      break;
    case "ArrowRight":
      if (e.target === $("#files-filter") || !row || row.dataset.dir !== "1") return;
      if (!rootOf(shown).open.has(sel)) toggleDir(sel);
      break;
    case "ArrowLeft": {
      if (e.target === $("#files-filter") || !row) return;
      if (row.dataset.dir === "1" && rootOf(shown).open.has(sel)) toggleDir(sel);
      else if (sel.includes("/")) pick(sel.slice(0, sel.lastIndexOf("/")));
      break;
    }
    case "Enter":
      if (!row && i < 0 && list[0]) return activate(list[0]);
      if (row) activate(row);
      break;
    case "Escape":
      if ($("#files-filter").value) {
        $("#files-filter").value = "";
        draw();
      } else activeTab()?.term.focus();
      break;
    default:
      return;
  }
  e.preventDefault();
}

function activate(row) {
  sel = row.dataset.rel;
  if (row.dataset.dir === "1") toggleDir(sel);
  else openFile(sel);
}

let wired = false;
function wire() {
  if (wired) return;
  wired = true;
  const tree = $("#file-tree");
  tree.tabIndex = 0;
  tree.addEventListener("click", (e) => {
    const row = e.target.closest(".fl-row");
    if (!row) return;
    pick(row.dataset.rel, false);
    activate(row);
  });
  tree.addEventListener("contextmenu", (e) => {
    const row = e.target.closest(".fl-row");
    if (!row) return;
    e.preventDefault();
    pick(row.dataset.rel, false);
    showMenu(row.dataset.rel, row.dataset.dir === "1", e.clientX, e.clientY);
  });
  tree.addEventListener("keydown", onKey);
  $("#files-filter").addEventListener("keydown", onKey);
  $("#files-filter").addEventListener("input", async () => {
    sel = "";
    const r = shown && rootOf(shown);
    if (!r) return;
    draw();
    if (!r.list && $("#files-filter").value.trim()) {
      const dir = shown;
      await loadList(dir);
      if (dir === shown) draw();
    }
  });
  $("#files-refresh").addEventListener("click", () => refreshFiles(true));
  $("#file-menu").addEventListener("click", (e) => {
    const b = e.target.closest("[data-fact]");
    if (b) menuAction(b.dataset.fact, $("#file-menu").dataset.rel, $("#file-menu").dataset.full);
  });
  window.addEventListener("mousedown", (e) => {
    if (!e.target.closest("#file-menu")) hideMenu();
  }, true);
  window.addEventListener("keydown", (e) => e.key === "Escape" && hideMenu(), true);
  // what agents changed while you were in another app
  window.addEventListener("focus", () => (state.showFiles || document.body.classList.contains("drawer-files")) && refreshFiles(true));
  setInterval(() => {
    if (document.visibilityState === "visible" && document.hasFocus() && (state.showFiles || document.body.classList.contains("drawer-files"))) refreshFiles();
  }, REFRESH_MS);
}

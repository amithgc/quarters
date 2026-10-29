// Past conversations: search them and pick one up again (⇧⌘F).
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  activeTab,
  canChange,
  esc,
  flash,
  fmtAgo,
  isTouch,
  mark,
  newTab,
  on,
  openProject,
  project,
  selectTab,
  state,
  tabsOf,
  watchOnly,
} from "../app.js";

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

wireHistory();

export { openHistory };

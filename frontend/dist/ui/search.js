// Search every project (⇧⌘K): ripgrep (or git grep) across all of them,
// results by project and file; a hit opens the file at its line.
// Loaded the first time it's needed (see ui() in app.js).

import { $, Go, esc, iconHTML, openFileAt, project } from "../app.js";

const sx = { opts: { case: false, word: false, regex: false }, hits: [], sel: 0, seq: 0, timer: 0 };

export function openSearch(text) {
  $("#search-dialog").classList.remove("hidden");
  const q = $("#sx-q");
  if (text) q.value = text;
  q.focus();
  q.select();
  if (q.value.trim()) run();
}

function close() {
  $("#search-dialog").classList.add("hidden");
}

function later() {
  clearTimeout(sx.timer);
  sx.timer = setTimeout(run, 250);
}

async function run() {
  const q = $("#sx-q").value;
  const seq = ++sx.seq;
  if (!q.trim()) {
    sx.hits = [];
    $("#sx-list").innerHTML = "";
    $("#sx-status").textContent = "";
    return;
  }
  $("#sx-status").textContent = "searching…";
  const t0 = performance.now();
  let r;
  try {
    r = await Go().SearchProjects(q, { ...sx.opts, glob: $("#sx-glob").value.trim() });
  } catch (e) {
    if (seq === sx.seq) $("#sx-status").textContent = String(e).replace(/^Error: /, "");
    return;
  }
  if (seq !== sx.seq) return; // a newer search is on its way
  sx.hits = r.hits || [];
  sx.sel = 0;
  render();
  const files = new Set(sx.hits.map((h) => h.project + "/" + h.path)).size;
  const projects = new Set(sx.hits.map((h) => h.project)).size;
  $("#sx-status").textContent = sx.hits.length
    ? `${sx.hits.length}${r.more ? "+" : ""} match${sx.hits.length === 1 && !r.more ? "" : "es"} in ${files} file${files === 1 ? "" : "s"}, ${projects} project${projects === 1 ? "" : "s"} · ${r.tool} · ${Math.round(performance.now() - t0)} ms`
    : `nothing found · ${r.tool}`;
}

function render() {
  let html = "", lastProject = "", lastFile = "";
  sx.hits.forEach((h, i) => {
    if (h.project !== lastProject) {
      const p = project(h.project);
      html += `<div class="sx-project">${p ? iconHTML(p) : ""}<span>${esc(p ? p.name : h.project)}</span></div>`;
      lastProject = h.project;
      lastFile = "";
    }
    if (h.path !== lastFile) {
      const cut = h.path.lastIndexOf("/");
      html += `<div class="sx-file"><span class="muted">${esc(h.path.slice(0, cut + 1))}</span>${esc(h.path.slice(cut + 1))}</div>`;
      lastFile = h.path;
    }
    html += `<button type="button" class="sx-hit${i === sx.sel ? " sel" : ""}" data-i="${i}"><span class="sx-line">${h.line}</span><span class="sx-text">${esc(h.pre)}<mark>${esc(h.match)}</mark>${esc(h.post)}</span></button>`;
  });
  $("#sx-list").innerHTML = html;
}

function select(i) {
  if (!sx.hits.length) return;
  sx.sel = Math.max(0, Math.min(sx.hits.length - 1, i));
  document.querySelectorAll("#sx-list .sx-hit").forEach((b) => b.classList.toggle("sel", +b.dataset.i === sx.sel));
  $(`#sx-list .sx-hit[data-i="${sx.sel}"]`)?.scrollIntoView({ block: "nearest" });
}

function open(i) {
  const h = sx.hits[i];
  if (!h) return;
  close();
  openFileAt(h.project, h.project, h.path, h.line);
}

function wire() {
  const el = $("#search-dialog");
  $("#sx-q").addEventListener("input", later);
  $("#sx-glob").addEventListener("input", later);
  el.querySelectorAll(".sx-opt").forEach((b) =>
    b.addEventListener("click", () => {
      sx.opts[b.dataset.opt] = !sx.opts[b.dataset.opt];
      b.classList.toggle("on", sx.opts[b.dataset.opt]);
      run();
      $("#sx-q").focus();
    }),
  );
  $("#sx-list").addEventListener("click", (e) => {
    const b = e.target.closest("[data-i]");
    if (b) open(+b.dataset.i);
  });
  el.addEventListener("mousedown", (e) => e.target === el && close());
  el.addEventListener("keydown", (e) => {
    if (e.key === "Escape") close();
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") select(sx.sel + (e.key === "ArrowDown" ? 1 : -1));
    else if (e.key === "Enter") open(sx.sel);
    else return;
    e.preventDefault();
    e.stopPropagation();
  });
}

wire();

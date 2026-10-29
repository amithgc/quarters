// Usage by project: every project's tokens, ranked (⇧⌘U).
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  bars,
  closeDrawers,
  esc,
  fmtTokens,
  iconHTML,
  openProject,
  project,
  refreshUsage,
  state,
} from "../app.js";

let usageRange = "week";

function openUsage() {
  $("#usage-dialog").classList.remove("hidden");
  $("#us-close").focus();
  renderUsage();
  refreshUsage(true);
}

function renderUsage() {
  const u = state.usage;
  for (const b of document.querySelectorAll("#us-range button")) b.classList.toggle("on", b.dataset.range === usageRange);
  if (!u) return void ($("#us-list").innerHTML = '<div class="muted">Reading the logs…</div>');
  const rows = [...u.projects].sort((a, b) => b[usageRange] - a[usageRange]).filter((x) => x[usageRange] > 0);
  if (u.other[usageRange] > 0) rows.push(u.other);
  const total = u.total[usageRange], top = Math.max(1, ...rows.map((x) => x[usageRange]));
  const label = { today: "today", week: "the last 7 days", month: "the last 30 days" }[usageRange];
  $("#us-list").innerHTML = !rows.length
    ? `<div class="muted">No Claude Code use ${label}.</div>`
    : `<div class="us-total"><span class="muted">${label}</span><b>${fmtTokens(total)}</b><span class="muted">tokens</span></div>` +
      rows
        .map((x) => {
          const p = x.path && project(x.path);
          const n = x[usageRange];
          return `<button type="button" class="us-row"${x.path ? ` data-open="${esc(x.path)}"` : " disabled"}>
            <span class="us-name">${p ? iconHTML(p) : ""}<span>${esc(x.name)}</span></span>
            <span class="us-bar"><span style="width:${((100 * n) / top).toFixed(1)}%"></span></span>
            <span class="us-n">${fmtTokens(n)}</span><span class="us-pct muted">${total ? Math.round((100 * n) / total) : 0}%</span>
            ${usageRange === "today" ? "" : bars(x.daily, usageRange === "week" ? 7 : 30, 18)}
          </button>`;
        })
        .join("");
}

function closeUsage() {
  $("#usage-dialog").classList.add("hidden");
}

function wireUsage() {
  const el = $("#usage-dialog");
  $("#us-close").addEventListener("click", closeUsage);
  el.addEventListener("mousedown", (e) => e.target === el && closeUsage());
  el.addEventListener("keydown", (e) => e.key === "Escape" && closeUsage());
  $("#us-range").addEventListener("click", (e) => {
    const b = e.target.closest("[data-range]");
    if (!b) return;
    usageRange = b.dataset.range;
    renderUsage();
  });
  $("#us-list").addEventListener("click", (e) => {
    const row = e.target.closest("[data-open]");
    if (!row) return;
    closeUsage();
    closeDrawers();
    state.selected = row.dataset.open;
    openProject(row.dataset.open);
  });
}

wireUsage();

export { openUsage, renderUsage };

// The add / edit project dialog, with its automations editor.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  PROJECT_COLORS,
  REMOTE,
  activeTab,
  agentName,
  autosForDialog,
  autosToSave,
  baseName,
  closeTab,
  dlg,
  dropdown,
  emojiIcon,
  esc,
  flash,
  harnessOptions,
  iconData,
  loadIcon,
  loadProjects,
  monogram,
  on,
  onMenu,
  openEmoji,
  project,
  projectColor,
  render,
  set,
  showActive,
  state,
  tabsOf,
} from "../app.js";

const DAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

const LAYOUTS = [["grid", "grid"], ["columns", "side by side"], ["rows", "stacked"], ["tabs", "separate tabs"]];

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

function renderIconField() {
  const prev = $("#pd-icon-preview");
  const name = $("#pd-name").value.trim() || baseName($("#pd-path").value.trim()) || "?";
  if (emojiIcon(dlg.icon)) {
    prev.innerHTML = `<span class="picon emo">${esc(dlg.icon)}</span>`;
    $("#pd-icon-clear").classList.remove("hidden");
    document.querySelectorAll("#pd-icon-found button").forEach((b) => b.classList.remove("on"));
    return;
  }
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
  const def = agentName(state.settings?.harness);
  $("#pd-harness").innerHTML = `<option value="" data-sep>as in Settings (${esc(def)})</option>` + harnessOptions();
  $("#pd-harness").value = p ? p.harness || "" : "";
  $("#pd-harness").disabled = REMOTE;
  dropdown($("#pd-yolo"));
  dropdown($("#pd-harness"));
  dlg.color = p ? p.color || "" : "";
  renderColorField();
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
}

function renderColorField() {
  const auto = projectColor({ name: $("#pd-name").value.trim() || baseName($("#pd-path").value) || "?" });
  const sw = (v, color, title) => `<button type="button" class="swatch${dlg.color === v ? " on" : ""}" data-color="${v}" style="--sw:${color}" title="${title}"></button>`;
  $("#pd-colors").innerHTML = sw("", auto, "From the name, like its letter tile") + sw("accent", "var(--accent)", "Quarters orange") + PROJECT_COLORS.map((c) => sw(c, c, c)).join("");
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
    const full = await Go().SaveProject(dlg.oldPath, $("#pd-name").value, $("#pd-path").value, $("#pd-group").value, dlg.icon, autosToSave(), $("#pd-yolo").value, $("#pd-harness").value, dlg.color);
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
  $("#pd-config").addEventListener("click", () => onMenu("edit-config"));
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
  $("#pd-icon-emoji").addEventListener("click", () =>
    openEmoji(emojiIcon(dlg.icon) ? dlg.icon : "", (e) => {
      dlg.icon = e;
      renderIconField();
    }));
  $("#pd-group").addEventListener("input", renderGroupChips);
  $("#pd-colors").addEventListener("click", (e) => {
    const b = e.target.closest("[data-color]");
    if (!b) return;
    dlg.color = b.dataset.color;
    renderColorField();
  });
  $("#pd-name").addEventListener("input", () => !dlg.color && renderColorField());
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

wireProjectDialog();

export { openProjectDialog };

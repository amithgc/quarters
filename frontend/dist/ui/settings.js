// The Settings dialog: features, coding agent, appearance and saved prompts.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  REMOTE,
  activeTab,
  agentName,
  canChange,
  esc,
  flash,
  harnessOptions,
  loadProjects,
  loadSettings,
  onMenu,
  openRemoteDialog,
  set,
  setTheme,
  state,
  theme,
} from "../app.js";

async function openSettings() {
  await loadSettings();
  if (!state.settings) return;
  set.open = true;
  set.prompts = (state.settings.prompts || []).map((p) => ({ ...p }));
  $("#set-error").textContent = "";
  renderSettings(true);
  renderThemeSeg();
  $("#settings-dialog").classList.remove("hidden");
  $("#set-close").focus();
}

function renderThemeSeg() {
  for (const b of document.querySelectorAll("#set-theme button")) b.classList.toggle("on", b.dataset.theme === theme);
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
  $("#set-harness").innerHTML = harnessOptions();
  $("#set-harness").value = v.harness;
  $("#set-harness").disabled = REMOTE;
  const h = (v.harnesses || []).find((x) => x.id === v.harness);
  $("#set-harness-note").innerHTML = h && !h.installed
    ? `<span class="red">${esc(h.bin)} isn't installed on this machine</span> · <a href="${esc(h.site)}" target="_blank" rel="noopener">get ${esc(h.name)}</a>`
    : "What new tabs start. Each project can choose its own (✎ edit project).";
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
  $("#set-theme").addEventListener("click", (e) => {
    const b = e.target.closest("[data-theme]");
    if (!b) return;
    setTheme(b.dataset.theme);
    renderThemeSeg();
  });
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
    } else if (e.target.id === "set-harness") {
      try {
        await Go().SetHarness(e.target.value);
        flash(`new tabs start ${agentName(e.target.value)}`);
      } catch (err) {
        $("#set-error").textContent = String(err).replace(/^Error: /, "");
      }
      await loadSettings();
      loadProjects();
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
  $("#set-config").addEventListener("click", () => {
    if (REMOTE) closeSettings(); // a dialog of its own; Settings reads the file again next time
    onMenu("edit-config");
  });
  $("#set-remote").addEventListener("click", () => {
    closeSettings();
    openRemoteDialog();
  });
  dlgEl.addEventListener("keydown", (e) => e.key === "Escape" && closeSettings());
  dlgEl.addEventListener("mousedown", (e) => e.target === dlgEl && closeSettings());
}

wireSettings();

export { openSettings, renderSettings };

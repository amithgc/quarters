// config.json in the editor, for a browser: it can't open the file in an app
// on the computer Quarters runs on, as the Mac app does (OpenConfig). The
// server checks the text before it writes it, so a save never breaks the
// file, and never writes over a change saved since it was opened.
// Loaded the first time it's needed (see ui() in app.js).

import { $, Go, esc, flash, keyText, loadProjects, loadSettings } from "../app.js";
import { extensions, loadCM } from "./fileview.js";

const cf = { view: null, hash: "", saved: "", armed: false, saving: false };
const errText = (e) => String(e).replace(/^Error: /, "");
const dirty = () => cf.view && cf.view.state.doc.toString() !== cf.saved;

export async function openConfig() {
  let r, CM;
  try {
    [r, CM] = await Promise.all([Go().ReadConfig(), loadCM()]);
  } catch (e) {
    return flash(errText(e));
  }
  Object.assign(cf, { hash: r.hash, saved: r.text, armed: false });
  note("");
  $("#cf-path").textContent = await Go().ConfigPath().catch(() => "");
  cf.view?.destroy();
  cf.view = new CM.EditorView({
    parent: $("#cfg-editor"),
    state: CM.EditorState.create({ doc: r.text, extensions: extensions(CM, "config.json", false, save, changed) }),
  });
  status();
  $("#config-dialog").classList.remove("hidden");
  cf.view.focus();
}

function close(force) {
  if (dirty() && !force && !cf.armed) {
    cf.armed = true;
    $("#cf-status").textContent = "Not saved: Esc or Cancel again to throw it away";
    return;
  }
  $("#config-dialog").classList.add("hidden");
  cf.view?.destroy();
  cf.view = null;
}

function changed() {
  cf.armed = false;
  status();
}

function status() {
  const st = $("#cf-status");
  st.textContent = dirty() ? `● not saved · ${keyText("⌘S")}` : "Quarters reads it again when you save";
  st.classList.toggle("accent", dirty());
}

function note(html) {
  $("#cf-note").innerHTML = html;
  $("#cf-note").classList.toggle("hidden", !html);
}

async function save() {
  if (!cf.view || cf.saving) return;
  const text = cf.view.state.doc.toString();
  cf.saving = true;
  try {
    const r = await Go().SaveConfig(text, cf.hash);
    cf.hash = r.hash;
    cf.saved = r.text;
    note("");
    flash("saved config.json");
    loadProjects();
    loadSettings();
    close(true);
  } catch (e) {
    const msg = errText(e);
    note(esc(msg) + (/changed since/.test(msg) ? ' <button type="button" class="ed-reload" data-cf-reload>Open the new version</button> <span class="muted">(your changes are lost)</span>' : ""));
    goToLine(+(msg.match(/^line (\d+):/) || [])[1]);
  } finally {
    cf.saving = false;
  }
}

// goToLine puts the cursor on the line a mistake is on.
function goToLine(n) {
  const doc = cf.view?.state.doc;
  if (!n || !doc || n > doc.lines) return;
  const at = doc.line(n).from;
  cf.view.dispatch({ selection: { anchor: at }, scrollIntoView: true });
  cf.view.focus();
}

function wire() {
  const el = $("#config-dialog");
  $("#cf-cancel").addEventListener("click", () => close());
  $("#cf-save").addEventListener("click", save);
  el.addEventListener("mousedown", (e) => e.target === el && close());
  el.addEventListener("keydown", (e) => {
    // an Esc the editor used (its search box, extra cursors) stays there
    if (e.key === "Escape" && !e.defaultPrevented && !e.target.closest(".cm-panels")) {
      e.preventDefault();
      close();
    }
  });
  $("#cf-note").addEventListener("click", (e) => {
    if (e.target.closest("[data-cf-reload]")) openConfig();
  });
}

wire();

// The text editor: queued prompts, agent instructions (CLAUDE.md…) and notes.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  canChange,
  esc,
  flash,
  isTouch,
  project,
  refreshInstructions,
} from "../app.js";

const ed = { open: false, kind: "", path: "", name: "", tabId: 0, hash: "", start: "", readOnly: false, armed: false };

async function openEditor(opts) {
  Object.assign(ed, { kind: opts.kind, path: opts.path || "", name: opts.name || "", tabId: opts.tabId || 0, hash: "", start: "", readOnly: false, armed: false });
  const box = $("#ed-text"), p = project(ed.path);
  $("#ed-note").classList.add("hidden");
  $("#ed-status").textContent = "";
  box.value = "";
  if (ed.kind === "queue") {
    $("#ed-title").textContent = "QUEUE A PROMPT";
    $("#ed-sub").textContent = p ? p.name : "";
    box.placeholder = `Sent when ${opts.agent} finishes what it's doing (and after the prompts queued before it).`;
    box.classList.remove("mono");
    $("#ed-save").textContent = "Add to queue";
  } else {
    const notes = ed.kind === "notes";
    $("#ed-title").textContent = notes ? "NOTES" : ed.name.toUpperCase();
    $("#ed-sub").textContent = `${p ? p.name : ""}${notes ? " · kept by Quarters" : ""}`;
    box.placeholder = notes ? "Anything you want to remember about this project: commands, links, what's next…" : `Standing instructions for the agent: how to build and test, conventions, what to avoid…`;
    box.classList.add("mono");
    $("#ed-save").textContent = "Save";
    box.disabled = true;
    try {
      const t = notes ? await Go().Notes(ed.path) : await Go().ReadInstructions(ed.path, ed.name);
      box.value = ed.start = t.text || "";
      ed.hash = t.hash;
      if (!t.text && !notes) $("#ed-status").textContent = `${ed.name} isn't there yet: saving creates it`;
    } catch (err) {
      return flash(String(err).replace(/^Error: /, ""));
    } finally {
      box.disabled = false;
    }
    ed.readOnly = !canChange();
    if (ed.readOnly) $("#ed-status").textContent = "read only on this device";
  }
  box.readOnly = ed.readOnly;
  $("#ed-save").classList.toggle("hidden", ed.readOnly);
  ed.open = true;
  $("#edit-dialog").classList.remove("hidden");
  box.focus();
  if (ed.kind !== "queue") box.setSelectionRange(0, 0);
  box.scrollTop = 0;
}

function closeEditor(force) {
  const dirty = !ed.readOnly && $("#ed-text").value !== ed.start;
  if (dirty && !force && !ed.armed) {
    ed.armed = true;
    $("#ed-status").textContent = "Not saved: Esc or Cancel again to throw it away";
    return;
  }
  ed.open = false;
  $("#edit-dialog").classList.add("hidden");
}

async function saveEditor() {
  if (ed.readOnly) return;
  const text = $("#ed-text").value;
  try {
    if (ed.kind === "queue") {
      await Go().QueueAdd(ed.tabId, text);
    } else {
      const t = ed.kind === "notes" ? await Go().SaveNotes(ed.path, text, ed.hash) : await Go().SaveInstructions(ed.path, ed.name, text, ed.hash);
      ed.hash = t.hash;
      ed.start = t.text;
      refreshInstructions(ed.path, true);
      flash(`saved ${ed.kind === "notes" ? "the notes" : ed.name}`);
    }
  } catch (err) {
    const note = $("#ed-note"), msg = String(err).replace(/^Error: /, "");
    note.innerHTML = esc(msg) + (/changed since/.test(msg) ? ' <button type="button" class="ed-reload" data-ed-reload>Open the new version</button> <span class="muted">(your changes are lost)</span>' : "");
    note.classList.remove("hidden");
    return;
  }
  closeEditor(true);
}

function wireEditor() {
  const el = $("#edit-dialog");
  $("#ed-cancel").addEventListener("click", () => closeEditor());
  $("#ed-save").addEventListener("click", saveEditor);
  el.addEventListener("mousedown", (e) => e.target === el && closeEditor());
  el.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      e.preventDefault();
      closeEditor();
    } else if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "Enter")) {
      e.preventDefault();
      saveEditor();
    } else if (e.key === "Enter" && !e.shiftKey && ed.kind === "queue" && !isTouch()) {
      e.preventDefault(); // a prompt: Enter adds it, ⇧Enter is a new line
      saveEditor();
    }
  });
  $("#ed-note").addEventListener("click", (e) => {
    if (e.target.closest("[data-ed-reload]")) openEditor({ kind: ed.kind, path: ed.path, name: ed.name });
  });
  $("#ed-text").addEventListener("input", () => {
    if (ed.armed) {
      ed.armed = false;
      $("#ed-status").textContent = "";
    }
  });
}

wireEditor();

export { openEditor };

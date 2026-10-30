// What changed since a checkpoint (the side panel's CHECKPOINTS section):
// the files on the left, a file's diff on the right, and Restore.
// Loaded the first time it's needed (see ui() in app.js).

import { $, Go, canChange, esc, flash, refreshDev, refreshStats } from "../app.js";
import { diffHTML } from "./diff.js";

const cp = { path: "", id: "", files: [], file: "", armed: false };
const errText = (e) => String(e).replace(/^Error: /, "");
const clock = (ms) => new Date(ms).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" });

export async function openCheckpoint(path, c) {
  Object.assign(cp, { path, id: c.id, files: [], file: "", armed: false });
  $("#cp-when").textContent = `${clock(c.at)} · ${c.label}`;
  $("#cp-files").innerHTML = '<div class="muted">loading…</div>';
  $("#cp-diff").innerHTML = "";
  $("#cp-stat").textContent = "";
  $("#cp-restore").textContent = "Restore these files";
  $("#cp-restore").classList.toggle("hidden", !canChange());
  $("#cp-dialog").classList.remove("hidden");
  $("#cp-close").focus();
  try {
    cp.files = (await Go().CheckpointFiles(path, c.id)) || [];
  } catch (e) {
    $("#cp-files").innerHTML = `<div class="red">${esc(errText(e))}</div>`;
    return;
  }
  const add = cp.files.reduce((n, f) => n + f.added, 0), del = cp.files.reduce((n, f) => n + f.del, 0);
  $("#cp-stat").innerHTML = cp.files.length
    ? `${cp.files.length} file${cp.files.length === 1 ? "" : "s"} changed since · <span class="green">+${add}</span> <span class="red">−${del}</span>`
    : "nothing has changed since";
  renderFiles();
  if (cp.files.length) showFile(cp.files[0].path);
}

function renderFiles() {
  const col = { A: "green", D: "red", M: "yellow" };
  $("#cp-files").innerHTML = cp.files.length
    ? cp.files
        .map((f) => {
          const cut = f.path.lastIndexOf("/");
          return `<button type="button" class="cp-file${f.path === cp.file ? " on" : ""}" data-f="${esc(f.path)}" title="${esc(f.path)}">
            <span class="${col[f.status] || "yellow"} st">${esc(f.status)}</span><span class="grow"><span class="muted">${esc(f.path.slice(0, cut + 1))}</span>${esc(f.path.slice(cut + 1))}</span>
            <span class="green">${f.added ? "+" + f.added : ""}</span><span class="red">${f.del ? "−" + f.del : ""}</span></button>`;
        })
        .join("")
    : '<div class="muted">The files are as they were at the checkpoint.</div>';
}

async function showFile(file) {
  cp.file = file;
  renderFiles();
  $("#cp-diff").innerHTML = '<span class="dl muted">loading…</span>';
  try {
    const d = await Go().CheckpointDiff(cp.path, cp.id, file);
    if (cp.file !== file) return;
    $("#cp-diff").innerHTML = diffHTML(d).html || `<span class="dl muted">${esc(d.note || "No changes.")}</span>`;
  } catch (e) {
    $("#cp-diff").innerHTML = `<span class="dl red">${esc(errText(e))}</span>`;
  }
}

async function restore() {
  if (!cp.armed) {
    cp.armed = true;
    $("#cp-restore").textContent = "Restore? Click again";
    setTimeout(() => {
      cp.armed = false;
      $("#cp-restore").textContent = "Restore these files";
    }, 4000);
    return;
  }
  $("#cp-restore").disabled = true;
  try {
    await Go().RestoreCheckpoint(cp.path, cp.id);
    flash("restored · how things were is a checkpoint too, so this can be undone");
    close();
  } catch (e) {
    $("#cp-stat").textContent = errText(e);
  } finally {
    $("#cp-restore").disabled = false;
    refreshDev(cp.path, true);
    refreshStats();
  }
}

function close() {
  $("#cp-dialog").classList.add("hidden");
}

function wire() {
  const el = $("#cp-dialog");
  $("#cp-files").addEventListener("click", (e) => {
    const b = e.target.closest("[data-f]");
    if (b) showFile(b.dataset.f);
  });
  $("#cp-restore").addEventListener("click", restore);
  $("#cp-close").addEventListener("click", close);
  el.addEventListener("mousedown", (e) => e.target === el && close());
  el.addEventListener("keydown", (e) => {
    if (e.key === "Escape") return close();
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const i = cp.files.findIndex((f) => f.path === cp.file) + (e.key === "ArrowDown" ? 1 : -1);
    if (cp.files[i]) showFile(cp.files[i].path);
    e.preventDefault();
  });
}

wire();

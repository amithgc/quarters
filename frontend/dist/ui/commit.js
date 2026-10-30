// The commit dialog (the side panel's GIT section): the changes, a message
// (✨ asks Claude Code to draft it from the diff), Commit or Commit & push.
// Files that look like they hold secrets are refused by Go, never committed.
// Loaded the first time it's needed (see ui() in app.js).

import { $, Go, esc, flash, refreshStats, state } from "../app.js";

const ci = { path: "", busy: false };
const errText = (e) => String(e).replace(/^Error: /, "");

export function openCommit(path) {
  const g = state.statsPath === path ? state.stats?.git : null;
  ci.path = path;
  $("#ci-branch").textContent = g ? `⎇ ${g.Branch}` : "";
  const files = (g && g.Files) || [];
  $("#ci-files").innerHTML = files.length
    ? files
        .map((f) => {
          const st = f.Status === "??" ? "U" : f.Status;
          const col = f.Status === "A" || f.Status === "??" ? "green" : f.Status === "D" ? "red" : "yellow";
          return `<div class="ci-file"><span class="${col} st">${esc(st)}</span><span class="grow">${esc(f.Path)}</span>${f.Added ? `<span class="green">+${f.Added}</span>` : ""}${f.Del ? `<span class="red">−${f.Del}</span>` : ""}</div>`;
        })
        .join("")
    : '<div class="muted">No changes.</div>';
  note("");
  $("#ci-status").textContent = `${files.length} file${files.length === 1 ? "" : "s"}, everything git doesn't ignore`;
  $("#commit-dialog").classList.remove("hidden");
  $("#ci-msg").focus();
}

function note(text) {
  $("#ci-note").textContent = text;
  $("#ci-note").classList.toggle("hidden", !text);
}

function busy(on, what) {
  ci.busy = on;
  for (const b of ["#ci-draft", "#ci-commit", "#ci-push"]) $(b).disabled = on;
  if (what) $("#ci-status").textContent = what;
}

async function draft() {
  if (ci.busy) return;
  busy(true, "✨ Claude is reading the diff…");
  $("#ci-draft").classList.add("working");
  try {
    $("#ci-msg").value = await Go().DraftCommitMessage(ci.path);
    $("#ci-status").textContent = "✓ drafted";
    note("");
    $("#ci-msg").focus();
  } catch (e) {
    note(errText(e));
    $("#ci-status").textContent = "";
  } finally {
    $("#ci-draft").classList.remove("working");
    busy(false);
  }
}

async function commit(push) {
  if (ci.busy) return;
  const msg = $("#ci-msg").value.trim();
  if (!msg) {
    note("Write a message first (or ✨ draft one).");
    return $("#ci-msg").focus();
  }
  busy(true, push ? "committing and pushing…" : "committing…");
  try {
    const done = await Go().GitCommit(ci.path, msg, push);
    flash(done);
    $("#ci-msg").value = "";
    close();
  } catch (e) {
    note(errText(e));
    $("#ci-status").textContent = "";
  } finally {
    busy(false);
    refreshStats();
  }
}

function close() {
  $("#commit-dialog").classList.add("hidden");
}

function wire() {
  const el = $("#commit-dialog");
  $("#ci-draft").addEventListener("click", draft);
  $("#ci-commit").addEventListener("click", () => commit(false));
  $("#ci-push").addEventListener("click", () => commit(true));
  $("#ci-cancel").addEventListener("click", close);
  el.addEventListener("mousedown", (e) => e.target === el && close());
  el.addEventListener("keydown", (e) => {
    if (e.key === "Escape") close();
    else if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      commit(e.shiftKey); // ⌘↵ commits, ⇧⌘↵ pushes too
    }
  });
}

wire();

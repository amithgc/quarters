// A file's changes since the last commit (click a file in the side panel).
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  esc,
} from "../app.js";

async function showDiff(path, file) {
  const el = $("#diff-dialog");
  el.classList.remove("hidden");
  $("#df-file").textContent = file;
  $("#df-stat").textContent = "";
  $("#df-note").textContent = "loading…";
  $("#df-body").innerHTML = "";
  $("#df-close").focus();
  let d;
  try {
    d = await Go().Diff(path, file);
  } catch (e) {
    $("#df-note").textContent = String(e).replace(/^Error: /, "");
    return;
  }
  $("#df-title").textContent = d.file.split("/").pop().toUpperCase();
  $("#df-note").textContent = d.note || "";
  $("#df-note").classList.toggle("hidden", !d.note);
  const { html, add, del } = diffHTML(d);
  $("#df-body").innerHTML = html;
  $("#df-stat").innerHTML = d.diff ? `<span class="green">+${add}</span> <span class="red">−${del}</span>` : "";
}

// diffHTML draws a file's diff (see FileDiff in Go) as coloured lines, and
// counts them. The checkpoint view uses it too.
export function diffHTML(d) {
  let add = 0, del = 0;
  if (!d.diff) return { html: "", add, del };
  const html = d.diff
    .replace(/\n$/, "")
    .split("\n")
    .map((l) => {
      if (d.added) {
        add++;
        return `<span class="dl add">+${esc(l)}</span>`;
      }
      if (/^(diff --git|index |--- |\+\+\+ |new file mode|deleted file mode|similarity|rename )/.test(l)) return "";
      if (l.startsWith("@@")) return `<span class="dl hunk">${esc(l)}</span>`;
      if (l.startsWith("+")) return add++, `<span class="dl add">${esc(l)}</span>`;
      if (l.startsWith("-")) return del++, `<span class="dl del">${esc(l)}</span>`;
      return `<span class="dl">${esc(l)}</span>`;
    })
    .filter(Boolean)
    .join("");
  return { html, add, del };
}

function closeDiff() {
  $("#diff-dialog").classList.add("hidden");
}

function wireDiff() {
  const el = $("#diff-dialog");
  $("#df-close").addEventListener("click", closeDiff);
  el.addEventListener("mousedown", (e) => e.target === el && closeDiff());
  el.addEventListener("keydown", (e) => e.key === "Escape" && closeDiff());
}

wireDiff();

export { showDiff };

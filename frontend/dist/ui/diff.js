// A file's changes since the last commit (click a file in the side panel),
// and the review: every changed file, one after the other, before committing.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  canChange,
  esc,
  flash,
  openFileAt,
  ui,
} from "../app.js";

const REVIEW_MAX = 500; // files listed: past that, git is the better tool
let review = null; // { path, files, at } while reviewing
let shown = 0; // the latest file asked for: a slower answer for an earlier one is dropped

async function showDiff(path, file) {
  setReview(null);
  await loadDiff(path, file);
}

// showReview opens the review of a project's changes.
export async function showReview(path) {
  let files;
  try {
    files = (await Go().ChangedFiles(path)) || [];
  } catch (e) {
    return flash(String(e).replace(/^Error: /, ""));
  }
  if (!files.length) return flash("nothing to review: no changes since the last commit");
  setReview({ path, files, at: 0 });
  $("#df-title").textContent = "REVIEW";
  pick(0);
}

function setReview(r) {
  review = r;
  const box = $("#diff-dialog .diff-box");
  box.classList.toggle("reviewing", !!r);
  $("#df-open").classList.toggle("hidden", !r);
  $("#df-commit").classList.toggle("hidden", !r || !canChange());
  $("#df-files").innerHTML = r ? railHTML(r.files) : "";
}

function railHTML(files) {
  const rows = files.slice(0, REVIEW_MAX).map((f, i) => {
    const col = f.Status === "A" || f.Status === "??" ? "green" : f.Status === "D" ? "red" : "yellow";
    const cut = f.Path.lastIndexOf("/");
    const count = `${f.Added ? `<span class="green">+${f.Added}</span>` : ""}${f.Del ? ` <span class="red">−${f.Del}</span>` : ""}`;
    return `<button type="button" class="rv-file" data-i="${i}" title="${esc(f.Path)}"><span class="${col} st">${esc(f.Status === "??" ? "U" : f.Status)}</span><span class="rv-name"><span class="muted">${esc(f.Path.slice(0, cut + 1))}</span>${esc(f.Path.slice(cut + 1))}</span><span class="rv-count">${count}</span></button>`;
  });
  const more = files.length > REVIEW_MAX ? `<div class="muted rv-more">+ ${files.length - REVIEW_MAX} more</div>` : "";
  return rows.join("") + more;
}

function pick(i) {
  const n = Math.min(review.files.length, REVIEW_MAX);
  review.at = Math.max(0, Math.min(n - 1, i));
  document.querySelectorAll("#df-files .rv-file").forEach((b, j) => {
    b.classList.toggle("on", j === review.at);
    if (j === review.at) b.scrollIntoView({ block: "nearest" });
  });
  const f = review.files[review.at];
  $("#df-open").disabled = f.Status === "D";
  loadDiff(review.path, f.Path);
}

async function loadDiff(path, file) {
  const ask = ++shown;
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
    if (ask === shown) $("#df-note").textContent = String(e).replace(/^Error: /, "");
    return;
  }
  if (ask !== shown) return;
  $("#df-title").textContent = review ? `REVIEW · ${review.at + 1} OF ${review.files.length}` : d.file.split("/").pop().toUpperCase();
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
  shown++;
  $("#diff-dialog").classList.add("hidden");
  setReview(null);
}

function wireDiff() {
  const el = $("#diff-dialog");
  $("#df-close").addEventListener("click", closeDiff);
  el.addEventListener("mousedown", (e) => e.target === el && closeDiff());
  el.addEventListener("keydown", (e) => {
    if (e.key === "Escape") return closeDiff();
    if (!review || e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === "j" || e.key === "k") {
      e.preventDefault();
      pick(review.at + (e.key === "j" ? 1 : -1));
    }
  });
  $("#df-files").addEventListener("click", (e) => {
    const b = e.target.closest(".rv-file");
    if (b && review) pick(+b.dataset.i);
  });
  $("#df-open").addEventListener("click", () => {
    if (!review) return;
    const { path, files, at } = review;
    closeDiff();
    openFileAt(path, path, files[at].Path);
  });
  $("#df-commit").addEventListener("click", () => {
    if (!review) return;
    const { path } = review;
    closeDiff();
    ui("commit.js").then((m) => m.openCommit(path));
  });
}

wireDiff();

export { showDiff };

// Editor tabs: a project's file in CodeMirror (vendor/codemirror.js, loaded
// with the first file). ⌘S saves; a save never overwrites a change made on
// disk since (an agent's edit): you choose to reload or keep yours. A file
// with no unsaved edits follows the disk by itself.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  canChange,
  esc,
  fileTabsOf,
  flash,
  newKey,
  renderTabs,
  setFocus,
  showFileTab,
  showImageView,
  shownFile,
  state,
} from "../app.js";

let cmLoad = null;
export const loadCM = () => (cmLoad ||= import("../vendor/codemirror.js").catch((e) => {
  cmLoad = null;
  throw e;
}));
const errText = (e) => String(e).replace(/^Error: /, "");
const CHECK_MS = 2500;

// openFile opens rel (in dir: the project or one of its worktrees) in an
// editor tab of project path, or shows the tab it's already in.
export async function openFile(path, dir, rel) {
  const have = fileTabsOf(path).find((f) => f.dir === dir && f.rel === rel);
  if (have) return showFileTab(have);
  const name = rel.split("/").pop();
  let r, CM;
  try {
    [r, CM] = await Promise.all([Go().ReadFile(dir, rel), loadCM()]);
  } catch (e) {
    return flash(errText(e));
  }
  if (r.image) return showImageView(r.image, name);
  if (r.note) return flash(`${name}: ${r.note}`);
  const again = fileTabsOf(path).find((f) => f.dir === dir && f.rel === rel); // opened twice quickly
  if (again) return showFileTab(again);

  const el = document.createElement("div");
  el.className = "file-pane hidden";
  el.innerHTML = `<div class="fp-bar"><span class="fp-path"></span><span class="fp-state muted"></span><button class="btn fp-save hidden" type="button">Save</button></div>
    <div class="fp-changed hidden"><span class="grow"></span><button class="btn" type="button" data-fp="reload">Reload</button><button class="btn" type="button" data-fp="keep">Keep mine</button></div>
    <div class="fp-editor"></div>`;
  $("#file-view").appendChild(el);
  const f = { key: newKey(), project: path, dir, rel, name, el, hash: r.hash, stamp: r.stamp, saved: r.text, dirty: false, view: null, readOnly: !canChange(), conflict: false, gone: false };
  el.querySelector(".fp-path").innerHTML = rel.split("/").map((p, i, all) => (i === all.length - 1 ? `<b>${esc(p)}</b>` : `${esc(p)}<span class="muted"> / </span>`)).join("");
  const ext = extensions(CM, f.name, f.readOnly, () => saveFile(f), () => changed(f));
  f.view = new CM.EditorView({ parent: el.querySelector(".fp-editor"), state: CM.EditorState.create({ doc: r.text, extensions: ext }) });
  f.view.contentDOM.addEventListener("focus", () => setFocus("terminal"));
  el.querySelector(".fp-save").addEventListener("click", () => saveFile(f));
  el.querySelector(".fp-changed").addEventListener("click", (e) => {
    const act = e.target.closest("[data-fp]")?.dataset.fp;
    if (act === "reload") reload(f);
    if (act === "keep") keepMine(f);
  });
  (state.fileTabs[path] ||= []).push(f);
  showStatus(f);
  showFileTab(f);
}

// extensions sets up an editor for a file called name: save runs on ⌘S,
// changed after every edit. The config.json editor uses it too.
export function extensions(CM, name, readOnly, save, changed) {
  const lang = CM.languageFor(name);
  const wrap = /\.(md|markdown|mdx|txt|rst)$/i.test(name) || !name.includes(".");
  return [
    CM.lineNumbers(),
    CM.highlightActiveLineGutter(),
    CM.highlightSpecialChars(),
    CM.history(),
    CM.foldGutter({ openText: "▾", closedText: "▸" }),
    CM.drawSelection(),
    CM.EditorState.allowMultipleSelections.of(true),
    CM.indentOnInput(),
    CM.bracketMatching(),
    CM.closeBrackets(),
    CM.rectangularSelection(),
    CM.crosshairCursor(),
    CM.highlightActiveLine(),
    CM.highlightSelectionMatches(),
    CM.syntaxHighlighting(highlightStyle(CM)),
    CM.keymap.of([
      { key: "Mod-s", run: () => (save(), true), preventDefault: true },
      ...CM.closeBracketsKeymap,
      ...CM.defaultKeymap,
      ...CM.searchKeymap,
      ...CM.historyKeymap,
      ...CM.foldKeymap,
      CM.indentWithTab,
    ]),
    lang || [],
    wrap ? CM.EditorView.lineWrapping : [],
    CM.EditorState.readOnly.of(readOnly),
    CM.EditorView.contentAttributes.of({ spellcheck: "false", autocorrect: "off", autocapitalize: "off" }),
    CM.EditorView.updateListener.of((u) => u.docChanged && changed()),
  ];
}

// colours from the app's theme, so light and dark follow it
let style = null;
function highlightStyle(CM) {
  const t = CM.tags;
  return (style ||= CM.HighlightStyle.define([
    { tag: [t.keyword, t.modifier, t.controlKeyword, t.operatorKeyword, t.definitionKeyword, t.moduleKeyword], color: "var(--violet)" },
    { tag: [t.string, t.special(t.string), t.regexp, t.character], color: "var(--green)" },
    { tag: [t.number, t.bool, t.null, t.atom, t.unit], color: "var(--yellow)" },
    { tag: [t.comment, t.lineComment, t.blockComment, t.docComment, t.meta], color: "var(--muted)", fontStyle: "italic" },
    { tag: [t.function(t.variableName), t.function(t.propertyName), t.definition(t.function(t.variableName))], color: "var(--accent)" },
    { tag: [t.typeName, t.className, t.namespace, t.standard(t.typeName)], color: "var(--yellow)" },
    { tag: [t.propertyName, t.attributeName, t.labelName], color: "var(--soft)" },
    { tag: [t.tagName, t.angleBracket], color: "var(--accent)" },
    { tag: [t.heading, t.heading1, t.heading2, t.heading3], color: "var(--accent)", fontWeight: "700" },
    { tag: [t.strong], fontWeight: "700" },
    { tag: [t.emphasis], fontStyle: "italic" },
    { tag: [t.link, t.url], color: "var(--violet)", textDecoration: "underline" },
    { tag: [t.quote, t.monospace], color: "var(--soft)" },
    { tag: [t.invalid, t.deleted], color: "var(--red)" },
    { tag: [t.inserted], color: "var(--green)" },
    { tag: [t.processingInstruction, t.escape, t.self], color: "var(--violet)" },
  ]));
}

function changed(f) {
  const doc = f.view.state.doc;
  const dirty = doc.length !== f.saved.length || doc.toString() !== f.saved;
  if (dirty === f.dirty) return;
  f.dirty = dirty;
  showStatus(f);
  renderTabs();
}

function showStatus(f) {
  const st = f.el.querySelector(".fp-state");
  st.textContent = f.gone ? "gone from the disk" : f.readOnly ? "read only on this device" : f.dirty ? "● not saved · ⌘S" : "saved";
  st.classList.toggle("accent", f.dirty && !f.gone);
  st.classList.toggle("red", f.gone);
  f.el.querySelector(".fp-save").classList.toggle("hidden", !f.dirty || f.readOnly);
}

export async function saveFile(f) {
  if (!f || f.readOnly || f.saving) return;
  if (f.conflict) return flash(`${f.name} changed on the disk: reload it, or keep yours`);
  const text = f.view.state.doc.toString();
  f.saving = true;
  try {
    const r = await Go().SaveFile(f.dir, f.rel, text, f.hash);
    f.hash = r.hash;
    f.saved = r.text;
    f.gone = false;
    if (r.text !== text && r.text.startsWith(text)) {
      // Quarters ends a file with a line break
      f.view.dispatch({ changes: { from: f.view.state.doc.length, insert: r.text.slice(text.length) } });
    }
    f.stamp = await Go().FileStamp(f.dir, f.rel).catch(() => "");
    changed(f);
    showStatus(f);
  } catch (e) {
    const msg = errText(e);
    if (/changed since/.test(msg)) await checkDisk(f, true);
    else flash(msg);
  } finally {
    f.saving = false;
  }
}

// checkDisk looks for a change on the disk: taken in when there are no
// unsaved edits, otherwise asked about.
export async function checkDisk(f, now) {
  if (!f || f.checking || (!now && Date.now() - (f.checked || 0) < 800)) return;
  f.checking = true;
  f.checked = Date.now();
  try {
    let s = "";
    try {
      s = await Go().FileStamp(f.dir, f.rel);
    } catch {
      if (!f.gone) {
        f.gone = true;
        showStatus(f);
      }
      return;
    }
    if (f.gone) {
      f.gone = false;
      showStatus(f);
    }
    if (s === f.stamp) return;
    const r = await Go().ReadFile(f.dir, f.rel);
    if (r.note || r.image) return;
    f.stamp = r.stamp;
    if (r.hash === f.hash) return;
    if (!f.dirty) return take(f, r);
    f.conflict = r;
    const bar = f.el.querySelector(".fp-changed");
    bar.querySelector(".grow").textContent = `${f.name} changed on the disk (an agent may have edited it), and you have unsaved changes.`;
    bar.classList.remove("hidden");
  } catch {
    /* try again next time */
  } finally {
    f.checking = false;
  }
}

// take puts the disk's text in the editor, touching only what changed, so
// the cursor and the scroll stay where they were.
function take(f, r) {
  const old = f.view.state.doc.toString(), text = r.text;
  let a = 0;
  while (a < old.length && a < text.length && old[a] === text[a]) a++;
  let b = 0;
  while (b < old.length - a && b < text.length - a && old[old.length - 1 - b] === text[text.length - 1 - b]) b++;
  f.hash = r.hash;
  f.saved = text;
  f.conflict = false;
  f.el.querySelector(".fp-changed").classList.add("hidden");
  if (old !== text) f.view.dispatch({ changes: { from: a, to: old.length - b, insert: text.slice(a, text.length - b) } });
  changed(f);
  showStatus(f);
}

function reload(f) {
  if (f.conflict) take(f, f.conflict);
}

// keepMine: the next save replaces what's on the disk.
function keepMine(f) {
  if (!f.conflict) return;
  f.hash = f.conflict.hash;
  f.saved = f.conflict.text;
  f.conflict = false;
  f.el.querySelector(".fp-changed").classList.add("hidden");
  changed(f);
  showStatus(f);
  f.view.focus();
}

// the file in view follows the disk while the window is in use
setInterval(() => {
  const f = shownFile();
  if (f && document.visibilityState === "visible" && document.hasFocus()) checkDisk(f);
}, CHECK_MS);
window.addEventListener("focus", () => checkDisk(shownFile(), true));

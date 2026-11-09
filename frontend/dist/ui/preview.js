// Preview tabs: a local dev server in a tab of its project, next to the
// agent working on it. They sit with the file tabs (see fileTabsOf in app.js)
// and, like them, belong to this window.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  fileTabsOf,
  flash,
  newKey,
  openLink,
  renderTabs,
  showFileTab,
  state,
} from "../app.js";
import { previewName, previewURL } from "../localurl.js";

// The page keeps its own origin (a dev server's scripts and storage need it),
// which is safe because it's never Quarters' (see previewURL). It can't
// navigate this window, and asks the browser for no other powers.
const SANDBOX = "allow-scripts allow-same-origin allow-forms allow-modals allow-popups allow-downloads";

const originOf = (url) => {
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
};

// openPreview shows url in a preview tab of project path: the tab already on
// that server if there is one, else a new one. Without a url, the tab opens
// with the address bar ready for one.
export function openPreview(path, url = "") {
  const have = fileTabsOf(path).find((f) => f.preview && url && originOf(f.rel) === originOf(url));
  if (have) {
    go(have, url);
    return showFileTab(have);
  }
  const el = document.createElement("div");
  el.className = "file-pane preview-pane hidden";
  el.innerHTML = `<div class="fp-bar pv-bar">
      <button class="pt-btn" type="button" data-pv="reload" title="Reload">↻</button>
      <form class="pv-form"><input class="pv-url" type="text" spellcheck="false" autocomplete="off" autocapitalize="off" placeholder="A local server: 5173 or localhost:3000/docs" aria-label="Address"></form>
      <button class="pt-btn" type="button" data-pv="open" title="Open in your browser: use this if the page stays blank (some servers refuse to be shown in a frame)">open in browser</button>
    </div>
    <div class="pv-empty muted">Type the address of a server running on this machine.</div>
    <iframe class="pv-frame hidden" title="Preview" referrerpolicy="no-referrer" sandbox="${SANDBOX}"></iframe>`;
  $("#file-view").appendChild(el);
  const frame = el.querySelector("iframe"), input = el.querySelector(".pv-url");
  const f = {
    key: newKey(),
    project: path,
    dir: path,
    rel: "",
    name: "preview",
    el,
    preview: true,
    dirty: false,
    readOnly: true,
    // what the file tab code asks of an editor
    view: {
      requestMeasure() {},
      focus() {
        if (!f.rel) input.focus();
      },
      destroy() {
        frame.src = "about:blank";
      },
    },
  };
  el.querySelector(".pv-bar").addEventListener("click", (e) => {
    const act = e.target.closest("[data-pv]")?.dataset.pv;
    if (act === "reload" && f.rel) go(f, f.rel);
    if (act === "open" && f.rel) openLink(f.rel);
  });
  el.querySelector(".pv-form").addEventListener("submit", (e) => {
    e.preventDefault();
    const next = previewURL(input.value, location.origin);
    if (!next) return flash("a preview shows a server on this machine, like localhost:5173");
    go(f, next);
  });
  (state.fileTabs[path] ||= []).push(f);
  if (url) go(f, url);
  showFileTab(f);
  return f;
}

function go(f, url) {
  const frame = f.el.querySelector("iframe");
  f.rel = url;
  f.name = previewName(url);
  f.el.querySelector(".pv-url").value = url;
  f.el.querySelector(".pv-empty").classList.add("hidden");
  frame.classList.remove("hidden");
  // setting the same address again doesn't always reload
  if (frame.getAttribute("src") === url) frame.setAttribute("src", "about:blank");
  requestAnimationFrame(() => frame.setAttribute("src", url));
  renderTabs();
}

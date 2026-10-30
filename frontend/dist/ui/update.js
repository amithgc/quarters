// How to update Quarters: the dialog behind the pill beside the version, red
// "update available" or grey "up to date" (see checkUpdate in app.js). The
// steps depend on where Quarters runs: the Mac app (used here or from a
// browser), or the server alone on Linux.
// Loaded the first time it's needed (see ui() in app.js).

import { $, REMOTE, esc, flash, openLink } from "../app.js";

let release = "";

const cmd = (text) => `<div class="up-cmd"><code>${esc(text)}</code><button type="button" class="btn" data-copy="${esc(text)}">Copy</button></div>`;

function steps(u) {
  if (u.server) {
    // releases carry Linux servers only; elsewhere it's built from source
    const byHand = u.os === "linux"
      ? `Download <code>${esc(`quarters-linux-${u.arch}.tar.gz`)}</code> from the release page, unpack it and put <code>quarters</code> where the old one is.`
      : "Build it again from the source (<code>git pull && go build -tags server</code>) and put it where the old one is.";
    return `
      <div class="up-step"><b>Homebrew</b>${cmd("brew upgrade amithgc/tap/quarters-server")}</div>
      <div class="up-step"><b>Or by hand</b><span class="up-note muted">${byHand}</span></div>
      <div class="up-note muted">Then restart the server (<code>systemctl --user restart quarters</code> if it runs as a service). Your sessions keep running in the old one until it stops; this page reconnects by itself.</div>`;
  }
  return `
    <div class="up-step"><b>Homebrew</b>${cmd("brew upgrade --cask quarters")}</div>
    <div class="up-step"><b>Or by hand</b><span class="up-note muted">Download the zip from the release page and drag Quarters into Applications.</span></div>
    <div class="up-note muted">${REMOTE ? "Run it on the Mac, then quit Quarters there and open it again. This page reconnects by itself." : "Quit Quarters first (running sessions end), update, then open it again."}</div>`;
}

export function openUpdate(u) {
  release = u.url;
  const have = $("#version").textContent.replace(/^v/, "");
  $("#up-title").textContent = u.version ? "UPDATE AVAILABLE" : "UP TO DATE";
  $("#up-versions").textContent = u.version ? `${have} → ${u.version}` : have;
  const where = u.server ? `the server on this ${u.os === "linux" ? "Linux machine" : "machine"}` : REMOTE ? "the Mac this browser uses" : "this Mac";
  const lead = u.version
    ? `Quarters <b>${esc(u.version)}</b> is out; ${esc(where)} runs ${esc(have)}.`
    : `${esc(where[0].toUpperCase() + where.slice(1))} runs <b>${esc(have)}</b>, the newest Quarters. When a new one is out the pill turns red; this is how to update then.`;
  $("#up-body").innerHTML = `<div class="up-lead">${lead}</div>${steps(u)}`;
  $("#update-dialog").classList.remove("hidden");
  $("#up-close").focus();
}

export function closeUpdate() {
  $("#update-dialog").classList.add("hidden");
}

function wire() {
  const el = $("#update-dialog");
  $("#up-close").addEventListener("click", closeUpdate);
  // the release notes open here: a browser's own tab, not one on the Mac
  $("#up-notes").addEventListener("click", () => release && (REMOTE ? window.open(release, "_blank", "noopener") : openLink(release)));
  el.addEventListener("mousedown", (e) => e.target === el && closeUpdate());
  el.addEventListener("keydown", (e) => e.key === "Escape" && closeUpdate());
  el.addEventListener("click", (e) => {
    const b = e.target.closest("[data-copy]");
    if (b) navigator.clipboard?.writeText(b.dataset.copy).then(() => flash("copied"), () => flash("couldn't copy"));
  });
}

wire();

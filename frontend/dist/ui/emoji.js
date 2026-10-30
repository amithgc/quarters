// The emoji picker for a project's icon: every emoji, by group, with a
// search by name. The list (vendor/emoji.json, from unicode-emoji-json) is
// fetched the first time it opens.
// Loaded the first time it's needed (see ui() in app.js).

import { $, esc, flash } from "../app.js";

let groups = null; // [{ name, emoji: [[emoji, name]…] }]
let picked = null; // called with the emoji chosen
let current = "";

async function load() {
  if (groups) return groups;
  const r = await fetch(new URL("../vendor/emoji.json", import.meta.url));
  if (!r.ok) throw new Error(`couldn't load the emoji (${r.status})`);
  return (groups = await r.json());
}

// openEmoji shows the picker; onPick gets the emoji chosen.
export async function openEmoji(now, onPick) {
  try {
    await load();
  } catch (e) {
    return flash(String(e.message || e));
  }
  picked = onPick;
  current = now || "";
  $("#em-search").value = "";
  $("#em-tabs").innerHTML = groups.map((g, i) => `<button type="button" data-g="${i}" title="${esc(g.name)}">${g.emoji[0][0]}</button>`).join("");
  render("");
  $("#emoji-dialog").classList.remove("hidden");
  $("#em-search").focus();
}

function render(q) {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const match = ([, name]) => words.every((w) => name.includes(w));
  const cell = ([e, name]) => `<button type="button" class="${e === current ? "on" : ""}" data-e="${e}" title="${esc(name)}">${e}</button>`;
  let html = "";
  groups.forEach((g, i) => {
    const list = words.length ? g.emoji.filter(match) : g.emoji;
    if (list.length) html += `<div class="em-group" id="em-g${i}"><div class="em-head">${esc(g.name)}</div><div class="em-grid">${list.map(cell).join("")}</div></div>`;
  });
  $("#em-list").innerHTML = html || `<div class="em-none muted">No emoji called “${esc(q)}”.</div>`;
  $("#em-list").scrollTop = 0;
  $("#em-tabs").classList.toggle("hidden", words.length > 0);
  $("#em-name").textContent = "";
}

function pick(e) {
  close();
  picked?.(e);
}

function close() {
  $("#emoji-dialog").classList.add("hidden");
}

function wire() {
  const el = $("#emoji-dialog");
  $("#em-search").addEventListener("input", (e) => render(e.target.value.trim()));
  $("#em-search").addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      const first = $("#em-list [data-e]");
      if (first) pick(first.dataset.e);
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      $("#em-list [data-e]")?.focus();
    }
  });
  $("#em-list").addEventListener("click", (e) => {
    const b = e.target.closest("[data-e]");
    if (b) pick(b.dataset.e);
  });
  // the name of the one under the pointer (or focused), at the bottom
  const show = (e) => {
    const b = e.target.closest?.("[data-e]");
    if (b) $("#em-name").textContent = `${b.dataset.e}  ${b.title}`;
  };
  $("#em-list").addEventListener("mouseover", show);
  $("#em-list").addEventListener("focusin", show);
  $("#em-tabs").addEventListener("click", (e) => {
    const b = e.target.closest("[data-g]");
    if (b) $(`#em-g${b.dataset.g}`)?.scrollIntoView({ block: "start" });
  });
  $("#em-cancel").addEventListener("click", close);
  el.addEventListener("mousedown", (e) => e.target === el && close());
  el.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    e.preventDefault();
    e.stopPropagation(); // the project dialog under it stays open
    close();
  });
}

wire();

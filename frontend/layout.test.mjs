// node --test frontend/*.test.mjs (make test)
import { test } from "node:test";
import assert from "node:assert/strict";
import { layoutFor, screenClass } from "./dist/layout.js";

const claude = { agent: true, reader: true };
const codex = { agent: true, reader: false };
const shell = { agent: false, reader: false };
const plain = { transcript: "terminal", card: false, toolsIn: "tabs" };

test("screen classes", () => {
  assert.equal(screenClass({ narrow: true, touch: true }), "phone");
  assert.equal(screenClass({ narrow: true, touch: false }), "phone"); // a narrow desktop window
  assert.equal(screenClass({ narrow: false, touch: true }), "tablet");
  assert.equal(screenClass({ narrow: false, touch: false }), "desktop");
});

test("every agent on a phone gets the message card and its tools", () => {
  for (const tab of [claude, codex]) {
    const l = layoutFor(tab, "phone");
    assert.equal(l.card, true);
    assert.equal(l.toolsIn, "card");
  }
});

test("a chat where the agent's log can be read, else its terminal", () => {
  assert.equal(layoutFor(claude, "phone").transcript, "chat");
  assert.equal(layoutFor(claude, "phone", "chat").transcript, "chat");
  assert.equal(layoutFor(claude, "phone", "terminal").transcript, "terminal");
  assert.equal(layoutFor(codex, "phone").transcript, "terminal");
  assert.equal(layoutFor(codex, "phone", "chat").transcript, "terminal"); // no reader: no chat
});

test("the card stays for every agent whichever transcript is shown", () => {
  assert.equal(layoutFor(claude, "phone", "terminal").card, true);
  assert.equal(layoutFor(codex, "phone").card, true);
});

test("shells, tablets, the Mac and no tab: a plain terminal", () => {
  assert.deepEqual(layoutFor(shell, "phone"), plain);
  assert.deepEqual(layoutFor(claude, "tablet"), plain);
  assert.deepEqual(layoutFor(codex, "tablet"), plain);
  assert.deepEqual(layoutFor(claude, "desktop"), plain);
  assert.deepEqual(layoutFor(null, "phone"), plain);
});

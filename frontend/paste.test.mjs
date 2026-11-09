// node --test frontend/*.test.mjs (make test)
import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_IMAGE_BYTES, fitsAsIs, imageIn, imageInItems, scaled, shrink } from "./dist/paste.js";

const file = (type) => ({ kind: "file", type, getAsFile: () => ({ type }) });
const data = (text, ...items) => ({ getData: () => text, items });

test("a copied image is what's pasted", () => {
  assert.equal(imageIn(data("", file("image/png"))).type, "image/png");
  assert.equal(imageIn(data("", { kind: "string", type: "text/html" }, file("image/jpeg"))).type, "image/jpeg");
});

test("text wins over the picture of it a spreadsheet also copies", () => {
  assert.equal(imageIn(data("a\tb", file("image/png"))), null);
});

test("no image: left to the terminal", () => {
  assert.equal(imageIn(data("")), null);
  assert.equal(imageIn(data("", file("application/pdf"))), null);
  assert.equal(imageIn(null), null);
});

test("clipboard.read(): the same rules", async () => {
  const blob = { type: "image/png" };
  const item = (...types) => ({ types, getType: async () => blob });
  assert.equal(await imageInItems([item("text/html", "image/png")]), blob);
  assert.equal(await imageInItems([item("text/plain", "image/png")]), null);
  assert.equal(await imageInItems([item("text/plain")]), null);
  assert.equal(await imageInItems([]), null);
});

test("what can be sent as it is", () => {
  assert.equal(fitsAsIs({ size: 1000, type: "image/png" }), true);
  assert.equal(fitsAsIs({ size: MAX_IMAGE_BYTES + 1, type: "image/png" }), false);
  assert.equal(fitsAsIs({ size: 1000, type: "image/tiff" }), false);
});

test("scaling keeps the shape and never enlarges", () => {
  assert.deepEqual(scaled(4000, 2000, 1000), { width: 1000, height: 500 });
  assert.deepEqual(scaled(100, 50, 1000), { width: 100, height: 50 });
  assert.deepEqual(scaled(800, 600, Infinity), { width: 800, height: 600 });
  assert.deepEqual(scaled(10000, 1, 100), { width: 100, height: 1 });
});

test("shrinking stops at the first size that fits", async () => {
  const asked = [];
  const out = await shrink(async (side, q) => {
    asked.push([side, q]);
    return { size: side > 2000 ? MAX_IMAGE_BYTES * 2 : 100, side };
  });
  assert.equal(out.side, 2000);
  assert.deepEqual(asked[0], [Infinity, undefined]); // the original pixels first
  assert.equal(asked.length, 4);
});

test("an image that never fits is an error, not a hang", async () => {
  await assert.rejects(shrink(async () => ({ size: MAX_IMAGE_BYTES * 2 })), /too big/);
});

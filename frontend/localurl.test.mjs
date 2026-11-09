// node --test frontend/*.test.mjs (make test)
import { test } from "node:test";
import assert from "node:assert/strict";
import { previewName, previewReachable, previewURL } from "./dist/localurl.js";

test("the ways of typing a local server", () => {
  assert.equal(previewURL("5173"), "http://localhost:5173/");
  assert.equal(previewURL(":5173"), "http://localhost:5173/");
  assert.equal(previewURL("5173/docs"), "http://localhost:5173/docs");
  assert.equal(previewURL("localhost:3000/a?b=1#c"), "http://localhost:3000/a?b=1#c");
  assert.equal(previewURL("  http://127.0.0.1:8000/  "), "http://127.0.0.1:8000/");
  assert.equal(previewURL("https://localhost:8443"), "https://localhost:8443/");
});

test("addresses servers announce become one a frame may load", () => {
  assert.equal(previewURL("http://0.0.0.0:3000/x"), "http://localhost:3000/x");
  assert.equal(previewURL("http://[::1]:3000/"), "http://localhost:3000/");
});

test("only local servers", () => {
  for (const bad of [
    "", "   ", null, "example.com", "https://example.com", "http://192.168.1.20:3000",
    "http://localhost.example.com", "http://localhost@example.com/", "http://user:pw@localhost:3000",
    "javascript:alert(1)", "file:///etc/passwd", "file://localhost/etc/passwd", "data:text/html,x", "about:blank", "ftp://localhost/",
  ]) assert.equal(previewURL(bad), null, String(bad));
});

test("Quarters never shows itself", () => {
  assert.equal(previewURL("localhost:8421", "http://localhost:8421"), null);
  assert.equal(previewURL("http://localhost:8421/x", "http://localhost:8421"), null);
  assert.equal(previewURL("localhost:8422", "http://localhost:8421"), "http://localhost:8422/");
});

test("reachable from the Mac app and a browser on the same machine", () => {
  assert.equal(previewReachable(false, "wails"), true);
  assert.equal(previewReachable(true, "localhost"), true);
  assert.equal(previewReachable(true, "127.0.0.1"), true);
  assert.equal(previewReachable(true, "quarters.example.com"), false);
  assert.equal(previewReachable(true, "192.168.1.20"), false);
});

test("tab names", () => {
  assert.equal(previewName("http://localhost:5173/a/b"), "localhost:5173");
  assert.equal(previewName("nonsense"), "preview");
});

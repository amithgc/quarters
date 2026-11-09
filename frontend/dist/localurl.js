// Which addresses a preview tab may show. A preview is for a server on this
// machine, so it's limited to these (the page's Content-Security-Policy says
// the same); localurl.test.mjs runs this in Node.

const LOCAL = new Set(["localhost", "127.0.0.1", "0.0.0.0", "[::1]"]);

// previewURL turns what was typed ("5173", ":5173", "localhost:3000/docs", a
// full URL) into the address to load, or null if it isn't a local server.
// Servers announce 0.0.0.0 and [::1], which a frame can't be limited to.
// own is this page's origin: Quarters is never shown inside itself, where the
// page in the frame could reach the app it's in.
export function previewURL(input, own = "") {
  let text = String(input || "").trim();
  if (!text) return null;
  if (/^:?\d{2,5}(\/|$)/.test(text)) text = `localhost:${text.replace(/^:/, "")}`;
  if (!/^https?:\/\//i.test(text)) {
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(text)) return null; // file://, javascript://…
    text = `http://${text}`;
  }
  let u;
  try {
    u = new URL(text);
  } catch {
    return null;
  }
  if (!/^https?:$/.test(u.protocol) || u.username || u.password || !LOCAL.has(u.hostname)) return null;
  if (u.hostname === "0.0.0.0" || u.hostname === "[::1]") u.hostname = "localhost";
  return u.origin === own ? null : u.href;
}

// previewReachable says whether this window can show the machine's local
// servers: the Mac app can, and so can a browser on that same machine. For a
// phone, localhost is the phone.
export const previewReachable = (remote, hostname) => !remote || hostname === "localhost" || hostname === "127.0.0.1" || hostname === "[::1]";

// previewName is the label of a preview's tab.
export function previewName(url) {
  try {
    return new URL(url).host;
  } catch {
    return "preview";
  }
}

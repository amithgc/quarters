// remote.js is Quarters in a browser: it stands in for the Mac app's
// window.go / window.runtime, carrying calls and events over one WebSocket.
// Signing in happens on /login (passkeys); this page only runs signed in.

const listeners = {}; // event -> [callback]
const pending = new Map(); // call id -> { resolve, reject }
const queue = []; // calls made while disconnected
let ws = null;
let nextID = 1;
let backoff = 500;
let connectedOnce = false;
const reconnectHooks = [];

export const remote = { client: "", device: "", access: "full", push: false, online: false, onReconnect: (cb) => reconnectHooks.push(cb) };

function emit(name, data) {
  for (const cb of listeners[name] || []) {
    try {
      cb(data);
    } catch (e) {
      console.error(e);
    }
  }
}

function setOnline(on) {
  remote.online = on;
  document.body.classList.toggle("offline", !on);
}

async function signedIn() {
  try {
    const r = await fetch("/auth/me", { credentials: "same-origin", cache: "no-store" });
    if (r.status === 401) return false;
  } catch {
    /* offline: keep trying */
  }
  return true;
}

function connect() {
  return new Promise((resolve) => {
    const sock = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/ws`);
    let hello = false;
    sock.onmessage = (e) => {
      let m;
      try {
        m = JSON.parse(e.data);
      } catch {
        return;
      }
      if (m.ev) {
        if (m.ev === "remote:hello") {
          hello = true;
          remote.client = m.d.client;
          remote.device = m.d.device;
          remote.access = m.d.access || "full";
          ws = sock;
          backoff = 500;
          setOnline(true);
          for (const q of queue.splice(0)) sock.send(q);
          resolve();
          if (connectedOnce) for (const cb of reconnectHooks) cb();
          connectedOnce = true;
          return;
        }
        emit(m.ev, m.d);
        return;
      }
      const p = pending.get(m.id);
      if (!p) return;
      pending.delete(m.id);
      m.e ? p.reject(new Error(m.e)) : p.resolve(m.r);
    };
    let gone = false;
    const lost = async (code) => {
      if (gone) return;
      gone = true;
      if (ws === sock) ws = null;
      setOnline(false);
      // calls not answered are failed, and must not run later, when the
      // connection is back: the page has already said they failed
      for (const [id, p] of pending) {
        p.reject(new Error("connection lost"));
        pending.delete(id);
      }
      queue.length = 0;
      if (code === 4401 || !(await signedIn())) {
        location.replace("/login");
        return;
      }
      setTimeout(() => connect().then(hello ? undefined : resolve), backoff);
      backoff = Math.min(backoff * 2, 8000);
    };
    sock.onclose = (e) => lost(e.code);
    sock.lost = lost;
  });
}

// Keystrokes and sizes are only worth sending live; other calls wait for the connection.
const liveOnly = new Set(["Write", "Resize", "Claim"]);

function call(m, args) {
  const id = nextID++;
  const msg = JSON.stringify({ id, m, a: args });
  if (liveOnly.has(m)) {
    if (ws && ws.readyState === 1) ws.send(msg);
    return Promise.resolve();
  }
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    if (ws && ws.readyState === 1) ws.send(msg);
    else queue.push(msg);
  });
}

// What only makes sense on the Mac itself, done the browser's way.
const local = {
  ChooseFolder: async () => "",
  ChooseIcon: async () => "",
  Confirm: async (title, message) => window.confirm(`${title}\n\n${message}`),
  OpenConfig: async () => {
    throw new Error("config.json can only be edited on the Mac");
  },
  RevealProject: async () => {
    throw new Error("Finder is only on the Mac");
  },
  SaveLayout: async () => {},
  SetBadge: async (n) => {
    document.title = n ? `(${n}) quarters` : "quarters";
    try {
      n ? navigator.setAppBadge?.(n) : navigator.clearAppBadge?.();
    } catch {}
  },
  Notify: async (title, body, key) => {
    // with phone alerts on, the Mac sends them (also when this page is closed)
    if (remote.push || !("Notification" in window) || Notification.permission !== "granted") return;
    try {
      const reg = await navigator.serviceWorker?.getRegistration();
      if (reg) reg.showNotification(title, { body, tag: key, data: { key }, icon: "/icons/icon-192.png", badge: "/icons/icon-192.png" });
      else new Notification(title, { body, tag: key });
    } catch {}
  },
};

window.go = {
  main: {
    App: new Proxy({}, { get: (_, m) => local[m] || ((...args) => call(m, args)) }),
  },
};

window.runtime = {
  EventsOn(name, cb) {
    (listeners[name] ||= []).push(cb);
  },
  BrowserOpenURL(url) {
    if (/^https?:\/\//i.test(url)) window.open(url, "_blank", "noopener,noreferrer");
  },
};

// ---- phone alerts (Web Push): the Mac pushes "Claude is waiting" ----

function keyBytes(b64url) {
  const b64 = b64url.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((b64url.length + 3) % 4);
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

// enablePush subscribes this browser to phone alerts (permission must be granted).
export async function enablePush() {
  const reg = await navigator.serviceWorker?.ready;
  if (!reg || !reg.pushManager) return false;
  const key = await call("PushKey", []);
  let sub = await reg.pushManager.getSubscription();
  // a subscription made for another key (remote.json was reset) no longer works
  const old = sub && sub.options && sub.options.applicationServerKey;
  if (sub && old && btoa(String.fromCharCode(...new Uint8Array(old))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") !== key) {
    await sub.unsubscribe();
    sub = null;
  }
  sub ||= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) });
  const j = sub.toJSON();
  await call("PushSubscribe", [{ endpoint: j.endpoint, p256dh: j.keys.p256dh, auth: j.keys.auth }]);
  remote.push = true;
  return true;
}

export async function disablePush() {
  const reg = await navigator.serviceWorker?.getRegistration();
  const sub = reg && reg.pushManager && (await reg.pushManager.getSubscription());
  remote.push = false;
  if (!sub) return;
  await call("PushUnsubscribe", [{ endpoint: sub.endpoint }]).catch(() => {});
  await sub.unsubscribe();
}

export async function signOut() {
  await fetch("/auth/logout", { method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin", body: "{}" }).catch(() => {});
  location.replace("/login");
}

// A notification tapped while the app was in the background opens its tab.
navigator.serviceWorker?.addEventListener("message", (e) => {
  if (e.data && e.data.type === "notify:open") emit("notify:open", e.data.key);
});

// The page wakes up after the phone slept: make sure the connection is live.
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible" || !ws || ws.readyState !== 1) return;
  const sock = ws;
  let answered = false;
  call("ClientID", []).then(() => (answered = true), () => {});
  // a socket that died while asleep can still look open: give up on it
  setTimeout(() => {
    if (answered || ws !== sock) return;
    sock.close();
    sock.lost(1006);
  }, 4000);
});

if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {});

await connect();

// Quarters' service worker. It makes the app installable and keeps the big
// static files (terminal library, fonts, pictures) on the device. Pages,
// sign-in and the live connection always go to the network: nothing behind
// the sign-in is ever answered from a cache.

const CACHE = "quarters-static-v2";
const STATIC = /^\/(vendor|mascot|icons)\//;

self.addEventListener("install", () => self.skipWaiting());

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

// Static files come from the cache at once and are refreshed in the
// background, so an update reaches the device on its next visit.
self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== "GET" || url.origin !== location.origin || !STATIC.test(url.pathname)) return;
  e.respondWith(
    caches.open(CACHE).then(async (cache) => {
      const hit = await cache.match(e.request);
      const fresh = fetch(e.request).then((res) => {
        if (res.ok) cache.put(e.request, res.clone());
        return res;
      });
      if (hit) {
        e.waitUntil(fresh.catch(() => {}));
        return hit;
      }
      return fresh;
    }),
  );
});

// A phone alert pushed by the Mac ("Claude is waiting", usage warnings).
self.addEventListener("push", (e) => {
  let m = {};
  try {
    m = e.data ? e.data.json() : {};
  } catch {}
  e.waitUntil(
    self.registration.showNotification(m.title || "quarters", {
      body: m.body || "", tag: m.key || "quarters", renotify: true, data: { key: m.key || "" },
      icon: "/icons/icon-192.png", badge: "/icons/icon-192.png",
    }),
  );
});

// Tapping a "your turn" notification opens the app on that tab.
self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const key = e.notification.data && e.notification.data.key;
  e.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
      const win = list[0];
      if (!win) return self.clients.openWindow(key ? "/#open=" + encodeURIComponent(key) : "/");
      win.postMessage({ type: "notify:open", key });
      return win.focus();
    }),
  );
});

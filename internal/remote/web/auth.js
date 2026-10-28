// Sign-in and pairing pages: passkeys (WebAuthn) only.

const $ = (s) => document.querySelector(s);
const page = document.body.dataset.page;

const b64 = {
  toBuf: (s) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "=")), (c) => c.charCodeAt(0)).buffer,
  fromBuf: (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, ""),
};

function say(text, kind = "") {
  $("#msg").textContent = text;
  $("#msg").className = "msg " + kind;
}

async function post(url, body) {
  const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin", body: JSON.stringify(body || {}) });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || `failed (${r.status})`);
  return data;
}

function creationOptions(o) {
  const pk = o.publicKey;
  pk.challenge = b64.toBuf(pk.challenge);
  pk.user.id = b64.toBuf(pk.user.id);
  for (const c of pk.excludeCredentials || []) c.id = b64.toBuf(c.id);
  return o;
}

function requestOptions(o) {
  const pk = o.publicKey;
  pk.challenge = b64.toBuf(pk.challenge);
  for (const c of pk.allowCredentials || []) c.id = b64.toBuf(c.id);
  return o;
}

function credentialJSON(cred) {
  const r = cred.response;
  const response = { clientDataJSON: b64.fromBuf(r.clientDataJSON) };
  if (r.attestationObject) {
    response.attestationObject = b64.fromBuf(r.attestationObject);
    if (r.getTransports) response.transports = r.getTransports();
  } else {
    response.authenticatorData = b64.fromBuf(r.authenticatorData);
    response.signature = b64.fromBuf(r.signature);
    if (r.userHandle) response.userHandle = b64.fromBuf(r.userHandle);
  }
  return { id: cred.id, rawId: b64.fromBuf(cred.rawId), type: cred.type, response,
    authenticatorAttachment: cred.authenticatorAttachment || undefined, clientExtensionResults: cred.getClientExtensionResults?.() || {} };
}

const friendly = (e) => (e && e.name === "NotAllowedError" ? "Cancelled, or the passkey was not found on this device." : String(e?.message || e));

function guessName() {
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return "iPhone";
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return "iPad";
  if (/Android/.test(ua)) return "Android";
  if (/Macintosh/.test(ua)) return "Mac browser";
  if (/Windows/.test(ua)) return "Windows PC";
  return "Browser";
}

async function login() {
  $("#go").disabled = true;
  say("Waiting for your passkey…");
  try {
    const opts = requestOptions(await post("/auth/login/begin"));
    const cred = await navigator.credentials.get(opts);
    await post("/auth/login/finish", credentialJSON(cred));
    say("Signed in.", "ok");
    location.replace("/");
  } catch (e) {
    say(friendly(e), "err");
    $("#go").disabled = false;
  }
}

async function pair(code) {
  const name = $("#name").value.trim();
  if (!name) return say("Give this device a name.", "err");
  $("#go").disabled = true;
  say("Creating your passkey…");
  try {
    const opts = creationOptions(await post("/auth/pair/begin", { code, name }));
    const cred = await navigator.credentials.create(opts);
    await post("/auth/pair/finish", credentialJSON(cred));
    say("Paired. Opening Quarters…", "ok");
    setTimeout(() => location.replace("/"), 600);
  } catch (e) {
    say(friendly(e), "err");
    $("#go").disabled = false;
  }
}

if (!window.PublicKeyCredential) {
  say("This browser cannot use passkeys. Use Safari or Chrome on a recent iOS, Android or computer.", "err");
  $("#go").disabled = true;
} else if (page === "login") {
  fetch("/auth/me", { credentials: "same-origin" }).then((r) => r.ok && location.replace("/"));
  $("#go").addEventListener("click", login);
} else if (page === "pair") {
  // The code is in the link's #fragment, which browsers never send to a server.
  const code = location.hash.slice(1);
  history.replaceState(null, "", "/pair");
  $("#name").value = guessName();
  if (!code) {
    say("Open the pairing link from the Mac (Remote Access → Pair a device).", "err");
    $("#go").disabled = true;
  } else $("#go").addEventListener("click", () => pair(code));
}

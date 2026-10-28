// Sign-in and pairing pages: passkeys (WebAuthn), and a username and
// password where a passkey can't be used.

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

async function passwordLogin(e) {
  e.preventDefault();
  $("#pw-go").disabled = true;
  say("Signing in…");
  try {
    await post("/auth/password", { user: $("#user").value.trim(), password: $("#pass").value });
    say("Signed in.", "ok");
    location.replace("/");
  } catch (err) {
    say(friendly(err), "err");
    $("#pass").value = "";
    $("#pw-go").disabled = false;
  }
}

// Passkeys need HTTPS (or localhost) and a name, never an IP address.
const passkeysWork = !!window.PublicKeyCredential && window.isSecureContext;

async function setupLogin() {
  fetch("/auth/me", { credentials: "same-origin" }).then((r) => r.ok && location.replace("/"));
  const info = await fetch("/auth/info", { credentials: "same-origin" }).then((r) => r.json()).catch(() => ({}));
  const passkey = passkeysWork && info.passkeyHere !== false;
  $("#go").classList.toggle("hidden", !passkey);
  $("#go").addEventListener("click", login);
  if (info.password) {
    $("#pw").classList.remove("hidden");
    $("#or").classList.toggle("hidden", !passkey);
    $("#pw-go").classList.toggle("quiet", passkey && info.passkeys);
    $("#pw").addEventListener("submit", passwordLogin);
    if (!passkey) {
      $("#foot").textContent = "Passkeys don't work at this address, so sign in with the username and password set on the Mac.";
      $("#user").focus();
    }
  } else if (!passkey) {
    say("Passkeys don't work at this address, and no password is set. On the Mac: Remote Access → Password sign-in.", "err");
    $("#foot").classList.add("hidden");
  }
}

// Android's Chrome installs web apps only from secure addresses: on the
// home network's plain HTTP, show how to let it trust this one address.
if (page === "login" && !window.isSecureContext && /Android/i.test(navigator.userAgent)) {
  $("#install-help").classList.remove("hidden");
  $("#install-origin").textContent = location.origin;
  $("#install-copy").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(location.origin);
      $("#install-copy").textContent = "copied";
    } catch {
      getSelection().selectAllChildren($("#install-origin")); // no clipboard on plain HTTP: select it instead
      $("#install-copy").textContent = "selected: copy it";
    }
  });
}

if (page === "login") {
  setupLogin();
} else if (!passkeysWork) {
  say("Pairing makes a passkey, which needs an https:// address or localhost. Here, sign in with the username and password instead.", "err");
  $("#go").disabled = true;
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

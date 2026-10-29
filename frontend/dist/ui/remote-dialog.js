// Remote access (the Mac app): the server, its addresses, paired devices and sign-in.
// Loaded the first time it's needed (see ui() in app.js).

import {
  $,
  Go,
  REMOTE,
  esc,
  flash,
  fmtAgo,
  on,
  openLink,
  renderRemotePill,
  rmd,
} from "../app.js";

// toggleRemote starts or stops the remote-access server from the menu.
async function toggleRemote() {
  if (REMOTE) return;
  const v = await Go().RemoteStatus();
  if (!v.url) return openRemoteDialog();
  try {
    await Go().SetRemote(!v.running, v.url, v.listen);
    flash(v.running ? "remote server stopped" : `remote server started · ${v.url}`);
  } catch (e) {
    flash(String(e).replace(/^Error: /, ""));
    openRemoteDialog();
  }
  Go().RemoteStatus().then(renderRemotePill).catch(() => {});
}

async function openRemoteDialog() {
  if (REMOTE) return;
  rmd.open = true;
  rmd.pairing = null;
  $("#remote-dialog").classList.remove("hidden");
  $("#rm-error").textContent = "";
  const v = await Go().RemoteStatus();
  $("#rm-url").value = v.url || "";
  $("#rm-listen").value = v.listen || "";
  $("#rm-lan").checked = !!v.lan;
  $("#rm-user").value = v.user || "";
  $("#rm-pass").value = $("#rm-pass2").value = "";
  rmd.devices = (v.devices || []).length;
  renderRemote(v);
  clearInterval(rmd.timer);
  rmd.timer = setInterval(refreshRemoteDialog, 2000);
}

function closeRemoteDialog() {
  rmd.open = false;
  clearInterval(rmd.timer);
  if (rmd.pairing) Go().CancelPairing();
  rmd.pairing = null;
  $("#remote-dialog").classList.add("hidden");
}

async function refreshRemoteDialog() {
  if (!rmd.open) return;
  const v = await Go().RemoteStatus();
  const n = (v.devices || []).length;
  if (rmd.pairing && n > rmd.devices) {
    const d = v.devices[n - 1];
    rmd.pairing = null;
    flash(`paired ${d.name}`);
  }
  rmd.devices = n;
  renderRemote(v);
}

function renderRemote(v) {
  rmd.status = v;
  $("#rm-state").innerHTML = v.running ? '<span class="green">● running</span>' : v.enabled ? '<span class="red">● failed to start</span>' : '<span class="muted">○ stopped</span>';
  const online = (v.devices || []).reduce((n, d) => n + (d.online || 0), 0);
  $("#rm-power-text").innerHTML = v.running
    ? `<b class="green">Server running</b> <span class="muted">at <a href="#" class="rm-addr" title="Open in your browser">${esc(v.address || v.url)}</a>${online ? ` · ${online} connected` : ""}</span>`
    : `<b>Server stopped</b> <span class="muted">— nothing can reach this Mac from outside</span>`;
  $("#rm-toggle").textContent = v.running ? "Stop server" : "Start server";
  $("#rm-toggle").classList.toggle("primary", !v.running);
  if (v.error) $("#rm-error").textContent = v.error;
  const devs = v.devices || [];
  $("#rm-devices").innerHTML = devs.length
    ? devs.map((d) => `<div class="rm-dev"><span class="${d.online ? "green" : "muted"}">${d.online ? "●" : "○"}</span>
        <span class="grow"><b>${esc(d.name)}</b>${d.push ? ' <span title="Gets phone alerts">🔔</span>' : ""} <span class="muted">${d.lastUsed ? `last used ${fmtAgo(d.lastUsed)} ago${d.lastIP ? " · " + esc(d.lastIP) : ""}` : ""}</span></span>
        <select class="rm-access" data-access="${esc(d.id)}" title="What this device may do">${[["full", "full control"], ["type", "type only"], ["view", "watch only"]].map(([v, l]) => `<option value="${v}"${(d.access || "full") === v ? " selected" : ""}>${l}</option>`).join("")}</select>
        <button class="btn danger" type="button" data-remove="${esc(d.id)}">Remove</button></div>`).join("")
    : '<div class="muted">No devices yet. Turn remote access on, then pair your phone.</div>';
  $("#rm-pw-state").innerHTML = v.user
    ? `On for <b>${esc(v.user)}</b>. Set a new password to change it (its browsers are signed out); remove <i>Password · ${esc(v.user)}</i> under Devices to turn it off.`
    : "Off. Set one (10+ characters) for browsers that can't use a passkey, such as at the Mac's home-network address.";
  $("#rm-pw-set").textContent = v.user ? "Change password" : "Set password";
  const li = $("#rm-lan-info"), lanOn = v.running && v.lan && (v.lanURLs || []).length > 0;
  li.classList.toggle("hidden", !lanOn);
  if (lanOn && li.dataset.key !== v.lanURLs.join() + "|" + v.user) { // rebuilt only when it changes
    li.dataset.key = v.lanURLs.join() + "|" + v.user;
    li.innerHTML = `<img src="${v.lanQR}" alt="QR code of ${esc(v.lanURLs[0])}" width="132" height="132">
      <div><b>On your home network</b><div class="muted">Scan with your phone's camera, or open:</div>
      ${v.lanURLs.map((u) => `<div><code>${esc(u)}</code></div>`).join("")}
      <div class="muted">${v.user ? `Sign in as <b>${esc(v.user)}</b>.` : '<span class="red">Set a password below first.</span>'}</div></div>`;
  }
  $("#rm-pair").disabled = !v.running;
  $("#rm-signout").disabled = !v.running;
  const pb = $("#rm-pairing");
  pb.classList.toggle("hidden", !rmd.pairing);
  if (rmd.pairing) {
    const left = Math.max(0, Math.round((rmd.pairing.expires - Date.now()) / 1000));
    if (!pb.dataset.url || pb.dataset.url !== rmd.pairing.url) {
      pb.dataset.url = rmd.pairing.url;
      pb.innerHTML = `<img src="${rmd.pairing.qr}" alt="pairing QR code">
        <div class="rm-pair-text"><b>Scan with your phone's camera</b>
          <span class="muted">or open this link on the device (it works once):</span>
          <code>${esc(rmd.pairing.url)}</code>
          <span class="muted" id="rm-left"></span>
          <button class="btn" type="button" id="rm-pair-cancel">Cancel</button></div>`;
      requestAnimationFrame(() => pb.scrollIntoView({ block: "nearest", behavior: "smooth" }));
      $("#rm-pair-cancel").onclick = () => {
        Go().CancelPairing();
        rmd.pairing = null;
        refreshRemoteDialog();
      };
    }
    $("#rm-left").textContent = left ? `expires in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}` : "expired";
  } else pb.dataset.url = "";
  $("#rm-log").textContent = (v.log || []).join("\n") || "nothing yet";
  renderRemotePill(v);
}

function wireRemoteDialog() {
  $("#rm-close").addEventListener("click", closeRemoteDialog);
  $("#remote-dialog").addEventListener("mousedown", (e) => e.target.id === "remote-dialog" && closeRemoteDialog());
  $("#remote-dialog").addEventListener("keydown", (e) => e.key === "Escape" && closeRemoteDialog());
  // Save keeps the server as it is (restarting it with the new addresses if it runs).
  const setRemote = async (on) => {
    $("#rm-error").textContent = "";
    try {
      await Go().SetRemote(on, $("#rm-url").value, $("#rm-listen").value, $("#rm-lan").checked);
      return true;
    } catch (e) {
      $("#rm-error").textContent = String(e).replace(/^Error: /, "");
      return false;
    } finally {
      refreshRemoteDialog();
    }
  };
  $("#rm-save").addEventListener("click", async () => (await setRemote(!!(rmd.status && rmd.status.enabled))) && flash("remote access saved"));
  $("#rm-lan").addEventListener("change", async () => (await setRemote(!!(rmd.status && rmd.status.enabled))) && flash($("#rm-lan").checked ? "home network on" : "home network off"));
  $("#rm-pw-set").addEventListener("click", async () => {
    const user = $("#rm-user").value.trim(), pw = $("#rm-pass").value;
    $("#rm-error").textContent = "";
    if (pw !== $("#rm-pass2").value) return ($("#rm-error").textContent = "the two passwords are different");
    try {
      await Go().SetRemotePassword(user, pw);
      $("#rm-pass").value = $("#rm-pass2").value = "";
      flash(`password set for ${user}`);
    } catch (e) {
      $("#rm-error").textContent = String(e).replace(/^Error: /, "");
    }
    refreshRemoteDialog();
  });
  $("#rm-power-text").addEventListener("click", (e) => {
    if (!e.target.closest(".rm-addr")) return;
    e.preventDefault();
    openLink(rmd.status.address || rmd.status.url);
  });
  $("#rm-toggle").addEventListener("click", async () => {
    const on = !(rmd.status && rmd.status.running);
    if (await setRemote(on)) flash(on ? "remote server started" : "remote server stopped");
  });
  $("#rm-pair").addEventListener("click", async () => {
    try {
      rmd.pairing = await Go().PairDevice();
      refreshRemoteDialog();
    } catch (e) {
      $("#rm-error").textContent = String(e);
    }
  });
  $("#rm-signout").addEventListener("click", async () => {
    if (await Go().Confirm("Sign out all browsers?", "Every signed-in browser is disconnected. Devices stay paired and can sign in again with their passkey.", "Sign out all")) {
      Go().SignOutAllDevices();
      refreshRemoteDialog();
    }
  });
  $("#rm-devices").addEventListener("change", async (e) => {
    const sel = e.target.closest("[data-access]");
    if (!sel) return;
    try {
      await Go().SetDeviceAccess(sel.dataset.access, sel.value);
      flash(`${sel.closest(".rm-dev").querySelector("b").textContent}: ${sel.selectedOptions[0].textContent}`);
    } catch (err) {
      flash(String(err));
    }
    refreshRemoteDialog();
  });
  $("#rm-devices").addEventListener("click", async (e) => {
    const b = e.target.closest("[data-remove]");
    if (!b) return;
    const name = b.closest(".rm-dev").querySelector("b").textContent;
    if (b.dataset.remove === "password") {
      if (!(await Go().Confirm("Turn off password sign-in?", "Browsers signed in with the password are signed out at once. You can set a password again later.", "Turn off"))) return;
    } else if (!(await Go().Confirm(`Remove ${name}?`, "It is signed out at once and its passkey stops working here. You can pair it again later.", "Remove"))) return;
    try {
      await Go().RemoveDevice(b.dataset.remove);
    } catch (err) {
      flash(String(err));
    }
    refreshRemoteDialog();
  });
}

wireRemoteDialog();

export { toggleRemote, openRemoteDialog };

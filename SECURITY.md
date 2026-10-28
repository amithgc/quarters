# Security

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Report it
privately through GitHub:
[Security → Report a vulnerability](https://github.com/amithgc/quarters/security/advisories/new).
Include what an attacker can do, how to reproduce it, and the version
(About Quarters). You'll get an answer within a few days, and credit in the
release notes if you'd like.

Remote access is the part that matters most: anything that lets someone who
hasn't paired a device reach a session, read files, or run commands is
critical.

## What Quarters protects, and how

Quarters runs on your Mac with your user's rights. Its terminals run Claude
Code and shells, so **anyone who can type into one can run commands as you**.
Remote access exists to let *you* do that from your phone, and is designed
so that nobody else can.

### Remote access is off until you turn it on

The web server only runs while you've started it (Remote Access → Start
server, or ⌥⌘R). It listens on `127.0.0.1`; a non-loopback address is refused
unless you configure TLS certificates. The internet reaches it through a
tunnel you run (Cloudflare Tunnel, Tailscale Funnel, …), so no ports are
opened on your router.

### Signing in

- **Passkeys only** (WebAuthn, user verification required: Face ID, Touch ID
  or the device PIN). There are no passwords. Passkeys are bound to your
  domain, so a look-alike site can't use them.
- **Pairing only from the Mac.** Remote Access → Pair a device makes a link
  (shown as a QR code) carrying a one-time code in the URL fragment, which is
  never sent to a server or logged. It works for one device, for 10 minutes,
  and is burnt after 5 wrong codes.
- **Sessions** are random 256-bit tokens kept in memory (only their SHA-256
  hash); a restart signs everyone out. They end after 12 hours unused and 7
  days at most. The cookie is `__Host-` prefixed, `HttpOnly`, `Secure` and
  `SameSite=Strict`.
- **Rate limits:** 20 sign-in requests per address per 5 minutes. An address
  with 5 failures is locked out for a minute, doubling with each further
  failure (up to an hour). 100 failures from anywhere within 10 minutes lock
  sign-in for everyone for 15 minutes, and the Mac shows a notification.
- The client address comes from the last `X-Forwarded-For` entry (the one the
  local tunnel appends), and only for connections from the tunnel on this
  Mac. Headers a client can set itself are not trusted.
- A passkey whose signature counter goes backwards (a sign of cloning) is
  refused, and you're told.
- The Mac notifies you of every sign-in and every newly paired device, and
  `~/.config/quarters/remote.log` records them, with every failure.

### Every request

- The `Host` header must be your public address or the local listener,
  which stops DNS-rebinding attacks.
- Sign-in requests and the WebSocket must come from your own origin, which
  stops cross-site requests and WebSocket hijacking.
- A strict Content-Security-Policy applies, the page can't be framed, and
  the service worker caches only static files (never anything behind
  sign-in).
- The session is checked on every WebSocket message. Signing out, removing a
  device or changing its access disconnects it at once.

### What a browser can do

The Mac window and a browser use the same functions, but a browser goes
through an allowlist (`remote_app.go`), and these are **Mac-only**: turning
remote access on, pairing, managing devices, the global hotkey, the default
permission mode, and *Remote sessions ask permission*.

Each device has an access level, enforced on the Mac:

| level | can |
| --- | --- |
| full control | everything the Mac window can do, except the Mac-only settings above |
| type only | type into terminals that are already open; can't start, close or change anything |
| watch only | see projects, terminals and stats; can't type or change anything |

Be clear about what these mean:

- **Type only is still shell access**, because a shell tab, or Claude itself,
  runs what you type. It stops a device from changing your setup, not from
  running commands.
- ***Remote sessions ask permission*** makes Claude started from a browser
  ask before using tools. It is a guard rail, not a sandbox: a full-control
  device can still open a shell.

Files a browser can read are limited to your projects: diffs, icons and
pasted images. Files ignored by git, and files that look like keys or
passwords (`.env`, `*.pem`, `id_rsa`, …), are never shown whole.

### Phone alerts (Web Push)

Push messages are encrypted for each device and signed with VAPID keys kept
in `remote.json`. They are only sent to the push services of Apple, Google,
Mozilla and Microsoft, so a device can't make Quarters send requests
anywhere else.

### Files

`~/.config/quarters/remote.json` holds the paired devices' **public** keys,
the WebAuthn user id and the VAPID keys; `remote.log` is the security log.
Both are written with mode 0600. Don't share them.

### Your Claude login

To show your plan's usage limits, Quarters reads the login Claude Code keeps
in the macOS Keychain and sends it only to Anthropic's API, as Claude Code
does. It never refreshes or changes it.

## If you lose a phone

Remote Access → find the device → **Remove**. It is signed out at once and
its passkey stops working. **Sign out all browsers** ends every session if
you're unsure which device it was.

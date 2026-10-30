# Third-party notices

Quarters includes or links to the following software. Thanks to their
authors.

## Shipped in the app's frontend (`frontend/dist/vendor`)

| | license |
| --- | --- |
| [xterm.js](https://github.com/xtermjs/xterm.js) and its fit, webgl, web-links and unicode11 addons | MIT, © The xterm.js authors (`LICENSE-xterm.txt`) |
| [CodeMirror 6](https://codemirror.net) (the file editor) with its language packages, Lezer parsers, style-mod, crelt and w3c-keyname | MIT, © Marijn Haverbeke and others (`LICENSE-codemirror.txt`) |
| [JetBrains Mono](https://github.com/JetBrains/JetBrainsMono) font, via [Fontsource](https://fontsource.org) | SIL Open Font License 1.1, © The JetBrains Mono Project Authors (`fonts/LICENSE-JetBrainsMono.txt`) |

## Go modules compiled into the app

| module | license |
| --- | --- |
| [github.com/wailsapp/wails/v2](https://github.com/wailsapp/wails) | MIT |
| [github.com/creack/pty](https://github.com/creack/pty) | MIT |
| [github.com/go-webauthn/webauthn](https://github.com/go-webauthn/webauthn) | BSD-3-Clause |
| [github.com/gorilla/websocket](https://github.com/gorilla/websocket) | BSD-2-Clause |
| [github.com/SherClockHolmes/webpush-go](https://github.com/SherClockHolmes/webpush-go) | MIT |
| [rsc.io/qr](https://github.com/rsc/qr) | BSD-3-Clause |

Their own dependencies are listed in `go.sum`; their licenses are in the Go
module cache (`go mod download -json`).

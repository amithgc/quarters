// Copies xterm.js, its addons and the JetBrains Mono font (with their
// licences) into dist/vendor, which the app embeds, and bundles the file
// editor's CodeMirror (codemirror.mjs) into one file there. Run by
// `npm run build`.
// ES modules are saved with a .js extension so every server sends them with
// a JavaScript content type.
import { cpSync, mkdirSync } from "node:fs";
import { build } from "esbuild";

const out = "dist/vendor";
mkdirSync(`${out}/fonts`, { recursive: true });
const files = {
  "@xterm/xterm/lib/xterm.mjs": "xterm.js",
  "@xterm/xterm/css/xterm.css": "xterm.css",
  "@xterm/addon-fit/lib/addon-fit.mjs": "addon-fit.js",
  "@xterm/addon-webgl/lib/addon-webgl.mjs": "addon-webgl.js",
  "@xterm/addon-web-links/lib/addon-web-links.mjs": "addon-web-links.js",
  "@xterm/addon-unicode11/lib/addon-unicode11.mjs": "addon-unicode11.js",
};
for (const [from, to] of Object.entries(files)) cpSync(`node_modules/${from}`, `${out}/${to}`);
// their licences travel with them
cpSync("node_modules/@xterm/xterm/LICENSE", `${out}/LICENSE-xterm.txt`);
cpSync("node_modules/@fontsource/jetbrains-mono/LICENSE", `${out}/fonts/LICENSE-JetBrainsMono.txt`);
for (const w of [400, 500, 700]) {
  for (const style of ["normal", "italic"]) {
    cpSync(`node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-${w}-${style}.woff2`,
      `${out}/fonts/jetbrains-mono-${w}-${style}.woff2`);
  }
}
// CodeMirror is many small modules: one minified file, loaded when a file opens
await build({ entryPoints: ["codemirror.mjs"], bundle: true, format: "esm", minify: true, target: "safari16", outfile: `${out}/codemirror.js`, legalComments: "none", logLevel: "warning" });
cpSync("node_modules/@codemirror/view/LICENSE", `${out}/LICENSE-codemirror.txt`);
console.log("vendored xterm.js, CodeMirror and JetBrains Mono into", out);

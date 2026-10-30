// Copies xterm.js, its addons, the JetBrains Mono font and the emoji list
// (with their licences) into dist/vendor, which the app embeds, and bundles
// the file editor's CodeMirror (codemirror.mjs) into one file there. Run by
// `npm run build`.
// ES modules are saved with a .js extension so every server sends them with
// a JavaScript content type.
import { cpSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
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
// every emoji, by group, for the icon picker: [{ name, emoji: [[emoji, name]…] }],
// leaving out ones too new for most systems to draw
const groups = JSON.parse(readFileSync("node_modules/unicode-emoji-json/data-by-group.json", "utf8"));
const emoji = groups
  .filter((g) => g.slug !== "component")
  .map((g) => ({ name: g.name, emoji: g.emojis.filter((e) => +e.emoji_version <= 15.1).map((e) => [e.emoji, e.name]) }));
writeFileSync(`${out}/emoji.json`, JSON.stringify(emoji));
cpSync("node_modules/unicode-emoji-json/LICENSE", `${out}/LICENSE-unicode-emoji-json.txt`);
console.log("vendored xterm.js, CodeMirror, JetBrains Mono and the emoji list into", out);

// The file editor's CodeMirror: the pieces ui/fileview.js uses, bundled into
// dist/vendor/codemirror.js by vendor.mjs (loaded the first time a file opens).

import { StreamLanguage } from "@codemirror/language";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { go } from "@codemirror/lang-go";
import { rust } from "@codemirror/lang-rust";
import { yaml } from "@codemirror/lang-yaml";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { swift } from "@codemirror/legacy-modes/mode/swift";
import { ruby } from "@codemirror/legacy-modes/mode/ruby";
import { lua } from "@codemirror/legacy-modes/mode/lua";
import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
import { properties } from "@codemirror/legacy-modes/mode/properties";
import { standardSQL } from "@codemirror/legacy-modes/mode/sql";
import { c, cpp, java, kotlin, csharp, objectiveC, dart } from "@codemirror/legacy-modes/mode/clike";

export { EditorState, Compartment } from "@codemirror/state";
export { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, highlightSpecialChars, rectangularSelection, crosshairCursor } from "@codemirror/view";
export { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
export { searchKeymap, highlightSelectionMatches, openSearchPanel } from "@codemirror/search";
export { syntaxHighlighting, HighlightStyle, indentOnInput, bracketMatching, foldGutter, foldKeymap, indentUnit } from "@codemirror/language";
export { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
export { tags } from "@lezer/highlight";

const legacy = (mode) => () => StreamLanguage.define(mode);

// by file extension (or whole name, for files like Dockerfile)
const LANGS = {
  js: () => javascript(), mjs: () => javascript(), cjs: () => javascript(),
  jsx: () => javascript({ jsx: true }),
  ts: () => javascript({ typescript: true }), mts: () => javascript({ typescript: true }), cts: () => javascript({ typescript: true }),
  tsx: () => javascript({ jsx: true, typescript: true }),
  json: () => json(), jsonc: () => json(), webmanifest: () => json(),
  css: () => css(), scss: () => css(), less: () => css(),
  html: () => html(), htm: () => html(), vue: () => html(), svelte: () => html(), svg: () => html(), xml: () => html(), plist: () => html(),
  md: () => markdown(), markdown: () => markdown(), mdx: () => markdown(),
  py: () => python(), pyi: () => python(),
  go: () => go(),
  rs: () => rust(),
  yml: () => yaml(), yaml: () => yaml(),
  sh: legacy(shell), bash: legacy(shell), zsh: legacy(shell), fish: legacy(shell), zshrc: legacy(shell), bashrc: legacy(shell), env: legacy(properties),
  toml: legacy(toml), ini: legacy(properties), cfg: legacy(properties), properties: legacy(properties), conf: legacy(properties),
  swift: legacy(swift), rb: legacy(ruby), lua: legacy(lua), sql: legacy(standardSQL),
  c: legacy(c), h: legacy(c), cc: legacy(cpp), cpp: legacy(cpp), hpp: legacy(cpp), m: legacy(objectiveC), mm: legacy(objectiveC),
  java: legacy(java), kt: legacy(kotlin), kts: legacy(kotlin), cs: legacy(csharp), dart: legacy(dart),
  dockerfile: legacy(dockerFile), makefile: legacy(shell), gemfile: legacy(ruby), rakefile: legacy(ruby),
};

// languageFor is the syntax colouring for a file name, or null (plain text).
export function languageFor(name) {
  const base = name.toLowerCase();
  const ext = base.includes(".") ? base.split(".").pop() : base;
  const make = LANGS[base.replace(/^\./, "")] || LANGS[ext] || (base.startsWith("dockerfile") ? LANGS.dockerfile : null);
  return make ? make() : null;
}

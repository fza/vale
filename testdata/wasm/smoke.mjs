// Exercises the wasm build's JavaScript API the way a browser host does:
// no filesystem, so wasm_exec.js's stub answers every os call, and every
// asset arrives through ValeAddFile. Run with `make test-wasm`.
//
//   node smoke.mjs <wasm_exec.js> <vale.wasm>
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import assert from "node:assert/strict";

const require = createRequire(import.meta.url);
globalThis.require = require;
globalThis.TextEncoder = TextEncoder;
globalThis.TextDecoder = TextDecoder;
globalThis.performance ??= { now: () => Date.now() };
const crypto = await import("node:crypto");
globalThis.crypto ??= crypto.webcrypto;

require(path.resolve(process.argv[2]));
const go = new Go();
const { instance } = await WebAssembly.instantiate(fs.readFileSync(path.resolve(process.argv[3])), go.importObject);
go.run(instance);
await new Promise((r) => setTimeout(r, 50));

for (const name of ["ValeLint", "ValeSuggest", "ValeTransform", "ValeAddFile", "ValeAddPackage", "ValeResetFS", "ValeListFS"]) {
  assert.equal(typeof globalThis[name], "function", `${name} is installed`);
}

// The built-in style needs no StylesPath.
const builtin = "MinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = Vale\n";
let out = ValeLint("This is a very important sentence. javascript agendize\n", "md", builtin);
assert.equal(out.error, undefined, out.error);
let alerts = JSON.parse(out.result)["input.md"];
assert.deepEqual(alerts.map((a) => [a.Check, a.Match]), [
  ["Vale.Spelling", "javascript"],
  ["Vale.Spelling", "agendize"],
]);

// A fix is resolved from the alert.
out = ValeSuggest(JSON.stringify(alerts[0]), builtin);
assert.deepEqual(out.suggestions, ["JavaScript"]);

// A style added to the in-memory filesystem is found through StylesPath.
ValeAddFile("/styles/T/W.yml", "extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - very\n");
assert.deepEqual(ValeListFS().paths, ["/styles/T/W.yml"]);
const custom = "StylesPath = /styles\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = T\n";
out = ValeLint("This is very good.\n", "md", custom);
assert.equal(out.error, undefined, out.error);
alerts = JSON.parse(out.result)["input.md"];
assert.deepEqual(alerts.map((a) => [a.Check, a.Line, a.Span]), [["T.W", 1, [9, 12]]]);

// Markup is understood: a code span is not linted.
out = ValeLint("Use `very` here.\n", "md", custom);
assert.deepEqual(JSON.parse(out.result)["input.md"], []);

// A code file is accepted but not read: its comments need tree-sitter, which
// the browser build has none of.
out = ValeLint("// This is very good.\nconst a = 1;\n", "js", custom);
assert.equal(out.error, undefined, out.error);
assert.deepEqual(JSON.parse(out.result)["input.js"], []);

// Plain text and HTML are read.
out = ValeLint("This is very good.\n", "txt", custom);
assert.equal(JSON.parse(out.result)["input.txt"].length, 1);
out = ValeLint("<p>This is <code>very</code> very good.</p>\n", "html", custom);
assert.deepEqual(JSON.parse(out.result)["input.html"].map((a) => a.Span), [[30, 33]]);

ValeResetFS();
assert.deepEqual(ValeListFS().paths, []);

console.log("ok");
process.exit(0);

// Every method the UI declares on window.go.main.App must exist as an exported
// method on the Go App. Nothing else catches a mismatch: `go build` only sees the
// Go side, and wails.d.ts is hand-written, so `tsc` believes whatever it declares.
// A renamed binding therefore ships green and fails at runtime with
// "The CARE Clinic runtime has no <name> method."
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url)); // app/frontend/scripts
const appDir = join(here, "..", "..");

// The App block in wails.d.ts: everything between "App: {" and its closing brace.
const dts = readFileSync(join(appDir, "frontend", "src", "wails.d.ts"), "utf8");
const block = appBlock(dts);
const declared = [...block.matchAll(/^\s+([A-Z]\w*)\(/gm)].map((m) => m[1]);

// Brace-matched rather than indentation-matched: the sibling "runtime" block
// declares EventsOn/LogPrint, which are Wails' own and not App methods.
function appBlock(src) {
  const start = src.indexOf("App: {");
  if (start < 0) throw new Error("no App block in wails.d.ts - has its shape changed?");
  let depth = 0;
  for (let i = src.indexOf("{", start); i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}" && --depth === 0) return src.slice(start, i);
  }
  throw new Error("unbalanced braces in wails.d.ts");
}
if (declared.length === 0) throw new Error("no bindings found in wails.d.ts - has its shape changed?");

const go = readdirSync(appDir)
  .filter((f) => f.endsWith(".go"))
  .map((f) => readFileSync(join(appDir, f), "utf8"))
  .join("\n");
const defined = new Set([...go.matchAll(/^func \(a \*App\) ([A-Z]\w*)\(/gm)].map((m) => m[1]));

const missing = declared.filter((name) => !defined.has(name));
if (missing.length > 0) {
  console.error("wails.d.ts declares bindings the Go App does not export:");
  for (const name of missing) console.error(`  ${name}`);
  console.error("\nRename both sides, or the UI fails at runtime, not at build.");
  process.exit(1);
}
const parameters = (text) => text.split(",").map((s) => s.trim()).filter(Boolean).length;
const goParameters = new Map(
  [...go.matchAll(/^func \(a \*App\) ([A-Z]\w*)\(([^)]*)\)/gm)]
    .map((m) => [m[1], parameters(m[2])]),
);
const mismatched = [...block.matchAll(/^\s+([A-Z]\w*)\(([^)]*)\)/gm)]
  .filter((m) => goParameters.get(m[1]) !== parameters(m[2]));
if (mismatched.length > 0) {
  for (const m of mismatched) {
    console.error(`${m[1]}: Go accepts ${goParameters.get(m[1])} arguments, wails.d.ts declares ${parameters(m[2])}`);
  }
  process.exit(1);
}
console.log(`bindings ok — ${declared.length} declared, all present on the Go App`);

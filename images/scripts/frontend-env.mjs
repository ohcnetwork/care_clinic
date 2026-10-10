#!/usr/bin/env node
// Writes the settings CI compiles the CARE frontend image with.
//
//   node images/scripts/frontend-env.mjs [--out images/frontend/clinic.env] [--json frontend-build.json]
//
// Compile-time values come from deployments/frontend.env (what a new clinic
// starts with), so a fresh install never needs a local rebuild. Every key in
// images/frontend/runtime-keys.json is compiled as a placeholder instead and
// filled in when the container starts. The --json file records both lists for
// the release-line manifest (runtime_keys and build_env).
//
// Keep the placeholder format in sync with PlaceholderPrefix in
// app/internal/compose/build.go and images/frontend/configure/main.go.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
export const PLACEHOLDER_PREFIX = "https://care-clinic.invalid/__CARE_CLINIC_";

export function parseEnv(text) {
  const env = {};
  for (const [i, raw] of text.split(/\r?\n/).entries()) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const m = line.match(/^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/);
    if (!m) throw new Error(`line ${i + 1} is not KEY=value`);
    let value = m[2];
    const q = value[0];
    if (q === "'" || q === '"' || q === "`") {
      const end = value.indexOf(q, 1);
      if (end < 0) throw new Error(`line ${i + 1}: unterminated quote`);
      value = value.slice(1, end);
      if (q === '"') value = value.replace(/\\n/g, "\n").replace(/\\(.)/g, "$1");
    } else {
      value = value.replace(/\s+#.*$/, "").trim();
    }
    env[m[1]] = value;
  }
  return env;
}

export function quote(value) {
  if (!value.includes("'")) return `'${value}'`;
  if (!/["\\$\n]/.test(value)) return `"${value}"`;
  if (!value.includes("`")) return "`" + value + "`";
  throw new Error("value cannot be quoted for dotenv: " + JSON.stringify(value));
}

export function frontendBuild(clinicEnv, runtimeKeys) {
  const runtime = Object.keys(runtimeKeys).sort();
  for (const key of runtime) {
    if (!/^REACT_[A-Z0-9_]+$/.test(key)) throw new Error(`runtime key ${key} must be a REACT_ setting`);
  }
  const buildEnv = {};
  for (const [key, value] of Object.entries(clinicEnv)) {
    if (value !== "" && key.startsWith("REACT_") && !runtime.includes(key)) buildEnv[key] = value;
  }
  const compiled = { ...buildEnv };
  for (const key of runtime) compiled[key] = PLACEHOLDER_PREFIX + key + "__";
  const dotenv = Object.keys(compiled).sort().map(k => `${k}=${quote(compiled[k])}\n`).join("");
  return { runtime_keys: runtime, build_env: buildEnv, dotenv };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const opt = name => {
    const i = args.indexOf(name);
    return i >= 0 ? args[i + 1] : undefined;
  };
  const out = opt("--out") ?? join(root, "images", "frontend", "clinic.env");
  const json = opt("--json");
  const clinicEnv = parseEnv(readFileSync(join(root, "deployments", "frontend.env"), "utf8"));
  const runtimeKeys = JSON.parse(readFileSync(join(root, "images", "frontend", "runtime-keys.json"), "utf8"));
  const build = frontendBuild(clinicEnv, runtimeKeys);
  writeFileSync(out, build.dotenv);
  if (json) {
    writeFileSync(json, JSON.stringify({ runtime_keys: build.runtime_keys, build_env: build.build_env }, null, 2) + "\n");
  }
  console.log(`wrote ${out}: ${build.runtime_keys.length} runtime settings, ${Object.keys(build.build_env).length} compiled in`);
}

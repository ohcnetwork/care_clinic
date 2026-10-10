#!/usr/bin/env node
// Renders the release-line manifest (lines.json) CARE Clinic reads.
//
//   node images/scripts/render-lines.mjs --source images/lines.source.json \
//     --builds <dir> [--previous lines.json] --out lines.json
//
// <dir>/<line id>.json is what the image workflow records for each line it
// built: {"backend": {"commit","image","builder"},
//         "frontend": {"commit","image","builder","runtime_keys","build_env"}}
// with images referenced by digest (name@sha256:...). A line without a new
// build keeps its entry from --previous, so a failed build never removes a line.
// The rules mirror app/internal/lines (Validate); keep them in sync.
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const ID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const COMMIT = /^[0-9a-f]{40}$/;
const IMAGE = /^[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}$/;
const KEY = /^[A-Z][A-Z0-9_]*$/;
const VERSION = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

function image(where, value, frontend) {
  if (!value || typeof value !== "object") throw new Error(`${where} is missing`);
  if (!COMMIT.test(value.commit ?? "")) throw new Error(`${where}.commit must be a 40-character commit SHA`);
  for (const field of ["image", "builder"]) {
    if (!IMAGE.test(value[field] ?? "")) throw new Error(`${where}.${field} must be name@sha256:<digest>`);
  }
  const out = { commit: value.commit, image: value.image, builder: value.builder };
  if (frontend) {
    const keys = value.runtime_keys ?? [];
    if (!Array.isArray(keys) || keys.length === 0 || !keys.every(k => KEY.test(k))) {
      throw new Error(`${where}.runtime_keys must list the settings the image applies at start`);
    }
    const env = value.build_env ?? {};
    if (typeof env !== "object" || Array.isArray(env)) throw new Error(`${where}.build_env is invalid`);
    for (const [k, v] of Object.entries(env)) {
      if (!KEY.test(k) || typeof v !== "string") throw new Error(`${where}.build_env.${k} is invalid`);
      if (keys.includes(k)) throw new Error(`${where}: ${k} cannot be both a runtime key and compiled in`);
    }
    if (keys.length) out.runtime_keys = [...keys].sort();
    if (Object.keys(env).length) out.build_env = env;
  }
  return out;
}

export function render(source, builds, previous) {
  if (!Array.isArray(source.lines) || source.lines.length === 0) throw new Error("source lists no lines");
  const lines = [];
  const seen = new Set();
  for (const s of source.lines) {
    if (!ID.test(s.id ?? "")) throw new Error(`invalid line id ${JSON.stringify(s.id)}`);
    if (seen.has(s.id)) throw new Error(`line ${s.id} is listed twice`);
    seen.add(s.id);
    for (const field of ["min_app_version", "max_app_version"]) {
      if (s[field] && !VERSION.test(s[field])) throw new Error(`${s.id}.${field} must be X.Y.Z`);
    }
    if (s.supported_until && !DATE.test(s.supported_until)) throw new Error(`${s.id}.supported_until must be YYYY-MM-DD`);
    const built = builds[s.id] ?? previous?.lines?.find(l => l.id === s.id);
    if (!built) {
      console.warn(`skipping line ${s.id}: no published images yet`);
      continue;
    }
    const line = {
      id: s.id,
      ...(s.title ? { title: s.title } : {}),
      backend: image(`${s.id}.backend`, built.backend, false),
      frontend: image(`${s.id}.frontend`, built.frontend, true),
    };
    for (const field of ["supported_until", "min_app_version", "max_app_version"]) {
      if (s[field]) line[field] = s[field];
    }
    lines.push(line);
  }
  if (lines.length === 0) throw new Error("no line has published images");
  const manifest = { schema: 1, generated: new Date().toISOString(), lines };
  if (source.recommended) {
    if (!lines.some(l => l.id === source.recommended)) throw new Error(`recommended line ${source.recommended} has no images`);
    manifest.recommended = source.recommended;
  }
  return manifest;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const opt = name => {
    const i = args.indexOf(name);
    return i >= 0 ? args[i + 1] : undefined;
  };
  const sourcePath = opt("--source");
  const buildsDir = opt("--builds");
  const out = opt("--out");
  if (!sourcePath || !buildsDir || !out) {
    console.error("usage: render-lines.mjs --source <lines.source.json> --builds <dir> [--previous <lines.json>] --out <lines.json>");
    process.exit(2);
  }
  const source = JSON.parse(readFileSync(sourcePath, "utf8"));
  const builds = {};
  for (const line of source.lines ?? []) {
    const path = join(buildsDir, `${line.id}.json`);
    if (existsSync(path)) builds[line.id] = JSON.parse(readFileSync(path, "utf8"));
  }
  const previousPath = opt("--previous");
  const previous = previousPath && existsSync(previousPath) ? JSON.parse(readFileSync(previousPath, "utf8")) : undefined;
  writeFileSync(out, JSON.stringify(render(source, builds, previous), null, 2) + "\n");
  console.log(`wrote ${out}`);
}

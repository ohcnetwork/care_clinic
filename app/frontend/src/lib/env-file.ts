// A .env file as a list of lines. Comments, blank lines and the order they
// appear in are preserved: the file is round-tripped, not regenerated, so hand
// edits made outside the app survive a save.

export type EnvLine =
  | { kind: "comment" | "blank"; raw: string }
  | { kind: "kv"; key: string; value: string };

const KV_RE = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/;

export const ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function parseEnv(text: string): EnvLine[] {
  const lines = text.split(/\r?\n/);
  if (lines.length && lines[lines.length - 1] === "") lines.pop();
  return lines.map((line): EnvLine => {
    if (line.trim() === "") return { kind: "blank", raw: "" };
    if (line.trimStart().startsWith("#")) return { kind: "comment", raw: line };
    const m = line.match(KV_RE);
    return m ? { kind: "kv", key: m[1], value: m[2] } : { kind: "comment", raw: line };
  });
}

export function serializeEnv(lines: EnvLine[]): string {
  return lines.map((l) => (l.kind === "kv" ? `${l.key}=${l.value}` : l.raw)).join("\n") + "\n";
}

/**
 * The value as the readers see it. compose-go (backend.env) and Vite
 * (frontend.env) both strip one pair of matching quotes (Vite also backticks);
 * double quotes unescape \" and \\. Anything after " #" on a bare value is a
 * comment.
 */
export function unquote(raw: string): string {
  const v = raw.trim();
  if (v.length >= 2 && (v[0] === "'" || v[0] === "`") && v[v.length - 1] === v[0]) return v.slice(1, -1);
  if (v.length >= 2 && v[0] === '"' && v[v.length - 1] === '"') {
    return v.slice(1, -1).replace(/\\(["\\])/g, "$1");
  }
  return v.replace(/\s+#.*$/, "");
}

/** Which reader a file is written for: compose-go or Vite's dotenv. */
export type EnvDialect = "backend" | "frontend";

// Bare when it is plainly safe; otherwise single-quoted, which neither reader
// expands or unescapes, so JSON and URLs survive untouched. A value that itself
// contains a single quote is backtick-quoted for Vite (which never unescapes
// \" inside double quotes) and double-quoted with escapes for compose-go.
export function quote(value: string, dialect: EnvDialect): string {
  if (/^[A-Za-z0-9_./:@+,=-]*$/.test(value)) return value;
  if (!value.includes("'")) return `'${value}'`;
  if (dialect === "frontend" && !value.includes("`")) return `\`${value}\``;
  return `"${value.replace(/[\\"]/g, "\\$&")}"`;
}

/** The parsed value of `key`, or undefined when the file has no such line. */
export function getValue(lines: EnvLine[], key: string): string | undefined {
  let found: string | undefined;
  for (const l of lines) if (l.kind === "kv" && l.key === key) found = unquote(l.value);
  return found;
}

export type EnvChange = { key: string; value: string | undefined };

const ADDED_HEADER = "# --- Added from CARE Clinic settings ---";

/**
 * Applies changes in order: an undefined value drops the key, a present key
 * is overwritten in place (every occurrence, so the last one still wins), and
 * a new key is appended under one shared header at the end of the file.
 */
export function applyChanges(
  lines: EnvLine[],
  changes: EnvChange[],
  dialect: EnvDialect,
): EnvLine[] {
  let out = lines;
  const added: EnvLine[] = [];
  for (const { key, value } of changes) {
    if (value === undefined) {
      out = out.filter((l) => !(l.kind === "kv" && l.key === key));
      continue;
    }
    const raw = quote(value, dialect);
    const line: EnvLine = { kind: "kv", key, value: raw };
    if (out.some((l) => l.kind === "kv" && l.key === key)) {
      out = out.map((l) => (l.kind === "kv" && l.key === key ? line : l));
    } else if (added.some((l) => l.kind === "kv" && l.key === key)) {
      added.splice(added.findIndex((l) => l.kind === "kv" && l.key === key), 1, line);
    } else {
      added.push(line);
    }
  }
  const header = out.findIndex((l) => l.kind === "comment" && l.raw === ADDED_HEADER);
  if (added.length === 0) {
    // Drop the header once nothing is left under it, and the blank above it.
    if (header >= 0 && !out.slice(header + 1).some((l) => l.kind === "kv")) {
      out = out.slice(0, header);
      while (out.length && out[out.length - 1].kind === "blank") out = out.slice(0, -1);
    }
    return out;
  }
  if (header < 0) {
    if (out.length && out[out.length - 1].kind !== "blank") out = [...out, { kind: "blank", raw: "" }];
    out = [...out, { kind: "comment", raw: ADDED_HEADER }];
  }
  return [...out, ...added];
}

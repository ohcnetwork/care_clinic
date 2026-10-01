import { quote, type EnvChange, type EnvLine } from "@/lib/env-file";
import type { Section } from "@/types";
import { normaliseLogo, splitList } from "./env-controls";
import { SETTING_BY_KEY, type Group, type Setting } from "./env-schema";

export type EnvDraft = Record<string, string | undefined>;

type Entry = { key: string; value: string; start: number; end: number };

function entries(text: string): { chunks: string[]; values: Entry[] } {
  const chunks = text.match(/[^\n]*\n|[^\n]+$/g) ?? [];
  const values: Entry[] = [];
  for (let index = 0; index < chunks.length; index++) {
    const match = chunks[index].replace(/\r?\n$/, "").match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/);
    if (!match) continue;
    const start = index;
    let value = match[2];
    const delimiter = value[0];
    if (delimiter === "'" || delimiter === '"' || delimiter === "`") {
      let end = -1;
      for (;;) {
        for (let pos = 1; pos < value.length; pos++) {
          if (value[pos] !== delimiter) continue;
          let slashes = 0;
          for (let before = pos - 1; before >= 0 && value[before] === "\\"; before--) slashes++;
          if (delimiter !== '"' || slashes % 2 === 0) { end = pos; break; }
        }
        if (end >= 0 || index + 1 >= chunks.length) break;
        value += "\n" + chunks[++index].replace(/\r?\n$/, "");
      }
      if (end >= 0 && /^\s*(?:#.*)?$/.test(value.slice(end + 1))) value = value.slice(0, end + 1);
    }
    values.push({ key: match[1], value, start, end: index });
  }
  return { chunks, values };
}

export function editableEnvLines(text: string): EnvLine[] {
  return entries(text).values.map(({ key, value }) => ({ kind: "kv", key, value }));
}

// Keep unedited bytes, including CRLF, comments, spacing and multiline values.
export function mergeEnvChanges(text: string, changes: EnvChange[], file: Section): string {
  const { chunks, values } = entries(text);
  const edits = new Map(changes.map((change) => [change.key, change.value]));
  const starts = new Map(values.map((entry) => [entry.start, entry]));
  const seen = new Set<string>();
  const newline = text.includes("\r\n") ? "\r\n" : "\n";
  let out = "";
  for (let index = 0; index < chunks.length; index++) {
    const entry = starts.get(index);
    if (!entry || !edits.has(entry.key)) {
      out += chunks[index];
      continue;
    }
    const value = edits.get(entry.key);
    seen.add(entry.key);
    if (value !== undefined) {
      const ending = chunks[entry.end].endsWith("\n") ? newline : "";
      out += `${entry.key}=${quote(value, file)}${ending}`;
    }
    index = entry.end;
  }
  const added = changes.filter(({ key, value }) => !seen.has(key) && value !== undefined);
  if (added.length) {
    if (out && !out.endsWith("\n")) out += newline;
    if (!out.includes("# --- Added from CARE Desktop settings ---")) {
      if (out) out += newline;
      out += `# --- Added from CARE Desktop settings ---${newline}`;
    }
    out += added.map(({ key, value }) => `${key}=${quote(value!, file)}${newline}`).join("");
  }
  return out;
}

export function effectiveSetting(setting: Setting, value: string | undefined): string {
  switch (setting.kind) {
    case "radio":
    case "select":
      return value ?? setting.fallback;
    case "multi":
      return value === undefined ? setting.fallback.join(",") : splitList(value).join(",");
    case "logo":
      return value === undefined ? "" : normaliseLogo(value);
    case "secret":
      return value ?? "";
    case "int":
      return (value ?? (setting.fallback === "all" ? "" : setting.fallback) ?? "").trim();
    default:
      return (value ?? "").trim();
  }
}

function isWebAddress(value: string): boolean {
  try {
    const url = new URL(value);
    return (url.protocol === "http:" || url.protocol === "https:") && !!url.hostname && !url.username && !url.password;
  } catch {
    return false;
  }
}

export function validateSetting(setting: Setting, value: string | undefined): string | null {
  const current = effectiveSetting(setting, value);
  if (/[\r\n\0]/.test(current)) return "Keep this value on one line.";
  switch (setting.kind) {
    case "int": {
      if (!current) return setting.required ? "Enter a number." : null;
      if (!/^-?\d+$/.test(current) || !Number.isSafeInteger(Number(current))) return "Enter a whole number.";
      const number = Number(current);
      if (setting.min !== undefined && number < setting.min) return `Must be at least ${setting.min}.`;
      if (setting.max !== undefined && number > setting.max) return `Must be at most ${setting.max}.`;
      return null;
    }
    case "radio":
    case "select":
      return (setting.kind === "select" && setting.none && current === "") || setting.options.some((option) => option.value === current)
        ? null : "Choose one of the listed options.";
    case "multi": {
      const chosen = splitList(current);
      if (!chosen.length) return "Choose at least one.";
      return chosen.every((entry) => setting.options.some((option) => option.value === entry)) ? null : "Remove any unlisted options before saving this setting.";
    }
    case "logo":
      if (!current) return null;
      try {
        const logo: unknown = JSON.parse(current);
        if (!logo || typeof logo !== "object" || Array.isArray(logo)) return "Enter a web address for each logo.";
        const { light, dark } = logo as Record<string, unknown>;
        return typeof light === "string" && typeof dark === "string" && isWebAddress(light) && isWebAddress(dark)
          ? null : "Use an http:// or https:// address without a username or password.";
      } catch {
        return "Enter a web address for each logo.";
      }
    default:
      if (setting.key === "REACT_DEFAULT_COUNTRY" && current && !/^[A-Za-z]{2}$/.test(current)) return "Use a two-letter country code, such as IN.";
      if (setting.key === "EMAIL_HOST" && current && /\s|:\/\/|\//.test(current)) return "Enter the mail server name, without a web address or spaces.";
      if (setting.key === "SNS_REGION" && current && !/^[a-z]{2}(?:-[a-z]+)+-\d+$/.test(current)) return "Enter an Amazon region, such as ap-south-1.";
      return null;
  }
}

export function groupSummary(group: Group, draft: EnvDraft): string {
  const value = (key: string) => {
    const setting = SETTING_BY_KEY.get(key)!;
    return effectiveSetting(setting, draft[key]);
  };
  const choice = (key: string) => {
    const setting = SETTING_BY_KEY.get(key)!;
    if (setting.kind !== "radio" && setting.kind !== "select") return "";
    return setting.options.find((option) => option.value === value(key))?.label ?? "Check current settings";
  };
  switch (group.id) {
    case "backups": {
      const days = Number(value("DB_BACKUP_RETENTION_PERIOD"));
      return !Number.isSafeInteger(days) || days < 0 ? "Check backup retention" : days === 0 ? "Keep backups forever" : `Keep backups for ${days} ${days === 1 ? "day" : "days"}`;
    }
    case "signin":
      return `Idle sign-out after ${value("JWT_REFRESH_TOKEN_LIFETIME")} min · repeated wrong passwords ${value("DISABLE_RATELIMIT") === "False" ? "slowed down" : value("DISABLE_RATELIMIT") === "True" ? "not slowed down" : "need checking"}`;
    case "patients":
      return `Patient sign-in: ${choice("REACT_DISABLE_PATIENT_LOGIN") === "No" ? "Off" : choice("REACT_DISABLE_PATIENT_LOGIN") === "Yes" ? "On" : "check settings"} · SMS: ${choice("USE_SMS")}`;
    case "email":
      return value("EMAIL_HOST") ? "Mail server set · check the settings before sending" : "Mail server not set";
    case "branding":
      return value("REACT_APP_TITLE") || "Default CARE branding";
    case "region": {
      const locales = SETTING_BY_KEY.get("REACT_ALLOWED_LOCALES")!;
      const names = splitList(value(locales.key)).map((entry) => locales.kind === "multi"
        ? locales.options.find((option) => option.value === entry)?.label ?? entry : entry);
      return [names.join(", "), value("REACT_DEFAULT_COUNTRY_NAME") || value("REACT_DEFAULT_COUNTRY")].filter(Boolean).join(" · ");
    }
    case "visits":
      return `${splitList(value("REACT_ALLOWED_ENCOUNTER_CLASSES")).length} kinds of visit`;
    case "registration":
      return `Quick registration: ${choice("REACT_ENABLE_MINIMAL_PATIENT_REGISTRATION")}`;
    case "billing":
      return `Entered prices include tax: ${choice("REACT_INVENTORY_DEFAULT_TAX_INCLUSIVE")}`;
    case "screens":
      return `Form drafts: ${choice("REACT_ENABLE_QUESTIONNAIRE_DRAFT")}`;
    default:
      return group.summary;
  }
}

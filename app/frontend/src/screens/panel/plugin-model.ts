import type { CarePlugin, PluginCatalogEntry, PluginSetting } from "@/types";

export type ConfigRow = {
  key: string;
  value: string;
  original?: { text: string; value: unknown };
  text?: boolean;
};
export type BackendDraft = { name: string; package_name: string; version: string; configs: ConfigRow[] };
export type FrontendDraft = { slug: string; url: string; metaText: string };
export type PluginRow = {
  uid: number;
  id: string;
  label: string;
  catalog: boolean;
  backend: BackendDraft | null;
  frontend: FrontendDraft | null;
  inactiveBackend?: BackendDraft;
  inactiveFrontend?: FrontendDraft;
  removed?: boolean;
};

export const emptyBackend = (): BackendDraft => ({ name: "", package_name: "", version: "@main", configs: [] });
export const emptyFrontend = (): FrontendDraft => ({ slug: "", url: "", metaText: "{}" });

function configText(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}

function parseConfigValue(raw: string): unknown {
  const text = raw.trim();
  if (text === "true") return true;
  if (text === "false") return false;
  if (/^-?\d+$/.test(text) || /^-?\d*\.\d+$/.test(text)) {
    const value = Number(text);
    return Number.isFinite(value) ? value : raw;
  }
  if (/^[[{]/.test(text)) {
    try {
      return JSON.parse(text, finiteNumber);
    } catch {
      // Backend settings also accept ordinary text, including text starting with a bracket.
      return raw;
    }
  }
  return raw;
}

function finiteNumber(_key: string, value: unknown): unknown {
  if (typeof value === "number" && !Number.isFinite(value)) throw new Error("Number is out of range.");
  return value;
}

function parseMeta(text: string): Record<string, unknown> {
  if (text.trim() === "") return {};
  const value: unknown = JSON.parse(text, finiteNumber);
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Settings must be a JSON object.");
  }
  return value as Record<string, unknown>;
}

export function toPluginRow(plugin: CarePlugin, uid: number): PluginRow {
  return {
    uid,
    id: plugin.id ?? "",
    label: plugin.label ?? "",
    catalog: Boolean(plugin.catalog),
    backend: plugin.backend ? {
      name: plugin.backend.name ?? "",
      package_name: plugin.backend.package_name ?? "",
      version: plugin.backend.version ?? "",
      configs: Object.entries(plugin.backend.configs ?? {}).map(([key, value]) => {
        const text = configText(value);
        return { key, value: text, original: { text, value } };
      }),
    } : null,
    frontend: plugin.frontend ? {
      slug: plugin.frontend.slug ?? "",
      url: plugin.frontend.url ?? "",
      metaText: JSON.stringify(plugin.frontend.meta ?? {}, null, 2),
    } : null,
  };
}

// Match plugins.Prepare: catalog sources win, operator settings survive, and
// entries no longer in the catalog become editable custom plugins.
export function reconcileCatalog(row: PluginRow, catalog: PluginCatalogEntry[]): PluginRow {
  if (!row.catalog) return row;
  const entry = catalog.find((entry) => entry.plugin.id === row.id.trim());
  if (!entry) return { ...row, catalog: false };
  const current = toPluginRow(entry.plugin, row.uid);
  const configs = row.backend?.configs ?? current.backend?.configs ?? [];
  const missing = (entry.required_configs ?? [])
    .filter((key) => !configs.some((config) => config.key.trim() === key))
    .map((key) => ({ key, value: "" }));
  return {
    ...current,
    catalog: true,
    backend: current.backend ? {
      ...current.backend, configs: missing.length ? [...configs, ...missing] : configs,
    } : null,
    frontend: current.frontend ? {
      ...current.frontend, metaText: row.frontend?.metaText ?? current.frontend.metaText,
    } : null,
  };
}

export function pluginSnapshot(rows: PluginRow[]): string {
  return JSON.stringify(rows.filter((row) => !row.removed).map((row) => ({
    id: row.id, label: row.label, catalog: row.catalog,
    backend: row.backend ? {
      ...row.backend, configs: row.backend.configs.map(({ key, value }) => ({ key, value })),
    } : null,
    frontend: row.frontend,
  })));
}

export function serializePlugins(rows: PluginRow[]): CarePlugin[] {
  return rows.filter((row) => !row.removed).map((row) => {
    const plugin: CarePlugin = { id: row.id.trim() };
    if (row.label.trim()) plugin.label = row.label.trim();
    if (row.catalog) plugin.catalog = true;
    if (row.backend) {
      const configs = Object.fromEntries(row.backend.configs.map((config) => [
        config.key.trim(),
        config.original && config.value === config.original.text
          ? config.original.value : config.text ? config.value : parseConfigValue(config.value),
      ]));
      plugin.backend = {
        name: row.backend.name.trim(),
        package_name: row.backend.package_name.trim(),
      };
      if (row.backend.version.trim()) plugin.backend.version = row.backend.version.trim();
      if (Object.keys(configs).length) plugin.backend.configs = configs;
    }
    if (row.frontend) {
      const meta = parseMeta(row.frontend.metaText);
      plugin.frontend = { slug: row.frontend.slug.trim(), url: row.frontend.url.trim() };
      if (Object.keys(meta).length) plugin.frontend.meta = meta;
    }
    return plugin;
  });
}

export function catalogConflicts(row: PluginRow, plugin: CarePlugin): boolean {
  return row.id.trim() === plugin.id ||
    Boolean(row.backend && plugin.backend && row.backend.name.trim() === plugin.backend.name) ||
    Boolean(row.frontend && plugin.frontend && row.frontend.slug.trim() === plugin.frontend.slug);
}

function isFrontendURL(text: string): boolean {
  const value = text.trim();
  const authority = /^https?:\/\/([^/?#]*)/i.exec(value)?.[1];
  if (!authority || authority.includes("\\") || /[\u0000-\u001f\u007f]/.test(value)) return false;
  try {
    const parsed = new URL(value);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.host !== "";
  } catch {
    return false;
  }
}

function metaProblem(text: string): string | undefined {
  let meta: Record<string, unknown>;
  try {
    meta = parseMeta(text);
  } catch {
    return "Enter a valid JSON object for the frontend settings.";
  }
  return Object.prototype.hasOwnProperty.call(meta, "url")
    ? "Set the frontend URL in its own field, not in these settings." : undefined;
}

export function requiredConfig(row: PluginRow, catalog: PluginCatalogEntry[], index: number): boolean {
  if (!row.catalog || !row.backend) return false;
  const required = catalog.find((entry) => entry.plugin.id === row.id.trim())?.required_configs ?? [];
  const key = row.backend.configs[index]?.key.trim() ?? "";
  return required.includes(key) && row.backend.configs.findIndex((config) => config.key.trim() === key) === index;
}

export function secretConfig(key: string): boolean {
  return /(_API_KEY|_SECRET|_TOKEN)$/.test(key.trim());
}

export function pluginProblems(row: PluginRow, rows: PluginRow[], catalog: PluginCatalogEntry[] = []) {
  const id = row.id.trim();
  const module = row.backend?.name.trim() ?? "";
  const slug = row.frontend?.slug.trim() ?? "";
  const others = rows.filter((other) => other.uid !== row.uid && !other.removed);
  const configs = row.backend?.configs.map((config, index, all) => {
    const key = config.key.trim();
    if (!key) return "Enter a setting name, or remove this setting.";
    if (all.some((other, otherIndex) => otherIndex !== index && other.key.trim() === key)) {
      return "This setting name is already in use.";
    }
    return undefined;
  }) ?? [];
  const name = row.label.trim() || id;
  if (row.removed) {
    const none = configs.map(() => undefined);
    return {
      id: undefined, parts: undefined, module: undefined, package: undefined, version: undefined,
      slug: undefined, url: undefined, meta: undefined, configs: none, configValues: none,
    };
  }
  const configValues = row.backend?.configs.map((config, index) =>
    requiredConfig(row, catalog, index) && !config.value.trim()
      ? `Enter a value; ${name} needs this setting.` : undefined) ?? [];

  return {
    id: !id ? "Enter a plugin ID."
      : !/^[A-Za-z0-9_.-]+$/.test(id) ? "Use letters, numbers, '.', '_' or '-'. Spaces belong in the display name."
        : others.some((other) => other.id.trim() === id) ? "This plugin ID is already in use." : undefined,
    parts: !row.backend && !row.frontend ? "Choose Backend, Frontend, or both." : undefined,
    module: row.backend && !/^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$/.test(module)
      ? "Enter a Python module name, such as care_example."
      : row.backend && others.some((other) => other.backend?.name.trim() === module)
        ? "This backend module is already in use." : undefined,
    package: row.backend && (!row.backend.package_name.trim() || /[ \t\r\n]/.test(row.backend.package_name.trim()))
      ? "Enter a pip source without spaces." : undefined,
    version: row.backend?.version.trim() && !/^[@=<>~!]/.test(row.backend.version.trim())
      ? "Start with @ (such as @main) or a pip operator (such as ==1.2)." : undefined,
    slug: row.frontend && !/^[A-Za-z0-9_-]+$/.test(slug)
      ? "Use letters, numbers, '_' or '-' for the frontend name."
      : row.frontend && others.some((other) => other.frontend?.slug.trim() === slug)
        ? "This frontend name is already in use." : undefined,
    url: row.frontend && !isFrontendURL(row.frontend.url)
      ? "Enter a full http:// or https:// URL with a host." : undefined,
    meta: row.frontend ? metaProblem(row.frontend.metaText) : undefined,
    configs,
    configValues,
  };
}

export function hasPluginProblems(problems: ReturnType<typeof pluginProblems>): boolean {
  return Object.values(problems).some((problem) => Array.isArray(problem) ? problem.some(Boolean) : Boolean(problem));
}

export function catalogEntry(row: PluginRow, catalog: PluginCatalogEntry[]): PluginCatalogEntry | undefined {
  return row.catalog ? catalog.find((entry) => entry.plugin.id === row.id.trim()) : undefined;
}

export function describedSettings(row: PluginRow, catalog: PluginCatalogEntry[]): PluginSetting[] {
  return (catalogEntry(row, catalog)?.settings ?? []).filter((setting) =>
    setting.part === "backend" ? row.backend !== null : row.frontend !== null);
}

export function isDescribed(row: PluginRow, catalog: PluginCatalogEntry[], key: string): boolean {
  return describedSettings(row, catalog).some((setting) => setting.part === "backend" && setting.key === key.trim());
}

export function readMeta(row: PluginRow): Record<string, unknown> | null {
  if (!row.frontend) return null;
  try {
    return parseMeta(row.frontend.metaText);
  } catch {
    return null;
  }
}

export function settingValue(row: PluginRow, setting: PluginSetting): unknown {
  if (setting.part === "backend") {
    const config = row.backend?.configs.find((config) => config.key.trim() === setting.key);
    if (!config) return undefined;
    if (config.original && config.value === config.original.text) return config.original.value;
    return config.text ? config.value : parseConfigValue(config.value);
  }
  let node: unknown = readMeta(row);
  for (const part of setting.key.split(".")) {
    if (node === null || typeof node !== "object" || Array.isArray(node)) return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return node;
}

function settingText(value: unknown): { value: string; text: boolean } {
  if (typeof value === "string") return { value, text: true };
  return { value: JSON.stringify(value ?? null), text: false };
}

export function withSetting(row: PluginRow, setting: PluginSetting, value: unknown): PluginRow {
  if (setting.part === "backend") {
    if (!row.backend) return row;
    const next = settingText(value);
    const index = row.backend.configs.findIndex((config) => config.key.trim() === setting.key);
    const configs = index < 0
      ? [...row.backend.configs, { key: setting.key, ...next }]
      : row.backend.configs.map((config, i) => i === index ? { ...config, ...next } : config);
    return { ...row, backend: { ...row.backend, configs } };
  }
  const meta = readMeta(row);
  if (!row.frontend || !meta) return row;
  const parts = setting.key.split(".");
  const root: Record<string, unknown> = { ...meta };
  let node = root;
  for (const part of parts.slice(0, -1)) {
    const child = node[part];
    const copy = child !== null && typeof child === "object" && !Array.isArray(child)
      ? { ...(child as Record<string, unknown>) } : {};
    node[part] = copy;
    node = copy;
  }
  node[parts[parts.length - 1]] = value;
  return { ...row, frontend: { ...row.frontend, metaText: JSON.stringify(root, null, 2) } };
}

export type PluginChanges = { added: PluginRow[]; removed: PluginRow[]; changed: PluginRow[] };

export function pluginChanges(rows: PluginRow[], applied: PluginRow[]): PluginChanges {
  const changes: PluginChanges = { added: [], removed: [], changed: [] };
  for (const row of rows) {
    const before = applied.find((saved) => saved.uid === row.uid);
    if (!before) {
      if (!row.removed) changes.added.push(row);
    } else if (row.removed) {
      changes.removed.push(row);
    } else if (pluginSnapshot([row]) !== pluginSnapshot([before])) {
      changes.changed.push(row);
    }
  }
  return changes;
}

export function pluginName(row: PluginRow): string {
  return row.label.trim() || row.id.trim() || "New plugin";
}

function names(rows: PluginRow[]): string {
  const list = rows.map(pluginName);
  return list.length <= 2 ? list.join(" and ") : `${list.slice(0, -1).join(", ")} and ${list[list.length - 1]}`;
}

export function describeChanges(changes: PluginChanges): string {
  const parts = [
    changes.added.length ? `add ${names(changes.added)}` : "",
    changes.removed.length ? `remove ${names(changes.removed)}` : "",
    changes.changed.length ? `change ${names(changes.changed)}` : "",
  ].filter(Boolean);
  if (!parts.length) return "";
  const text = parts.length <= 2 ? parts.join(" and ") : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
  return text[0].toUpperCase() + text.slice(1) + ".";
}

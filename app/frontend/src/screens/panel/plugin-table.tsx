import { ChevronDown } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import { Switch } from "@/components/ui/switch";
import { bridge } from "@/lib/bridge";
import { errorText, firstLine } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";
import type { CarePlugin, PluginCatalogEntry } from "@/types";

type ConfigRow = { key: string; value: string };
type BackendDraft = { name: string; package_name: string; version: string; configs: ConfigRow[] };
type FrontendDraft = { slug: string; url: string; metaText: string };
type Row = {
  uid: number;
  id: string;
  label: string;
  catalog: boolean;
  backend: BackendDraft | null;
  frontend: FrontendDraft | null;
};

const CUSTOM = "__custom__";

function configText(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}

function parseConfigValue(raw: string): unknown {
  const t = raw.trim();
  if (t === "true") return true;
  if (t === "false") return false;
  if (/^-?\d+$/.test(t)) return parseInt(t, 10);
  if (/^-?\d*\.\d+$/.test(t)) return parseFloat(t);
  if (/^[[{]/.test(t)) {
    try {
      return JSON.parse(t);
    } catch {
      return raw;
    }
  }
  return raw;
}

function parseMeta(text: string): Record<string, unknown> {
  if (text.trim() === "") return {};
  const value: unknown = JSON.parse(text);
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("must be a JSON object");
  }
  return value as Record<string, unknown>;
}

function metaProblem(text: string): string | null {
  try {
    parseMeta(text);
    return null;
  } catch (e) {
    return errorText(e);
  }
}

function toDraft(p: CarePlugin, uid: number): Row {
  return {
    uid,
    id: p.id ?? "",
    label: p.label ?? "",
    catalog: Boolean(p.catalog),
    backend: p.backend
      ? {
          name: p.backend.name ?? "",
          package_name: p.backend.package_name ?? "",
          version: p.backend.version ?? "",
          configs: Object.entries(p.backend.configs ?? {}).map(([key, value]) => ({
            key,
            value: configText(value),
          })),
        }
      : null,
    frontend: p.frontend
      ? {
          slug: p.frontend.slug ?? "",
          url: p.frontend.url ?? "",
          metaText: JSON.stringify(p.frontend.meta ?? {}, null, 2),
        }
      : null,
  };
}

function serialize(rows: Row[]): CarePlugin[] {
  return rows.map((r) => {
    const out: CarePlugin = { id: r.id.trim() };
    if (r.label.trim()) out.label = r.label.trim();
    if (r.catalog) out.catalog = true;
    if (r.backend) {
      const configs: Record<string, unknown> = {};
      for (const c of r.backend.configs) {
        if (c.key.trim() !== "") configs[c.key.trim()] = parseConfigValue(c.value);
      }
      out.backend = {
        name: r.backend.name.trim(),
        package_name: r.backend.package_name.trim(),
      };
      if (r.backend.version.trim()) out.backend.version = r.backend.version.trim();
      if (Object.keys(configs).length) out.backend.configs = configs;
    }
    if (r.frontend) {
      const meta = parseMeta(r.frontend.metaText);
      out.frontend = { slug: r.frontend.slug.trim(), url: r.frontend.url.trim() };
      if (Object.keys(meta).length) out.frontend.meta = meta;
    }
    return out;
  });
}

const emptyBackend = (): BackendDraft => ({ name: "", package_name: "", version: "@main", configs: [] });
const emptyFrontend = (): FrontendDraft => ({ slug: "", url: "", metaText: "{}" });

export function PluginTable() {
  const { busy, runAction, log } = useCare();
  const [rows, setRows] = useState<Row[]>([]);
  const [catalog, setCatalog] = useState<PluginCatalogEntry[]>([]);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set());
  const nextUid = useRef(0);
  const [problem, setProblem] = useState<string | null>(null);

  const take = () => nextUid.current++;

  const load = useCallback(async () => {
    setExpanded(new Set());
    setProblem(null);
    try {
      const [saved, available] = await Promise.all([
        bridge.ReadPlugins(),
        bridge.PluginCatalog(),
      ]);
      setCatalog(available);
      setRows(saved.map((p) => toDraft(p, nextUid.current++)));
      setProblem("");
    } catch (e) {
      setRows([]);
      setProblem(errorText(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const describe = useMemo(
    () => new Map(catalog.map((c) => [c.plugin.id, c.description ?? ""])),
    [catalog],
  );
  const available = catalog.filter((c) => !rows.some((r) => r.catalog && r.id === c.plugin.id));
  const metaErrors = rows.filter((r) => r.frontend && metaProblem(r.frontend.metaText));

  const patchRow = (uid: number, values: Partial<Row>) =>
    setRows((prev) => prev.map((r) => (r.uid === uid ? { ...r, ...values } : r)));

  const patchBackend = (uid: number, values: Partial<BackendDraft>) =>
    setRows((prev) =>
      prev.map((r) => (r.uid === uid && r.backend ? { ...r, backend: { ...r.backend, ...values } } : r)),
    );

  const patchFrontend = (uid: number, values: Partial<FrontendDraft>) =>
    setRows((prev) =>
      prev.map((r) =>
        r.uid === uid && r.frontend ? { ...r, frontend: { ...r.frontend, ...values } } : r,
      ),
    );

  const patchConfig = (uid: number, index: number, values: Partial<ConfigRow>) =>
    setRows((prev) =>
      prev.map((r) =>
        r.uid === uid && r.backend
          ? {
              ...r,
              backend: {
                ...r.backend,
                configs: r.backend.configs.map((c, i) => (i === index ? { ...c, ...values } : c)),
              },
            }
          : r,
      ),
    );

  const toggleExpanded = (uid: number) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });

  const add = (value: string) => {
    const uid = take();
    if (value === CUSTOM) {
      setRows((prev) => [
        ...prev,
        { uid, id: "", label: "", catalog: false, backend: emptyBackend(), frontend: emptyFrontend() },
      ]);
    } else {
      const entry = catalog.find((c) => c.plugin.id === value);
      if (!entry) return;
      setRows((prev) => [...prev, toDraft({ ...entry.plugin, catalog: true }, uid)]);
    }
    setExpanded((prev) => new Set(prev).add(uid));
  };

  const save = async () => {
    if (busy || problem !== "" || metaErrors.length) return;
    try {
      await bridge.SavePlugins(serialize(rows));
      toast("Applying plugins");
      await runAction("apply-plugins");
    } catch (e) {
      log(`error saving plugins: ${errorText(e)}`);
      toast(firstLine(errorText(e)));
    }
  };

  return (
    <>
      {problem ? (
        <Alert variant="danger">
          <span>{problem}</span>
          <Button disabled={busy} onClick={() => void load()}>Retry</Button>
        </Alert>
      ) : null}

      <div className="overflow-hidden rounded-lg border border-line">
        {rows.length === 0 ? (
          <div className="p-5 text-center text-[13px] text-faint">No plugins yet</div>
        ) : null}

        {rows.map((row, i) => {
          const open = expanded.has(row.uid);
          const description = row.catalog ? describe.get(row.id) : "";
          return (
            <div key={row.uid} className={cn(i > 0 && "border-t border-hair")}>
              <div className="flex items-center gap-2.5 p-3">
                <button
                  type="button"
                  onClick={() => toggleExpanded(row.uid)}
                  className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 text-left"
                >
                  <ChevronDown
                    className={cn("size-[14px] flex-none transition-transform", open && "rotate-180")}
                    strokeWidth={2.5}
                  />
                  <span className="min-w-0">
                    <span className="block truncate text-[13.5px] font-semibold text-ink">
                      {row.label || row.id || "New plugin"}
                    </span>
                    {description ? (
                      <span className="block truncate text-[12.5px] text-muted-foreground">
                        {description}
                      </span>
                    ) : null}
                  </span>
                </button>
                {row.backend ? <Badge size="sm" variant="plain">Backend</Badge> : null}
                {row.frontend ? <Badge size="sm" variant="plain">Frontend</Badge> : null}
                {!row.catalog ? <Badge size="sm">Custom</Badge> : null}
                <Button
                  size="icon"
                  title="remove plugin"
                  className="border-danger-line text-danger-ink hover:border-danger-line hover:bg-danger-bg hover:text-danger-ink"
                  onClick={() => setRows((prev) => prev.filter((r) => r.uid !== row.uid))}
                >
                  ×
                </Button>
              </div>

              {open ? (
                <div className="flex flex-col gap-4 px-3 pb-4">
                  {!row.catalog ? (
                    <div className="flex flex-wrap items-center gap-4">
                      <Field label="Name" className="w-[220px]">
                        <Input
                          className="h-9 font-mono text-[13px]"
                          spellCheck={false}
                          placeholder="care_example"
                          value={row.id}
                          onChange={(e) => patchRow(row.uid, { id: e.target.value })}
                        />
                      </Field>
                      <PartSwitch
                        label="Backend"
                        checked={row.backend !== null}
                        onChange={(on) => patchRow(row.uid, { backend: on ? emptyBackend() : null })}
                      />
                      <PartSwitch
                        label="Frontend"
                        checked={row.frontend !== null}
                        onChange={(on) => patchRow(row.uid, { frontend: on ? emptyFrontend() : null })}
                      />
                    </div>
                  ) : null}

                  {row.backend ? (
                    <Section title="Backend" hint="Installed into the CARE server; changing it rebuilds the backend.">
                      {!row.catalog ? (
                        <div className="flex flex-wrap gap-2.5">
                          <Field label="Python module" className="w-[190px]">
                            <Input
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="care_example"
                              value={row.backend.name}
                              onChange={(e) => patchBackend(row.uid, { name: e.target.value })}
                            />
                          </Field>
                          <Field label="pip source" className="min-w-[240px] flex-1">
                            <Input
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="git+https://github.com/org/repo.git"
                              value={row.backend.package_name}
                              onChange={(e) => patchBackend(row.uid, { package_name: e.target.value })}
                            />
                          </Field>
                          <Field label="Version" className="w-[110px]">
                            <Input
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="@main"
                              value={row.backend.version}
                              onChange={(e) => patchBackend(row.uid, { version: e.target.value })}
                            />
                          </Field>
                        </div>
                      ) : null}
                      <div className="flex flex-col gap-[7px]">
                        {row.backend.configs.map((config, ci) => (
                          <div key={ci} className="flex items-center gap-2.5">
                            <Input
                              className="h-9 w-[250px] flex-none rounded-[8px] px-[11px] font-mono text-[13px]"
                              placeholder="SETTING_NAME"
                              spellCheck={false}
                              value={config.key}
                              onChange={(e) => patchConfig(row.uid, ci, { key: e.target.value })}
                            />
                            <Input
                              className="h-9 flex-1 rounded-[8px] px-[11px] text-[13px]"
                              placeholder="value"
                              spellCheck={false}
                              value={config.value}
                              onChange={(e) => patchConfig(row.uid, ci, { value: e.target.value })}
                            />
                            <Button
                              size="icon"
                              className="border-danger-line text-danger-ink hover:border-danger-line hover:bg-danger-bg hover:text-danger-ink"
                              onClick={() =>
                                patchBackend(row.uid, {
                                  configs: row.backend!.configs.filter((_, i2) => i2 !== ci),
                                })
                              }
                            >
                              ×
                            </Button>
                          </div>
                        ))}
                        <Button
                          className="self-start"
                          onClick={() =>
                            patchBackend(row.uid, {
                              configs: [...row.backend!.configs, { key: "", value: "" }],
                            })
                          }
                        >
                          Add setting
                        </Button>
                      </div>
                    </Section>
                  ) : null}

                  {row.frontend ? (
                    <Section
                      title="Frontend"
                      hint="Loaded by staff browsers from its URL; no rebuild needed. Settings are the plugin's JSON config."
                    >
                      {!row.catalog ? (
                        <div className="flex flex-wrap gap-2.5">
                          <Field label="Frontend name" className="w-[190px]">
                            <Input
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="care_example_fe"
                              value={row.frontend.slug}
                              onChange={(e) => patchFrontend(row.uid, { slug: e.target.value })}
                            />
                          </Field>
                          <Field label="remoteEntry.js URL" className="min-w-[240px] flex-1">
                            <Input
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="https://org.github.io/repo/assets/remoteEntry.js"
                              value={row.frontend.url}
                              onChange={(e) => patchFrontend(row.uid, { url: e.target.value })}
                            />
                          </Field>
                        </div>
                      ) : null}
                      <textarea
                        className={cn(
                          "min-h-[110px] w-full rounded-[8px] border border-line bg-white px-[11px] py-2 font-mono text-[12.5px] text-ink outline-none focus-visible:border-brand",
                          metaProblem(row.frontend.metaText) && "border-danger-line",
                        )}
                        spellCheck={false}
                        value={row.frontend.metaText}
                        onChange={(e) => patchFrontend(row.uid, { metaText: e.target.value })}
                      />
                      {metaProblem(row.frontend.metaText) ? (
                        <span className="text-[12.5px] text-danger-ink">
                          Settings {metaProblem(row.frontend.metaText)}
                        </span>
                      ) : null}
                    </Section>
                  ) : null}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>

      <div className="flex items-center gap-2.5">
        <Select value="" disabled={busy || problem !== ""} onValueChange={add}>
          <SelectTrigger className="w-auto min-w-[190px]" aria-label="Add a plugin">
            <SelectValue placeholder="Add a plugin" />
          </SelectTrigger>
          <SelectContent className="w-auto">
            {available.map((entry) => (
              <SelectItem key={entry.plugin.id} value={entry.plugin.id}>
                {entry.plugin.label || entry.plugin.id}
              </SelectItem>
            ))}
            <SelectItem value={CUSTOM}>Custom plugin</SelectItem>
          </SelectContent>
        </Select>
        <span className="flex-1" />
        <Button
          variant="primary"
          disabled={busy || problem !== "" || metaErrors.length > 0}
          onClick={() => void save()}
        >
          Save and apply
        </Button>
      </div>
    </>
  );
}

function Field({
  label,
  className,
  children,
}: {
  label: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <label className={cn("flex flex-col gap-1", className)}>
      <span className="text-[11.5px] font-bold tracking-[0.05em] text-faint uppercase">{label}</span>
      {children}
    </label>
  );
}

function PartSwitch({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-center gap-2 self-end pb-2 text-[13px] font-semibold text-ink">
      <Switch checked={checked} onCheckedChange={onChange} />
      {label}
    </label>
  );
}

function Section({
  title,
  hint,
  children,
}: {
  title: string;
  hint: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2.5 rounded-lg border border-hair p-3">
      <div>
        <div className="text-[13px] font-semibold text-ink">{title}</div>
        <div className="text-[12.5px] text-muted-foreground">{hint}</div>
      </div>
      {children}
    </div>
  );
}

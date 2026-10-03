import { ChevronDown } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import { Alert } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
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
import { Switch } from "@/components/ui/switch";
import { bridge, onCareEvent } from "@/lib/bridge";
import { operationError as describeOperationError } from "@/lib/operation-errors";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";
import type { CarePlugin, PluginCatalogEntry } from "@/types";

import {
  catalogConflicts, emptyBackend, emptyFrontend, hasPluginProblems, pluginProblems,
  pluginSnapshot, reconcileCatalog, serializePlugins, toPluginRow,
  type BackendDraft, type ConfigRow, type FrontendDraft, type PluginRow,
} from "./plugin-model";
import { PanelLogButton } from "./panel-ui";

import "./plugin-table.css";

const CUSTOM = "__custom__";
type SavePhase = "idle" | "saving" | "applying" | "save-error" | "apply-error" | "finished";

export function PluginTable({ disabled = false }: { disabled?: boolean }) {
  const { busy, tab, flow, restorePending, pluginRecoveryPending, runAction, log, operationError, clearOperationError } = useCare();
  const [rows, setRows] = useState<PluginRow[]>([]);
  const [catalog, setCatalog] = useState<PluginCatalogEntry[]>([]);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set());
  const [loaded, setLoaded] = useState(false);
  const [catalogLoaded, setCatalogLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadErrors, setLoadErrors] = useState({ saved: false, catalog: false });
  const [baseline, setBaseline] = useState("");
  const [phase, setPhase] = useState<SavePhase>("idle");
  const [saveProblem, setSaveProblem] = useState("");
  const [needsApply, setNeedsApply] = useState(false);
  const [removing, setRemoving] = useState<PluginRow | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const nextUid = useRef(0);
  const mounted = useRef(false);
  const attemptedLoad = useRef(false);
  const reading = useRef(false);
  const saving = useRef(false);
  const applying = useRef(false);
  const submittedRows = useRef<PluginRow[]>([]);
  const appliedRows = useRef<PluginRow[]>([]);
  const initialRows = useRef<PluginRow[]>([]);
  const sources = useRef<{ saved: CarePlugin[] | null; catalog: PluginCatalogEntry[] | null }>({ saved: null, catalog: null });
  const savedUIDs = useRef<ReadonlySet<number>>(new Set());
  const opener = useRef<HTMLButtonElement | null>(null);
  const addButton = useRef<HTMLButtonElement | null>(null);
  const root = useRef<HTMLDivElement | null>(null);
  const focusRow = useRef<number | null>(null);
  const active = tab === "plugins";
  const activeRef = useRef(active);
  activeRef.current = active;

  const working = phase === "saving" || phase === "applying";
  const ready = loaded && catalogLoaded;
  const locked = disabled || busy || restorePending || pluginRecoveryPending || flow !== "panel" || !ready || loading || working;
  const lockedRef = useRef(locked);
  lockedRef.current = locked;
  const dirty = loaded && pluginSnapshot(rows) !== baseline;

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    if (!active) {
      setRemoving(null);
      focusRow.current = null;
    }
    if (!active || locked) setPickerOpen(false);
  }, [active, locked]);

  useEffect(() => {
    if (active || phase !== "apply-error") return;
    setRows(appliedRows.current);
    setExpanded(new Set());
    setNeedsApply(false);
    setSaveProblem("");
    setPhase("idle");
    submittedRows.current = [];
  }, [active, phase]);

  const load = useCallback(async () => {
    if (reading.current || (sources.current.saved && sources.current.catalog)) return;
    reading.current = true;
    attemptedLoad.current = true;
    setLoading(true);
    setLoadErrors({ saved: false, catalog: false });
    try {
      const [saved, available] = await Promise.allSettled([
        sources.current.saved ?? bridge.ReadPlugins(),
        sources.current.catalog ?? bridge.PluginCatalog(),
      ]);
      if (!mounted.current) return;
      const savedOK = saved.status === "fulfilled" && Array.isArray(saved.value);
      const catalogOK = available.status === "fulfilled" && Array.isArray(available.value);
      if (savedOK) {
        if (sources.current.saved === null) {
          initialRows.current = saved.value.map((plugin) => toPluginRow(plugin, nextUid.current++));
        }
        sources.current.saved = saved.value;
        setLoaded(true);
      } else {
        log("plugins: the saved plugin settings could not be read");
      }
      if (catalogOK) {
        sources.current.catalog = available.value;
        setCatalog(available.value);
        setCatalogLoaded(true);
      } else {
        log("plugins: the bundled plugin catalog could not be read");
      }
      if (sources.current.saved) {
        const next = initialRows.current.map((row) => sources.current.catalog
          ? reconcileCatalog(row, sources.current.catalog) : row);
        appliedRows.current = next;
        setRows(next);
        setBaseline(pluginSnapshot(next));
        savedUIDs.current = new Set(next.map((row) => row.uid));
      }
      setLoadErrors({ saved: !savedOK, catalog: !catalogOK });
    } catch {
      log("plugins: the plugin list could not be opened");
      if (mounted.current) setLoadErrors({ saved: true, catalog: true });
    } finally {
      reading.current = false;
      if (mounted.current) setLoading(false);
    }
  }, [log]);

  useEffect(() => {
    if (tab === "plugins" && flow === "panel" && !busy && !disabled && !attemptedLoad.current) void load();
  }, [tab, flow, busy, disabled, load]);

  useEffect(() => onCareEvent("care-done", (code: number, action?: string) => {
    if (action !== "apply-plugins" || !applying.current) return;
    applying.current = false;
    saving.current = false;
    setNeedsApply(code !== 0);
    if (code === 0) {
      appliedRows.current = submittedRows.current;
      setBaseline(pluginSnapshot(submittedRows.current));
      savedUIDs.current = new Set(submittedRows.current.map((row) => row.uid));
    }
    setPhase(code === 0 ? "finished" : "apply-error");
  }), []);

  const describe = useMemo(
    () => new Map(catalog.map((c) => [c.plugin.id, c.description ?? ""])),
    [catalog],
  );
  const available = catalog.filter((entry) => !rows.some((row) => catalogConflicts(row, entry.plugin)));
  const problems = useMemo(() => new Map(rows.map((row) => [row.uid, pluginProblems(row, rows)])), [rows]);
  const invalid = [...problems.values()].some(hasPluginProblems);

  const changeRows = (change: (current: PluginRow[]) => PluginRow[]) => {
    if (lockedRef.current || saving.current) return;
    setRows(change);
    setPhase((current) => current === "finished" ? "idle" : current);
  };

  const patchRow = (uid: number, values: Partial<PluginRow>) =>
    changeRows((prev) => prev.map((r) => (r.uid === uid ? { ...r, ...values } : r)));

  const patchBackend = (uid: number, values: Partial<BackendDraft>) =>
    changeRows((prev) =>
      prev.map((r) => (r.uid === uid && r.backend ? { ...r, backend: { ...r.backend, ...values } } : r)),
    );

  const patchFrontend = (uid: number, values: Partial<FrontendDraft>) =>
    changeRows((prev) =>
      prev.map((r) =>
        r.uid === uid && r.frontend ? { ...r, frontend: { ...r.frontend, ...values } } : r,
      ),
    );

  const patchConfig = (uid: number, index: number, values: Partial<ConfigRow>) =>
    changeRows((prev) =>
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
    if (lockedRef.current || saving.current) return;
    const uid = nextUid.current++;
    if (value === CUSTOM) {
      changeRows((prev) => [
        ...prev,
        { uid, id: "", label: "", catalog: false, backend: emptyBackend(), frontend: emptyFrontend() },
      ]);
    } else {
      const entry = catalog.find((c) => c.plugin.id === value);
      if (!entry) return;
      changeRows((prev) => prev.some((row) => catalogConflicts(row, entry.plugin))
        ? prev : [...prev, toPluginRow({ ...entry.plugin, catalog: true }, uid)]);
    }
    focusRow.current = uid;
    setExpanded((prev) => new Set(prev).add(uid));
  };

  const remove = (row: PluginRow, button: HTMLButtonElement) => {
    if (lockedRef.current || saving.current) return;
    if (savedUIDs.current.has(row.uid)) {
      opener.current = button;
      setRemoving(row);
    } else {
      changeRows((current) => current.filter((item) => item.uid !== row.uid));
      addButton.current?.focus();
    }
  };

  const save = async () => {
    if (lockedRef.current || saving.current || invalid) return;
    saving.current = true;
    setSaveProblem("");
    if (operationError?.action === "apply-plugins") clearOperationError();
    let persisted = false;
    try {
      setPhase("saving");
      await bridge.SavePlugins(serializePlugins(rows));
      persisted = true;
      submittedRows.current = rows;
      setNeedsApply(true);
      if (!mounted.current) return;
      applying.current = true;
      setPhase("applying");
      // Acceptance is separate from completion; only care-done finishes this state.
      const accepted = await runAction("apply-plugins");
      if (!accepted && applying.current) {
        applying.current = false;
        saving.current = false;
        if (mounted.current) setPhase("apply-error");
      }
    } catch (cause) {
      applying.current = false;
      log(persisted ? "plugins: applying the saved settings could not start" : "plugins: the settings could not be saved");
      if (mounted.current) {
        setSaveProblem(describeOperationError("apply-plugins", cause).message);
        setPhase(persisted ? "apply-error" : "save-error");
      }
    } finally {
      if (!applying.current) saving.current = false;
    }
  };

  return (
    <div className="care-plugins" ref={root}>
      {loadErrors.saved || loadErrors.catalog ? (
        <Alert variant="danger" role="alert" className="care-plugins-message">
          <div>
            <strong>{loadErrors.saved ? "Couldn't read the saved plugins." : "Couldn't read the plugin catalog."}</strong>
            <p>Your saved choices haven&apos;t been replaced. Try again before making changes.</p>
          </div>
          <div className="care-plugins-actions">
            <Button disabled={busy || disabled || loading} onClick={() => void load()}>Retry</Button>
            <PanelLogButton />
          </div>
        </Alert>
      ) : null}
      {loading ? <div role="status" className="care-plugins-loading">
        <span aria-hidden="true"><Spinner /></span><span>{loaded ? "Loading the plugin catalog…" : "Loading plugins…"}</span>
      </div> : null}
      {pluginRecoveryPending ? <Alert variant="warn" role="status">
        Plugin recovery is unfinished. Use Recover clinic in Overview before changing plugins.
      </Alert> : restorePending ? <Alert variant="warn" role="status">
        An earlier restore needs to finish. Start CARE from Overview before changing plugins.
      </Alert> : (busy || disabled) && !working ? <Alert variant="info" role="status">
        Wait for the current operation to finish before changing plugins. Your edits stay here.
      </Alert> : null}
      {phase === "save-error" || phase === "apply-error" ? (
        <Alert variant="danger" role="alert" className="care-plugins-message">
          <div>
            <strong>{phase === "save-error" ? "Plugin settings couldn't be saved." : "The plugin changes couldn't be applied."}</strong>
            <p>{phase === "save-error" ? "Your edits are still here. " : "These changes were not applied. Leaving this tab discards the failed changes. "}
              {saveProblem || (operationError?.action === "apply-plugins" ? operationError.message : "") ||
                "Check the settings before retrying, or open the log file for support."}</p>
          </div>
          <PanelLogButton />
        </Alert>
      ) : null}
      {working || phase === "finished" ? <Alert variant="info" role="status" className="care-plugins-progress">
        {working ? <span aria-hidden="true"><Spinner /></span> : null}
        <div>
          <strong>{phase === "saving" ? "Saving plugin settings…"
            : phase === "applying" ? "Applying plugin changes…" : "Plugin task finished."}</strong>
          <p>{phase === "saving" ? "The changes haven't been applied yet."
            : phase === "applying" ? "Wait for CARE to rebuild and check clinic health. If loading fails, the previous settings will be restored."
              : "Plugins are applied and CARE is online. Reload CARE in staff browsers."}</p>
        </div>
      </Alert> : null}

      <div className="care-plugins-table overflow-hidden rounded-lg border border-line" aria-busy={loading}>
        {ready && !loading && rows.length === 0 ? (
          <div className="p-5 text-center text-[13px] text-faint">No plugins yet</div>
        ) : null}

        {rows.map((row, i) => {
          const open = expanded.has(row.uid);
          const description = row.catalog ? describe.get(row.id) : "";
          const name = row.label.trim() || row.id.trim() || "New plugin";
          const applied = appliedRows.current.find((saved) => saved.uid === row.uid);
          const unapplied = !applied || pluginSnapshot([row]) !== pluginSnapshot([applied]);
          const errors = problems.get(row.uid)!;
          const field = (name: string) => `plugin-${row.uid}-${name}`;
          return (
            <section key={row.uid} aria-labelledby={`plugin-name-${row.uid}`}
              className={cn("care-plugin-row", i > 0 && "border-t border-hair")}>
              <div className="care-plugin-heading flex items-center gap-2.5 p-3">
                <button
                  type="button"
                  onClick={() => toggleExpanded(row.uid)}
                  aria-label={`Edit ${name}`}
                  aria-expanded={open}
                  aria-controls={`plugin-editor-${row.uid}`}
                  className="care-plugin-toggle flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 text-left"
                >
                  <ChevronDown
                    aria-hidden="true"
                    className={cn("size-[14px] flex-none transition-transform", open && "rotate-180")}
                    strokeWidth={2.5}
                  />
                  <span className="min-w-0">
                    <span id={`plugin-name-${row.uid}`} className="block truncate text-[13.5px] font-semibold text-ink">
                      {name}
                    </span>
                    {description ? (
                      <span className="block truncate text-[12.5px] text-muted-foreground">
                        {description}
                      </span>
                    ) : null}
                  </span>
                </button>
                <div className="care-plugin-badges">
                  {loaded && unapplied ? <Badge size="sm" variant="warn">Not applied</Badge> : null}
                  {row.backend ? <Badge size="sm" variant="plain">Backend</Badge> : null}
                  {row.frontend ? <Badge size="sm" variant="plain">Frontend</Badge> : null}
                  {!row.catalog ? <Badge size="sm">Custom</Badge> : null}
                  {ready && hasPluginProblems(errors) ? <Badge size="sm" variant="bad">Check fields</Badge> : null}
                </div>
                <Button
                  type="button"
                  size="icon"
                  aria-label={`Remove ${name}`}
                  title={`Remove ${name}`}
                  disabled={locked}
                  className="border-danger-line text-danger-ink hover:border-danger-line hover:bg-danger-bg hover:text-danger-ink"
                  onClick={(event) => remove(row, event.currentTarget)}
                >
                  <span aria-hidden="true">×</span>
                </Button>
              </div>

              {open ? (
                <fieldset id={`plugin-editor-${row.uid}`} disabled={locked}
                  className="care-plugin-editor flex min-w-0 flex-col gap-4 px-3 pb-4">
                  <legend className="sr-only">{name} settings</legend>
                  {!row.catalog ? (
                    <div className="care-plugin-identity flex flex-wrap items-start gap-4">
                      <Field id={field("label")} label="Display name" className="care-plugin-identity-field"
                        hint="Optional; defaults to the plugin ID. Spaces are allowed.">
                        <Input
                          id={field("label")}
                          aria-describedby={`${field("label")}-help`}
                          className="h-9 text-[13px]"
                          placeholder="CARE Onboarding"
                          value={row.label}
                          onChange={(e) => patchRow(row.uid, { label: e.target.value })}
                        />
                      </Field>
                      <Field id={field("id")} label="Plugin ID" className="care-plugin-identity-field"
                        error={errors.id} hint="Unique technical ID; no spaces.">
                        <Input
                          id={field("id")}
                          className="h-9 font-mono text-[13px]"
                          spellCheck={false}
                          placeholder="care_example"
                          value={row.id}
                          aria-invalid={Boolean(errors.id)}
                          aria-describedby={`${field("id")}-help`}
                          onChange={(e) => patchRow(row.uid, { id: e.target.value })}
                        />
                      </Field>
                      <div className="care-plugin-parts" role="group" aria-label="Plugin parts"
                        aria-describedby={errors.parts ? field("parts-help") : undefined}>
                        <PartSwitch label="Backend" checked={row.backend !== null} disabled={locked}
                          onChange={(on) => patchRow(row.uid, {
                            backend: on ? row.inactiveBackend ?? emptyBackend() : null,
                            inactiveBackend: row.backend ?? row.inactiveBackend,
                          })} />
                        <PartSwitch label="Frontend" checked={row.frontend !== null} disabled={locked}
                          onChange={(on) => patchRow(row.uid, {
                            frontend: on ? row.inactiveFrontend ?? emptyFrontend() : null,
                            inactiveFrontend: row.frontend ?? row.inactiveFrontend,
                          })} />
                        {errors.parts ? <p id={field("parts-help")} className="care-plugin-field-error">{errors.parts}</p> : null}
                      </div>
                    </div>
                  ) : null}

                  {row.backend ? (
                    <Section title="Backend" hint="Installed into the CARE server; changing it rebuilds the backend.">
                      {!row.catalog ? (
                        <div className="flex flex-wrap gap-2.5">
                          <Field id={field("module")} label="Python module" className="care-plugin-short-field"
                            error={errors.module}>
                            <Input
                              id={field("module")}
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="care_example"
                              value={row.backend.name}
                              aria-invalid={Boolean(errors.module)}
                              aria-describedby={errors.module ? `${field("module")}-help` : undefined}
                              onChange={(e) => patchBackend(row.uid, { name: e.target.value })}
                            />
                          </Field>
                          <Field id={field("package")} label="pip source" className="care-plugin-source-field"
                            error={errors.package}>
                            <Input
                              id={field("package")}
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="git+https://github.com/org/repo.git"
                              value={row.backend.package_name}
                              aria-invalid={Boolean(errors.package)}
                              aria-describedby={errors.package ? `${field("package")}-help` : undefined}
                              onChange={(e) => patchBackend(row.uid, { package_name: e.target.value })}
                            />
                          </Field>
                          <Field id={field("version")} label="Version" className="care-plugin-version-field"
                            error={errors.version || undefined}>
                            <Input
                              id={field("version")}
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="@main"
                              value={row.backend.version}
                              aria-invalid={Boolean(errors.version)}
                              aria-describedby={errors.version ? `${field("version")}-help` : undefined}
                              onChange={(e) => patchBackend(row.uid, { version: e.target.value })}
                            />
                          </Field>
                        </div>
                      ) : null}
                      <div className="flex flex-col gap-[7px]">
                        {row.backend.configs.map((config, ci) => (
                          <div key={ci} className="care-plugin-config-row">
                            <Field id={field(`setting-${ci}-name`)} label={`Setting ${ci + 1} name`}
                              className="care-plugin-config-name" hiddenLabel error={errors.configs[ci]}>
                              <Input
                                id={field(`setting-${ci}-name`)}
                                className="h-9 rounded-[8px] px-[11px] font-mono text-[13px]"
                                placeholder="SETTING_NAME"
                                spellCheck={false}
                                value={config.key}
                                aria-invalid={Boolean(errors.configs[ci])}
                                aria-describedby={errors.configs[ci] ? `${field(`setting-${ci}-name`)}-help` : undefined}
                                onChange={(e) => patchConfig(row.uid, ci, { key: e.target.value })}
                              />
                            </Field>
                            <Field id={field(`setting-${ci}-value`)} label={`Setting ${ci + 1} value`}
                              className="care-plugin-config-value" hiddenLabel>
                              <Input
                                id={field(`setting-${ci}-value`)}
                                className="h-9 rounded-[8px] px-[11px] text-[13px]"
                                placeholder="value"
                                spellCheck={false}
                                autoComplete="off"
                                value={config.value}
                                onChange={(e) => patchConfig(row.uid, ci, { value: e.target.value })}
                              />
                            </Field>
                            <Button
                              type="button"
                              size="icon"
                              aria-label={`Remove setting ${ci + 1} from ${name}`}
                              className="border-danger-line text-danger-ink hover:border-danger-line hover:bg-danger-bg hover:text-danger-ink"
                              onClick={() =>
                                patchBackend(row.uid, {
                                  configs: row.backend!.configs.filter((_, i2) => i2 !== ci),
                                })
                              }
                            >
                              <span aria-hidden="true">×</span>
                            </Button>
                          </div>
                        ))}
                        <Button
                          type="button"
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
                          <Field id={field("slug")} label="Frontend name" className="care-plugin-short-field"
                            error={errors.slug}>
                            <Input
                              id={field("slug")}
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="care_example_fe"
                              value={row.frontend.slug}
                              aria-invalid={Boolean(errors.slug)}
                              aria-describedby={errors.slug ? `${field("slug")}-help` : undefined}
                              onChange={(e) => patchFrontend(row.uid, { slug: e.target.value })}
                            />
                          </Field>
                          <Field id={field("url")} label="remoteEntry.js URL" className="care-plugin-source-field"
                            error={errors.url}>
                            <Input
                              id={field("url")}
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="https://org.github.io/repo/assets/remoteEntry.js"
                              value={row.frontend.url}
                              aria-invalid={Boolean(errors.url)}
                              aria-describedby={errors.url ? `${field("url")}-help` : undefined}
                              onChange={(e) => patchFrontend(row.uid, { url: e.target.value })}
                            />
                          </Field>
                        </div>
                      ) : null}
                      <Field id={field("meta")} label="Frontend settings (JSON)" error={errors.meta}>
                        <textarea
                          id={field("meta")}
                          className={cn(
                            "min-h-[110px] w-full rounded-[8px] border border-line bg-white px-[11px] py-2 font-mono text-[12.5px] text-ink outline-none focus-visible:border-brand",
                            errors.meta && "border-danger-line",
                          )}
                          spellCheck={false}
                          autoComplete="off"
                          aria-invalid={Boolean(errors.meta)}
                          aria-describedby={errors.meta ? `${field("meta")}-help` : undefined}
                          value={row.frontend.metaText}
                          onChange={(e) => patchFrontend(row.uid, { metaText: e.target.value })}
                        />
                      </Field>
                    </Section>
                  ) : null}
                </fieldset>
              ) : null}
            </section>
          );
        })}
      </div>

      {ready ? <div className="care-plugins-edit-status" role="status" aria-live="polite">
        {dirty ? "Unsaved changes" : needsApply && !working ? "Plugin changes have not been applied" : ""}
        {invalid ? <p>Check the highlighted fields before saving.</p> : null}
      </div> : null}
      <div className="care-plugins-actions care-plugins-footer">
        <Select value="" disabled={locked} open={active && !locked && pickerOpen}
          onOpenChange={setPickerOpen} onValueChange={add}>
          <SelectTrigger ref={addButton} className="care-plugins-add w-auto" aria-label="Add a plugin">
            <SelectValue placeholder="Add a plugin" />
          </SelectTrigger>
          <SelectContent className="w-auto" onCloseAutoFocus={(event) => {
            if (!activeRef.current || lockedRef.current) {
              event.preventDefault();
              focusRow.current = null;
              return;
            }
            if (focusRow.current === null) return;
            event.preventDefault();
            const uid = focusRow.current;
            focusRow.current = null;
            window.requestAnimationFrame(() => {
              if (!activeRef.current || lockedRef.current) return;
              root.current?.querySelector<HTMLElement>(`#plugin-editor-${uid} input, #plugin-editor-${uid} textarea, #plugin-editor-${uid} button`)?.focus();
            });
          }}>
            {available.map((entry) => (
              <SelectItem key={entry.plugin.id} value={entry.plugin.id}>
                {entry.plugin.label || entry.plugin.id}
              </SelectItem>
            ))}
            <SelectItem value={CUSTOM}>Custom plugin</SelectItem>
          </SelectContent>
        </Select>
        <Button
          type="button"
          variant="primary"
          disabled={locked || invalid}
          onClick={() => void save()}
        >
          {phase === "saving" ? "Saving…" : phase === "applying" ? "Applying…"
            : needsApply && !dirty ? "Try applying again" : "Save and apply"}
        </Button>
      </div>
      <p className="care-plugins-hint">
        Saving updates the plugin list. Applying may rebuild CARE and briefly interrupt access.
      </p>
      <AlertDialog open={active && removing !== null} onOpenChange={(open) => { if (!open) setRemoving(null); }}>
        <AlertDialogContent className="care-plugins-dialog" onCloseAutoFocus={(event) => {
          event.preventDefault();
          if (!activeRef.current) return;
          if (opener.current?.isConnected && !opener.current.disabled) opener.current.focus();
          else if (!lockedRef.current) addButton.current?.focus();
          else root.current?.querySelector<HTMLButtonElement>(".care-plugin-toggle")?.focus();
        }}>
          <AlertDialogTitle>Remove {removing?.label.trim() || removing?.id || "this plugin"}?</AlertDialogTitle>
          <AlertDialogDescription>
            The plugin and its settings will be removed from this list. CARE won&apos;t change until you choose Save and apply.
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction disabled={locked} className="care-plugin-confirm-remove" onClick={() => {
              if (!removing || lockedRef.current || saving.current) return;
              changeRows((current) => current.filter((row) => row.uid !== removing.uid));
              setRemoving(null);
            }}>Remove plugin</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Field({
  id,
  label,
  className,
  error,
  hint,
  hiddenLabel = false,
  children,
}: {
  id: string;
  label: string;
  className?: string;
  error?: string;
  hint?: string;
  hiddenLabel?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("care-plugin-field flex min-w-0 flex-col gap-1", className)}>
      <label htmlFor={id} className={hiddenLabel ? "sr-only" : "text-[11.5px] font-bold tracking-[0.05em] text-faint uppercase"}>{label}</label>
      {children}
      {error || hint ? <span id={`${id}-help`}
        className={cn("text-[12px]", error ? "care-plugin-field-error" : "text-muted-foreground")}>
        {error || hint}
      </span> : null}
    </div>
  );
}

function PartSwitch({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-center gap-2 text-[13px] font-semibold text-ink">
      <Switch aria-label={label} checked={checked} disabled={disabled} onCheckedChange={onChange} />
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
    <div className="care-plugin-section flex min-w-0 flex-col gap-2.5 rounded-lg border border-hair p-3">
      <div>
        <div className="text-[13px] font-semibold text-ink">{title}</div>
        <div className="text-[12.5px] text-muted-foreground">{hint}</div>
      </div>
      {children}
    </div>
  );
}

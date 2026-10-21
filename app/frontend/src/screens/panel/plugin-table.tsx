import { ArrowUpRight, ChevronDown, Eye, EyeOff, Trash2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import { Alert } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { useCare, type PanelTab } from "@/state/care-store";
import type { CarePlugin, PluginCatalogEntry, PluginSetting, PluginSetupStep } from "@/types";

import {
  catalogConflicts, catalogEntry, describeChanges, describedSettings, emptyBackend, emptyFrontend,
  hasPluginProblems, isDescribed, pluginChanges, pluginName, pluginProblems, pluginSnapshot, readMeta,
  reconcileCatalog, requiredConfig, secretConfig, serializePlugins, settingValue, toPluginRow, withSetting,
  type BackendDraft, type ConfigRow, type FrontendDraft, type PluginRow,
} from "./plugin-model";
import { PanelLogButton } from "./panel-ui";

import "./plugin-table.css";

const CUSTOM = "__custom__";
type SavePhase = "idle" | "saving" | "applying" | "save-error" | "apply-error" | "finished";
type Problems = ReturnType<typeof pluginProblems>;
type Status = { label: string; variant: "ok" | "warn" | "bad" | "plain" };

const TAB_NAMES: Record<PanelTab, string> = {
  overview: "Overview", storage: "Storage", backups: "Backups", "home-access": "Home access",
  plugins: "Plugins", updates: "Updates", advanced: "Advanced",
};

export function PluginTable({ disabled = false }: { disabled?: boolean }) {
  const {
    busy, tab, setTab, setTabGuard, setUnsavedPlugins, mdnsName, flow, restorePending, pluginRecoveryPending,
    runAction, log, operationError, clearOperationError,
  } = useCare();
  const [rows, setRows] = useState<PluginRow[]>([]);
  const [catalog, setCatalog] = useState<PluginCatalogEntry[]>([]);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set());
  const [technical, setTechnical] = useState<ReadonlySet<number>>(new Set());
  const [touched, setTouched] = useState<ReadonlySet<string>>(new Set());
  const [attempted, setAttempted] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [catalogLoaded, setCatalogLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadErrors, setLoadErrors] = useState({ saved: false, catalog: false });
  const [baseline, setBaseline] = useState("");
  const [phase, setPhase] = useState<SavePhase>("idle");
  const [saveProblem, setSaveProblem] = useState("");
  const [needsApply, setNeedsApply] = useState(false);
  const [leaving, setLeaving] = useState<PanelTab | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const nextUid = useRef(0);
  const mounted = useRef(false);
  const attemptedLoad = useRef(false);
  const reading = useRef(false);
  const saving = useRef(false);
  const applying = useRef(false);
  const allowLeave = useRef(false);
  const leaveOpener = useRef<HTMLElement | null>(null);
  const leaveSaving = useRef(false);
  const submittedRows = useRef<PluginRow[]>([]);
  const appliedRows = useRef<PluginRow[]>([]);
  const initialRows = useRef<PluginRow[]>([]);
  const sources = useRef<{ saved: CarePlugin[] | null; catalog: PluginCatalogEntry[] | null }>({ saved: null, catalog: null });
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
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    setUnsavedPlugins(dirty);
  }, [dirty, setUnsavedPlugins]);

  useEffect(() => () => setUnsavedPlugins(false), [setUnsavedPlugins]);

  useEffect(() => {
    setTabGuard((next) => {
      if (allowLeave.current || next === "plugins" || !activeRef.current || !dirtyRef.current || lockedRef.current) return true;
      leaveOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setLeaving(next);
      return false;
    });
    return () => setTabGuard(null);
  }, [setTabGuard]);

  useEffect(() => {
    if (!active) {
      setLeaving(null);
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
      appliedRows.current = submittedRows.current.filter((row) => !row.removed);
      setRows((current) => current.filter((row) => !row.removed));
      setBaseline(pluginSnapshot(submittedRows.current));
    }
    setPhase(code === 0 ? "finished" : "apply-error");
  }), []);

  const available = catalog.filter((entry) => !rows.some((row) => catalogConflicts(row, entry.plugin)));
  const problems = useMemo(() => new Map(rows.map((row) => [row.uid, pluginProblems(row, rows, catalog)])), [rows, catalog]);
  const invalidRows = rows.filter((row) => hasPluginProblems(problems.get(row.uid)!));
  const invalid = invalidRows.length > 0;
  const changes = pluginChanges(rows, appliedRows.current);
  const summary = describeChanges(changes);
  const changeCount = changes.added.length + changes.removed.length + changes.changed.length;

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
                configs: r.backend.configs.map((c, i) => (i === index ? { ...c, ...values, typed: undefined } : c)),
              },
            }
          : r,
      ),
    );

  const patchSetting = (uid: number, setting: PluginSetting, value: unknown) =>
    changeRows((prev) => prev.map((r) => (r.uid === uid ? withSetting(r, setting, value) : r)));

  const toggle = (set: typeof setExpanded, uid: number) =>
    set((prev) => {
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
      changeRows((prev) => prev.some((row) => !row.removed && catalogConflicts(row, entry.plugin))
        ? prev : [...prev, reconcileCatalog(toPluginRow({ ...entry.plugin, catalog: true }, uid), catalog)]);
    }
    focusRow.current = uid;
    setExpanded((prev) => new Set(prev).add(uid));
  };

  const remove = (row: PluginRow) => {
    if (lockedRef.current || saving.current) return;
    if (appliedRows.current.some((saved) => saved.uid === row.uid)) {
      patchRow(row.uid, { removed: true });
      setExpanded((prev) => {
        const next = new Set(prev);
        next.delete(row.uid);
        return next;
      });
      window.requestAnimationFrame(() => {
        root.current?.querySelector<HTMLButtonElement>(`#plugin-undo-${row.uid}`)?.focus();
      });
    } else {
      changeRows((current) => current.filter((item) => item.uid !== row.uid));
      addButton.current?.focus();
    }
  };

  const discard = () => {
    if (lockedRef.current || saving.current) return;
    setRows(appliedRows.current);
    setExpanded(new Set());
    setTechnical(new Set());
    setTouched(new Set());
    setAttempted(false);
    setSaveProblem("");
    if (phase === "apply-error") {
      setNeedsApply(false);
      submittedRows.current = [];
    }
    setPhase("idle");
  };

  const revealProblems = () => {
    setAttempted(true);
    setExpanded((prev) => new Set([...prev, ...invalidRows.map((row) => row.uid)]));
    setTechnical((prev) => new Set([...prev, ...invalidRows.filter((row) => {
      const { configValues, ...rest } = problems.get(row.uid)!;
      return row.catalog && (hasPluginProblems({ ...rest, configValues: [] }) ||
        configValues.some((problem, index) => problem && !isDescribed(row, catalog, row.backend?.configs[index]?.key ?? "")));
    }).map((row) => row.uid)]));
    window.requestAnimationFrame(() => {
      root.current?.querySelector<HTMLElement>("[aria-invalid='true']")?.focus();
    });
  };

  const save = async (): Promise<boolean> => {
    if (lockedRef.current || saving.current) return false;
    if (invalid) {
      revealProblems();
      return false;
    }
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
      if (!mounted.current) return false;
      applying.current = true;
      setPhase("applying");
      const accepted = await runAction("apply-plugins");
      if (!accepted && applying.current) {
        applying.current = false;
        saving.current = false;
        if (mounted.current) setPhase("apply-error");
      }
      return accepted;
    } catch (cause) {
      applying.current = false;
      log(persisted ? "plugins: applying the saved settings could not start" : "plugins: the settings could not be saved");
      if (mounted.current) {
        setSaveProblem(describeOperationError("apply-plugins", cause).message);
        setPhase(persisted ? "apply-error" : "save-error");
      }
      return false;
    } finally {
      if (!applying.current) saving.current = false;
    }
  };

  const leave = (next: PanelTab) => {
    setLeaving(null);
    allowLeave.current = true;
    try {
      setTab(next);
    } finally {
      allowLeave.current = false;
    }
  };

  const runStep = (step: PluginSetupStep) => {
    if (step.action === "advanced") setTab("advanced");
    else if (step.action === "care") void bridge.OpenURL(`https://${mdnsName}/`).catch(() => log("plugins: CARE could not be opened in the browser"));
  };

  const pending = ready && (dirty || needsApply || working || phase === "save-error" || phase === "apply-error");

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
        <Alert variant="danger" role="alert" tabIndex={-1} className="care-plugins-message care-plugins-save-error">
          <div>
            <strong>{phase === "save-error" ? "Plugin settings couldn't be saved." : "The plugin changes couldn't be applied."}</strong>
            <p>{phase === "save-error" ? "Your edits are still here. " : "These changes were not applied. Fix them and try again, or discard them. "}
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
            : phase === "applying" ? "Applying plugin changes…" : "Plugin changes are applied."}</strong>
          <p>{phase === "saving" ? "The changes haven't been applied yet."
            : phase === "applying" ? "Wait for CARE to rebuild and check clinic health. If loading fails, the previous settings will be restored."
              : "CARE is online with the new plugins. Ask staff to reload CARE in their browsers."}</p>
        </div>
      </Alert> : null}

      <div className="care-plugins-table overflow-hidden rounded-lg border border-line" aria-busy={loading}>
        {ready && !loading && rows.length === 0 ? (
          <div className="p-5 text-center text-[13px] text-faint">No plugins yet. Choose one from Add a plugin below.</div>
        ) : null}

        {rows.map((row, i) => {
          const open = expanded.has(row.uid) && !row.removed;
          const entry = catalogEntry(row, catalog);
          const description = entry?.description ?? "";
          const name = pluginName(row);
          const applied = appliedRows.current.find((saved) => saved.uid === row.uid);
          const errors = problems.get(row.uid)!;
          const field = (key: string) => `plugin-${row.uid}-${key}`;
          const shown = (key: string, message: string | undefined) =>
            message && (attempted || touched.has(field(key))) ? message : undefined;
          const status = rowStatus(row, applied, errors, attempted);
          const settings = describedSettings(row, catalog);
          const steps = entry?.setup_steps ?? [];
          const showTechnical = !row.catalog || !settings.length || technical.has(row.uid);
          const meta = readMeta(row);
          const extraConfigs = row.backend?.configs
            .map((config, index) => ({ config, index }))
            .filter(({ config, index }) => !row.catalog || !isDescribed(row, catalog, config.key) || errors.configs[index])
            ?? [];
          return (
            <section key={row.uid} aria-labelledby={`plugin-name-${row.uid}`}
              className={cn("care-plugin-row", i > 0 && "border-t border-hair", row.removed && "care-plugin-removed")}>
              <div className="care-plugin-heading flex items-center gap-2.5 p-3">
                <button
                  type="button"
                  onClick={() => toggle(setExpanded, row.uid)}
                  disabled={row.removed}
                  aria-label={`Edit ${name}`}
                  aria-expanded={open}
                  aria-controls={`plugin-editor-${row.uid}`}
                  className="care-plugin-toggle flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 text-left"
                >
                  <ChevronDown
                    aria-hidden="true"
                    className={cn("size-[14px] flex-none transition-transform", open && "rotate-180", row.removed && "invisible")}
                    strokeWidth={2.5}
                  />
                  <span className="min-w-0">
                    <span id={`plugin-name-${row.uid}`} className="care-plugin-name block truncate text-[13.5px] font-semibold text-ink">
                      {name}
                    </span>
                    {row.removed ? (
                      <span className="block text-[12.5px] text-muted-foreground">
                        Will be removed when you save and apply.
                      </span>
                    ) : description ? (
                      <span className={cn("block text-[12.5px] text-muted-foreground", !open && "truncate")}>
                        {description}
                      </span>
                    ) : null}
                  </span>
                </button>
                <div className="care-plugin-badges">
                  {ready ? <Badge size="sm" variant={status.variant}>{status.label}</Badge> : null}
                  {!row.catalog && !row.removed ? <Badge size="sm">Custom</Badge> : null}
                </div>
                {row.removed ? (
                  <Button id={`plugin-undo-${row.uid}`} type="button" size="sm" disabled={locked}
                    aria-label={`Undo removing ${name}`} onClick={() => patchRow(row.uid, { removed: false })}>
                    <Undo2 aria-hidden="true" className="size-3.5" />Undo
                  </Button>
                ) : (
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    aria-label={`Remove ${name}`}
                    title={`Remove ${name}`}
                    disabled={locked}
                    className="care-plugin-remove"
                    onClick={() => remove(row)}
                  >
                    <Trash2 aria-hidden="true" className="size-3.5" />Remove
                  </Button>
                )}
              </div>

              {open ? (
                <fieldset id={`plugin-editor-${row.uid}`} disabled={locked}
                  onBlurCapture={(event) => {
                    const id = (event.target as HTMLElement).id;
                    if (id && !touched.has(id)) setTouched((prev) => new Set(prev).add(id));
                  }}
                  className="care-plugin-editor flex min-w-0 flex-col gap-4 px-3 pb-4">
                  <legend className="sr-only">{name} settings</legend>

                  {steps.length ? (
                    <div className="care-plugin-steps" role="note" aria-label={`Also needed for ${name}`}>
                      <div className="text-[12.5px] font-semibold text-ink">Also needed</div>
                      <ol>
                        {steps.map((step, si) => (
                          <li key={si}>
                            <span>{step.text}</span>
                            {step.action ? (
                              <Button type="button" size="sm" variant="soft" onClick={() => runStep(step)}>
                                {step.action === "advanced" ? "Open Advanced settings" : "Open CARE"}
                                <ArrowUpRight aria-hidden="true" className="size-3.5" />
                              </Button>
                            ) : null}
                          </li>
                        ))}
                      </ol>
                    </div>
                  ) : null}

                  {settings.length ? (
                    <div className="care-plugin-settings flex flex-col gap-3.5">
                      {settings.some((s) => s.part === "frontend") && !meta ? (
                        <Alert variant="warn" role="status">
                          These settings can&apos;t be shown because the advanced JSON below is invalid. Fix it in Technical details.
                        </Alert>
                      ) : null}
                      {settings.map((setting) => {
                        const current = settingValue(row, setting);
                        const index = setting.part === "backend"
                          ? row.backend?.configs.findIndex((config) => config.key.trim() === setting.key) ?? -1 : -1;
                        const required = index >= 0 && requiredConfig(row, catalog, index);
                        const id = field(`setting-${setting.part}-${setting.key}`);
                        return (
                          <SettingField
                            key={`${setting.part}-${setting.key}`}
                            id={id}
                            setting={setting}
                            value={current === undefined ? setting.default : current}
                            required={required}
                            disabled={locked || (setting.part === "frontend" && !meta)}
                            error={index >= 0 ? (attempted || touched.has(id) ? errors.configValues[index] : undefined) : undefined}
                            onChange={(value) => patchSetting(row.uid, setting, value)}
                          />
                        );
                      })}
                    </div>
                  ) : null}

                  {row.catalog && settings.length ? (
                    <button type="button" className="care-plugin-technical-toggle"
                      aria-expanded={showTechnical} aria-controls={`plugin-technical-${row.uid}`}
                      onClick={() => toggle(setTechnical, row.uid)}>
                      <ChevronDown aria-hidden="true" className={cn("size-3.5 transition-transform", showTechnical && "rotate-180")} strokeWidth={2.5} />
                      Technical details
                    </button>
                  ) : null}

                  {showTechnical ? (
                    <div id={`plugin-technical-${row.uid}`} className="flex min-w-0 flex-col gap-4">
                      {row.catalog ? (
                        <dl className="care-plugin-sources">
                          {row.backend ? <>
                            <dt>Server part</dt>
                            <dd><code>{row.backend.package_name}{row.backend.version}</code> (module <code>{row.backend.name}</code>)</dd>
                          </> : null}
                          {row.frontend ? <>
                            <dt>Screen part</dt>
                            <dd><code>{row.frontend.url}</code> (name <code>{row.frontend.slug}</code>)</dd>
                          </> : null}
                          <dt>Plugin ID</dt>
                          <dd><code>{row.id}</code></dd>
                        </dl>
                      ) : (
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
                            error={shown("id", errors.id)} hint="Unique technical ID; no spaces.">
                            <Input
                              id={field("id")}
                              className="h-9 font-mono text-[13px]"
                              spellCheck={false}
                              placeholder="care_example"
                              value={row.id}
                              aria-invalid={Boolean(shown("id", errors.id))}
                              aria-describedby={`${field("id")}-help`}
                              onChange={(e) => patchRow(row.uid, { id: e.target.value })}
                            />
                          </Field>
                          <div className="care-plugin-parts" role="group" aria-label="Plugin parts" tabIndex={-1}
                            aria-invalid={errors.parts ? true : undefined}
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
                      )}

                      {row.backend ? (
                        <Section title={row.catalog ? "Other server settings" : "Backend"}
                          hint={row.catalog ? "Settings without a form above. Changing them rebuilds the CARE server."
                            : "Installed into the CARE server; changing it rebuilds the backend."}>
                          {!row.catalog ? (
                            <div className="flex flex-wrap gap-2.5">
                              <Field id={field("module")} label="Python module" className="care-plugin-short-field"
                                error={shown("module", errors.module)}>
                                <Input
                                  id={field("module")}
                                  className="h-9 font-mono text-[13px]"
                                  spellCheck={false}
                                  placeholder="care_example"
                                  value={row.backend.name}
                                  aria-invalid={Boolean(shown("module", errors.module))}
                                  aria-describedby={shown("module", errors.module) ? `${field("module")}-help` : undefined}
                                  onChange={(e) => patchBackend(row.uid, { name: e.target.value })}
                                />
                              </Field>
                              <Field id={field("package")} label="pip source" className="care-plugin-source-field"
                                error={shown("package", errors.package)}>
                                <Input
                                  id={field("package")}
                                  className="h-9 font-mono text-[13px]"
                                  spellCheck={false}
                                  placeholder="git+https://github.com/org/repo.git"
                                  value={row.backend.package_name}
                                  aria-invalid={Boolean(shown("package", errors.package))}
                                  aria-describedby={shown("package", errors.package) ? `${field("package")}-help` : undefined}
                                  onChange={(e) => patchBackend(row.uid, { package_name: e.target.value })}
                                />
                              </Field>
                              <Field id={field("version")} label="Version" className="care-plugin-version-field"
                                error={shown("version", errors.version)}>
                                <Input
                                  id={field("version")}
                                  className="h-9 font-mono text-[13px]"
                                  spellCheck={false}
                                  placeholder="@main"
                                  value={row.backend.version}
                                  aria-invalid={Boolean(shown("version", errors.version))}
                                  aria-describedby={shown("version", errors.version) ? `${field("version")}-help` : undefined}
                                  onChange={(e) => patchBackend(row.uid, { version: e.target.value })}
                                />
                              </Field>
                            </div>
                          ) : null}
                          <div className="flex flex-col gap-[7px]">
                            {extraConfigs.map(({ config, index: ci }) => {
                              const required = requiredConfig(row, catalog, ci);
                              const nameError = shown(`setting-${ci}-name`, errors.configs[ci]);
                              const valueError = shown(`setting-${ci}-value`, errors.configValues[ci]);
                              return (
                                <div key={ci} className="care-plugin-config-row">
                                  <Field id={field(`setting-${ci}-name`)} label={`Setting ${ci + 1} name`}
                                    className="care-plugin-config-name" hiddenLabel error={nameError}>
                                    <Input
                                      id={field(`setting-${ci}-name`)}
                                      className="h-9 rounded-[8px] px-[11px] font-mono text-[13px]"
                                      placeholder="SETTING_NAME"
                                      spellCheck={false}
                                      readOnly={required}
                                      value={config.key}
                                      aria-invalid={Boolean(nameError)}
                                      aria-describedby={nameError ? `${field(`setting-${ci}-name`)}-help` : undefined}
                                      onChange={(e) => patchConfig(row.uid, ci, { key: e.target.value })}
                                    />
                                  </Field>
                                  <Field id={field(`setting-${ci}-value`)} label={`Setting ${ci + 1} value`}
                                    className="care-plugin-config-value" hiddenLabel error={valueError}>
                                    <Input
                                      id={field(`setting-${ci}-value`)}
                                      type={required && secretConfig(config.key) ? "password" : undefined}
                                      className="h-9 rounded-[8px] px-[11px] text-[13px]"
                                      placeholder={required ? "required" : "value"}
                                      spellCheck={false}
                                      autoComplete="off"
                                      value={config.value}
                                      aria-required={required || undefined}
                                      aria-invalid={Boolean(valueError)}
                                      aria-describedby={valueError ? `${field(`setting-${ci}-value`)}-help` : undefined}
                                      onChange={(e) => patchConfig(row.uid, ci, { value: e.target.value })}
                                    />
                                  </Field>
                                  {required ? <span aria-hidden="true" className="care-plugin-config-spacer" /> : <Button
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
                                  </Button>}
                                </div>
                              );
                            })}
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
                          title={row.catalog ? "Screen settings" : "Frontend"}
                          hint={row.catalog ? "Every screen setting as JSON, including the ones above. Takes effect on the next page load."
                            : "Loaded by staff browsers from its URL; no rebuild needed. Settings are the plugin's JSON config."}
                        >
                          {!row.catalog ? (
                            <div className="flex flex-wrap gap-2.5">
                              <Field id={field("slug")} label="Frontend name" className="care-plugin-short-field"
                                error={shown("slug", errors.slug)}>
                                <Input
                                  id={field("slug")}
                                  className="h-9 font-mono text-[13px]"
                                  spellCheck={false}
                                  placeholder="care_example_fe"
                                  value={row.frontend.slug}
                                  aria-invalid={Boolean(shown("slug", errors.slug))}
                                  aria-describedby={shown("slug", errors.slug) ? `${field("slug")}-help` : undefined}
                                  onChange={(e) => patchFrontend(row.uid, { slug: e.target.value })}
                                />
                              </Field>
                              <Field id={field("url")} label="remoteEntry.js URL" className="care-plugin-source-field"
                                error={shown("url", errors.url)}>
                                <Input
                                  id={field("url")}
                                  className="h-9 font-mono text-[13px]"
                                  spellCheck={false}
                                  placeholder="https://org.github.io/repo/assets/remoteEntry.js"
                                  value={row.frontend.url}
                                  aria-invalid={Boolean(shown("url", errors.url))}
                                  aria-describedby={shown("url", errors.url) ? `${field("url")}-help` : undefined}
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
                    </div>
                  ) : null}
                </fieldset>
              ) : null}
            </section>
          );
        })}
      </div>

      <div className="care-plugins-actions care-plugins-footer">
        <Select value="" disabled={locked} open={active && !locked && pickerOpen}
          onOpenChange={setPickerOpen} onValueChange={add}>
          <SelectTrigger ref={addButton} className="care-plugins-add w-auto" aria-label="Add a plugin">
            <SelectValue placeholder="Add a plugin" />
          </SelectTrigger>
          <SelectContent className="care-plugins-picker" onCloseAutoFocus={(event) => {
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
                <span className="care-plugins-picker-name">{entry.plugin.label || entry.plugin.id}</span>
                {entry.description ? <span aria-hidden="true" className="care-plugins-picker-help">{entry.description}</span> : null}
              </SelectItem>
            ))}
            <SelectItem value={CUSTOM}>
              <span className="care-plugins-picker-name">Custom plugin</span>
              <span aria-hidden="true" className="care-plugins-picker-help">For developers: add a plugin from its source.</span>
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className={cn("care-plugins-savebar", pending && "care-plugins-savebar-pending",
        pending && invalid && attempted && "care-plugins-savebar-invalid")}>
        <div className="care-plugins-edit-status" role="status" aria-live="polite" tabIndex={-1}>
          <strong>{!ready ? "" : working ? (phase === "saving" ? "Saving…" : "Applying…")
            : dirty ? `${changeCount} unsaved ${changeCount === 1 ? "change" : "changes"}`
              : needsApply ? "Plugin changes have not been applied" : "No unsaved changes"}</strong>
          {dirty && summary && !working ? <span>{summary}</span> : null}
          {!ready ? null : invalid && attempted && !working ? (
            <p>{invalidRows.length === 1 ? `${pluginName(invalidRows[0])} needs attention` : `${invalidRows.length} plugins need attention`} before saving.</p>
          ) : !working ? <span className="care-plugins-savebar-hint">{pending
            ? "Applying may rebuild CARE and briefly interrupt access."
            : "Changes here take effect only after you choose Save and apply."}</span> : null}
        </div>
        <div className="care-plugins-actions">
          {dirty ? <Button type="button" disabled={locked} onClick={discard}>Discard changes</Button> : null}
          <Button
            type="button"
            variant="primary"
            disabled={locked || !pending}
            onClick={() => void save()}
          >
            {phase === "saving" ? "Saving…" : phase === "applying" ? "Applying…"
              : needsApply && !dirty ? "Try applying again" : "Save and apply"}
          </Button>
        </div>
      </div>

      <AlertDialog open={active && leaving !== null} onOpenChange={(open) => { if (!open) setLeaving(null); }}>
        <AlertDialogContent className="care-plugins-dialog" onCloseAutoFocus={(event) => {
          event.preventDefault();
          if (!activeRef.current) return;
          if (leaveSaving.current) {
            root.current?.querySelector<HTMLElement>(".care-plugins-edit-status")?.focus();
            return;
          }
          const opener = leaveOpener.current;
          if (opener?.isConnected && !opener.matches(":disabled") && opener.getClientRects().length) opener.focus();
          else addButton.current?.focus();
        }}>
          <AlertDialogTitle>Your plugin changes aren&apos;t saved</AlertDialogTitle>
          <AlertDialogDescription>
            {summary ? `You chose to ${summary.charAt(0).toLowerCase()}${summary.slice(1)} ` : ""}
            Nothing changes in CARE until you choose Save and apply.
            {leaving ? ` Discarding opens ${TAB_NAMES[leaving]} without them.` : ""}
          </AlertDialogDescription>
          {invalid ? <p className="care-plugin-field-error">
            {invalidRows.length === 1 ? `${pluginName(invalidRows[0])} needs attention` : `${invalidRows.length} plugins need attention`} before these changes can be saved.
          </p> : null}
          <AlertDialogFooter className="care-plugins-dialog-footer">
            <AlertDialogCancel>Keep editing</AlertDialogCancel>
            <Button type="button" className="care-plugin-confirm-discard" onClick={() => {
              const next = leaving;
              discard();
              if (next) leave(next);
            }}>Discard changes</Button>
            <Button type="button" variant="primary" disabled={locked || invalid} onClick={() => {
              const next = leaving;
              leaveSaving.current = true;
              setLeaving(null);
              void save().then((started) => {
                leaveSaving.current = false;
                if (!activeRef.current) return;
                if (started && next) {
                  leave(next);
                  return;
                }
                window.requestAnimationFrame(() => {
                  root.current?.querySelector<HTMLElement>(".care-plugins-save-error, [aria-invalid='true']")?.focus();
                });
              });
            }}>Save and apply</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function rowStatus(row: PluginRow, applied: PluginRow | undefined, errors: Problems, attempted: boolean): Status {
  if (row.removed) return { label: "Will be removed", variant: "bad" };
  if (hasPluginProblems(errors)) {
    const { configValues, ...rest } = errors;
    if (configValues.some(Boolean) && !hasPluginProblems({ ...rest, configValues: [] })) {
      return { label: "Needs setup", variant: "warn" };
    }
    return attempted ? { label: "Check fields", variant: "bad" } : { label: "Not finished", variant: "warn" };
  }
  if (!applied) return { label: "New · not applied", variant: "warn" };
  if (pluginSnapshot([row]) !== pluginSnapshot([applied])) return { label: "Changed · not applied", variant: "warn" };
  return { label: "Active", variant: "ok" };
}

function SettingField({
  id,
  setting,
  value,
  required,
  disabled,
  error,
  onChange,
}: {
  id: string;
  setting: PluginSetting;
  value: unknown;
  required: boolean;
  disabled: boolean;
  error?: string;
  onChange: (value: unknown) => void;
}) {
  const [reveal, setReveal] = useState(false);
  const help = `${id}-help`;
  const label = <>{setting.label}{required ? <span className="care-plugin-required"> (required)</span> : null}</>;

  if (setting.type === "boolean") {
    return (
      <div className="care-plugin-setting-switch">
        <Switch id={id} checked={value === true} disabled={disabled}
          aria-describedby={setting.help ? help : undefined}
          onCheckedChange={(on) => onChange(on)} />
        <div className="min-w-0">
          <label htmlFor={id} className="text-[13px] font-semibold text-ink">{label}</label>
          {setting.help ? <p id={help} className="text-[12px] text-muted-foreground">{setting.help}</p> : null}
        </div>
      </div>
    );
  }

  if (setting.type === "choices") {
    const chosen = Array.isArray(value) ? value.map(String) : [];
    const options = [...(setting.options ?? []), ...chosen
      .filter((v) => !setting.options?.some((option) => option.value === v))
      .map((v) => ({ value: v, label: v }))];
    return (
      <fieldset className="care-plugin-setting" aria-describedby={setting.help || error ? help : undefined}>
        <legend className="care-plugin-setting-label">{label}</legend>
        <div className="care-plugin-choices">
          {options.map((option) => {
            const optionId = `${id}-${option.value}`;
            return (
              <label key={option.value} htmlFor={optionId} className="care-plugin-choice">
                <Checkbox id={optionId} disabled={disabled} checked={chosen.includes(option.value)}
                  className="data-[state=checked]:border-brand data-[state=checked]:bg-brand"
                  onCheckedChange={(on) => onChange(on === true
                    ? options.map((o) => o.value).filter((v) => v === option.value || chosen.includes(v))
                    : chosen.filter((v) => v !== option.value))} />
                {option.label}
              </label>
            );
          })}
        </div>
        <SettingHelp id={help} help={setting.help} error={error} />
      </fieldset>
    );
  }

  return (
    <div className="care-plugin-setting">
      <label htmlFor={id} className="care-plugin-setting-label">{label}</label>
      {setting.type === "choice" ? (
        <Select value={typeof value === "string" ? value : ""} disabled={disabled} onValueChange={onChange}>
          <SelectTrigger id={id} className="care-plugin-setting-input" aria-describedby={setting.help || error ? help : undefined}>
            <SelectValue placeholder={setting.placeholder ?? "Choose"} />
          </SelectTrigger>
          <SelectContent>
            {setting.options?.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
          </SelectContent>
        </Select>
      ) : setting.type === "list" ? (
        <ListInput id={id} value={value} disabled={disabled} placeholder={setting.placeholder}
          describedBy={setting.help || error ? help : undefined} invalid={Boolean(error)} onChange={onChange} />
      ) : (
        <div className="care-plugin-setting-row">
          <Input
            id={id}
            className="care-plugin-setting-input h-9 text-[13px]"
            type={setting.type === "secret" && !reveal ? "password" : setting.type === "number" ? "number" : "text"}
            inputMode={setting.type === "url" ? "url" : undefined}
            spellCheck={false}
            autoComplete="off"
            disabled={disabled}
            placeholder={setting.placeholder}
            value={value === undefined || value === null ? "" : String(value)}
            aria-required={required || undefined}
            aria-invalid={Boolean(error)}
            aria-describedby={setting.help || error ? help : undefined}
            onChange={(e) => onChange(setting.type === "number"
              ? (e.target.value === "" ? null : Number(e.target.value)) : e.target.value)}
          />
          {setting.type === "secret" ? (
            <Button type="button" size="icon" className="care-plugin-reveal" disabled={disabled}
              aria-label={reveal ? `Hide ${setting.label}` : `Show ${setting.label}`} aria-pressed={reveal}
              onClick={() => setReveal((on) => !on)}>
              {reveal ? <EyeOff aria-hidden="true" className="size-3.5" /> : <Eye aria-hidden="true" className="size-3.5" />}
            </Button>
          ) : null}
        </div>
      )}
      <SettingHelp id={help} help={setting.help} error={error} />
    </div>
  );
}

function SettingHelp({ id, help, error }: { id: string; help?: string; error?: string }) {
  if (!help && !error) return null;
  return (
    <span id={id} className="flex flex-col gap-0.5 text-[12px]">
      {error ? <span className="care-plugin-field-error">{error}</span> : null}
      {help ? <span className="text-muted-foreground">{help}</span> : null}
    </span>
  );
}

function ListInput({
  id,
  value,
  disabled,
  placeholder,
  describedBy,
  invalid,
  onChange,
}: {
  id: string;
  value: unknown;
  disabled: boolean;
  placeholder?: string;
  describedBy?: string;
  invalid: boolean;
  onChange: (value: string[]) => void;
}) {
  const items = Array.isArray(value) ? value.map(String) : [];
  const [text, setText] = useState(items.join("\n"));
  const lines = (raw: string) => raw.split("\n").map((line) => line.trim()).filter(Boolean);
  const current = JSON.stringify(items);
  useEffect(() => {
    setText((raw) => JSON.stringify(lines(raw)) === current ? raw : (JSON.parse(current) as string[]).join("\n"));
  }, [current]);
  return (
    <textarea
      id={id}
      className={cn(
        "care-plugin-setting-input min-h-[76px] w-full rounded-[8px] border border-line bg-white px-[11px] py-2 font-mono text-[12.5px] text-ink outline-none focus-visible:border-brand",
        invalid && "border-danger-line",
      )}
      rows={Math.max(2, items.length + 1)}
      spellCheck={false}
      autoComplete="off"
      disabled={disabled}
      placeholder={placeholder}
      aria-invalid={invalid}
      aria-describedby={describedBy}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        onChange(lines(e.target.value));
      }}
    />
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

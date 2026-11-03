import {
  Archive, ChevronDown, ChevronRight, LockKeyhole, ShieldCheck,
  Palette, Receipt, Settings2, Trash2, UserPlus,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";
import { bridge } from "@/lib/bridge";
import { ENV_KEY_RE, getValue, type EnvChange, type EnvLine } from "@/lib/env-file";
import { useCare } from "@/state/care-store";
import type { EnvFileChange, Section } from "@/types";
import {
  editableEnvLines, effectiveSetting, mergeEnvChanges, validateExtraSetting, validateSetting, type EnvDraft,
} from "./advanced-env";
import {
  AdvancedError, AdvancedNotice, AdvancedSecretInput, advancedProblem, useAdvancedLock, type AdvancedProblem,
} from "./advanced-ui";
import { SettingControl, splitList } from "./env-controls";
import { fileForKey, GROUPS, isHiddenKey, MANAGED_NOTES, SETTING_BY_KEY, SETTINGS, VISIT_OPTIONS, type Setting } from "./env-schema";

const SECTIONS: Section[] = ["backend", "frontend"];
const ICONS = [Palette, UserPlus, Receipt, Archive, LockKeyhole, ShieldCheck];
type Files = Record<Section, { text: string; lines: EnvLine[] }>;
type CustomRow = { uid: number; key: string; value: string; file: Section; isNew: boolean };

function visibleOptions(setting: Setting, files: Files | null): Setting {
  if (setting.key !== "REACT_DEFAULT_ENCOUNTER_TYPE" || setting.kind !== "select" || !files) return setting;
  const configured = getValue(files.frontend.lines, "REACT_ALLOWED_ENCOUNTER_CLASSES");
  if (configured === undefined) return setting;
  const allowed = splitList(configured);
  return { ...setting, options: setting.options.filter((option) => allowed.includes(option.value)) };
}

function initialDraft(files: Files): EnvDraft {
  return Object.fromEntries(SETTINGS.map((setting) =>
    [setting.key, getValue(files[setting.file].lines, setting.key)]));
}

function customRows(files: Files, take: () => number): CustomRow[] {
  const rows: CustomRow[] = [];
  for (const file of SECTIONS) {
    const seen = new Set<string>();
    for (const line of files[file].lines) {
      if (line.kind !== "kv" || SETTING_BY_KEY.has(line.key) || isHiddenKey(line.key) || seen.has(line.key)) continue;
      seen.add(line.key);
      rows.push({ uid: take(), key: line.key, value: getValue(files[file].lines, line.key) ?? "", file, isNew: false });
    }
  }
  return rows;
}

export function EnvEditor({ adminPassword, expanded: expandedProp, onExpandedChange, onWorkingChange }: {
  adminPassword: string;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  onWorkingChange?: (working: boolean) => void;
}) {
  const { busy, restorePending, applySettings, operationError } = useCare();
  const lock = useAdvancedLock();
  const [localExpanded, setLocalExpanded] = useState(false);
  const editing = expandedProp === undefined ? localExpanded : expandedProp;
  const [files, setFiles] = useState<Files | null>(null);
  const [initial, setInitial] = useState<EnvDraft>({});
  const [draft, setDraft] = useState<EnvDraft>({});
  const [customInitial, setCustomInitial] = useState<CustomRow[]>([]);
  const [custom, setCustom] = useState<CustomRow[]>([]);
  const [loadProblem, setLoadProblem] = useState<AdvancedProblem | null>(null);
  const [saveProblem, setSaveProblem] = useState<AdvancedProblem | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [applying, setApplying] = useState(false);
  const [notice, setNotice] = useState("");
  const [discarding, setDiscarding] = useState(false);
  const nextUid = useRef(0);
  const pending = useRef(false);
  const loadVersion = useRef(0);
  const loadingRequest = useRef<{ password: string; promise: Promise<Files> } | null>(null);
  // What the editor shows as saved once CARE confirms the new settings are running.
  const submitted = useRef<{ file: Section; text: string }[]>([]);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const take = () => nextUid.current++;
  const working = busy || saving || applying || restorePending;
  const locked = working || lock.disabled;

  const setEditing = (next: boolean) => {
    setLocalExpanded(next);
    onExpandedChange?.(next);
  };
  const readFiles = useCallback(async (): Promise<Files> => {
    const [backend, frontend] = await Promise.all(SECTIONS.map((file) => bridge.ReadEnv(file, adminPassword)));
    return {
      backend: { text: backend, lines: editableEnvLines(backend) },
      frontend: { text: frontend, lines: editableEnvLines(frontend) },
    };
  }, [adminPassword]);
  const load = useCallback(async () => {
    const version = ++loadVersion.current;
    setLoading(true);
    setLoadProblem(null);
    const request = loadingRequest.current?.password === adminPassword ? loadingRequest.current
      : { password: adminPassword, promise: readFiles() };
    loadingRequest.current = request;
    try {
      const next = await request.promise;
      if (version !== loadVersion.current) return;
      const values = initialDraft(next);
      const rows = customRows(next, take);
      setFiles(next);
      setInitial(values);
      setDraft(values);
      setCustomInitial(rows);
      setCustom(rows);
    } catch (cause) {
      if (version !== loadVersion.current) return;
      setFiles(null);
      setLoadProblem(advancedProblem(cause, "The clinic settings couldn't be read",
        "Try again when CARE Clinic is idle. No settings have been changed."));
    } finally {
      if (loadingRequest.current === request) loadingRequest.current = null;
      if (version === loadVersion.current) setLoading(false);
    }
  }, [readFiles, adminPassword]);
  useEffect(() => {
    void load();
    return () => { loadVersion.current++; };
  }, [load]);
  useEffect(() => { onWorkingChange?.(saving || applying); }, [saving, applying, onWorkingChange]);

  const dirtyKeys = useMemo(() => SETTINGS.filter((setting) =>
    effectiveSetting(setting, draft[setting.key]) !== effectiveSetting(setting, initial[setting.key]))
    .map((setting) => setting.key), [draft, initial]);
  const customDelta = useMemo(() => {
    const before = new Map(customInitial.map((row) => [row.uid, row]));
    return {
      removed: customInitial.filter((row) => !custom.some((current) => current.uid === row.uid)),
      changed: custom.filter((row) => row.isNew ? row.key.trim() !== "" || row.value !== "" : before.get(row.uid)?.value !== row.value),
    };
  }, [custom, customInitial]);
  const errors = useMemo(() => {
    const out: Record<string, string> = {};
    for (const setting of SETTINGS) {
      if (!dirtyKeys.includes(setting.key)) continue;
      const error = validateSetting(setting, draft[setting.key]);
      if (error) out[setting.key] = error;
    }
    if (dirtyKeys.includes("REACT_DEFAULT_ENCOUNTER_TYPE") && files) {
      const selectedVisit = draft.REACT_DEFAULT_ENCOUNTER_TYPE ?? "";
      const configured = getValue(files.frontend.lines, "REACT_ALLOWED_ENCOUNTER_CLASSES");
      const allowed = configured === undefined ? VISIT_OPTIONS.map((option) => option.value) : splitList(configured);
      if (selectedVisit && !allowed.includes(selectedVisit)) out.REACT_DEFAULT_ENCOUNTER_TYPE = "This visit type is not enabled for your clinic. Ask support to enable it first.";
    }
    const names = new Map<string, number>();
    for (const row of custom) {
      const key = row.key.trim();
      if (key || row.value) names.set(`${row.file}:${key}`, (names.get(`${row.file}:${key}`) ?? 0) + 1);
    }
    for (const row of custom) {
      const key = row.key.trim();
      if (!key && !row.value) continue;
      const id = `custom:${row.uid}`;
      if (!key) out[id] = "Give the setting a name.";
      else if (!ENV_KEY_RE.test(key)) out[id] = "Start with a letter or _. Use only letters, digits and _.";
      else if (isHiddenKey(key)) out[id] = MANAGED_NOTES[key] ?? "This setting is protected by CARE Clinic.";
      else if (SETTING_BY_KEY.has(key)) out[id] = `Change “${SETTING_BY_KEY.get(key)!.label}” in its section above instead.`;
      else if ((names.get(`${row.file}:${key}`) ?? 0) > 1) out[id] = "This setting is listed twice.";
      else if (customDelta.changed.some((changed) => changed.uid === row.uid)) {
        const error = validateExtraSetting(key, row.value);
        if (error) out[id] = error;
      }
    }
    return out;
  }, [draft, custom, dirtyKeys, customDelta, files]);
  const changeCount = dirtyKeys.length + customDelta.changed.length + customDelta.removed.length;
  const hasErrors = Object.keys(errors).length > 0;
  const disabled = locked || loading || !files;
  const canSave = !disabled && changeCount > 0 && !hasErrors;

  const commitFile = (file: Section, text: string) => {
    if (!files) return;
    const next = { text, lines: editableEnvLines(text) };
    setFiles((current) => current ? { ...current, [file]: next } : current);
    const values = initialDraft({ ...files, [file]: next });
    const committed = Object.fromEntries(SETTINGS.filter((setting) => setting.file === file).map((setting) => [setting.key, values[setting.key]]));
    setInitial((current) => ({ ...current, ...committed }));
    setDraft((current) => ({ ...current, ...committed }));
    const rows = customRows({ ...files, [file]: next }, take).filter((row) => row.file === file)
      .map((row) => ({ ...row, uid: custom.find((before) => before.file === file && before.key.trim() === row.key)?.uid ?? row.uid }));
    setCustomInitial((current) => [...current.filter((row) => row.file !== file), ...rows]);
    setCustom((current) => [...current.filter((row) => row.file !== file), ...rows]);
  };
  // The editor only treats the new settings as saved once CARE is running on them.
  // When CARE could not start on them the previous settings are back, so the
  // edits stay in place for another try.
  useEffect(() => {
    if (!applying || busy) return;
    setApplying(false);
    const applied = submitted.current;
    submitted.current = [];
    if (operationError) {
      setSaveProblem({ title: "The settings weren't applied",
        message: `Your edits are still here. ${operationError.action === "apply-settings" ? operationError.message
          : "Check the log, then try again."}` });
      return;
    }
    for (const { file, text } of applied) commitFile(file, text);
    setNotice("Settings applied.");
  }, [applying, busy, operationError]);
  const save = async () => {
    if (!canSave || lock.isLocked() || pending.current || !files) return;
    pending.current = true;
    setSaving(true);
    setSaveProblem(null);
    setNotice("");
    try {
      const fresh = await readFiles();
      const touched: { file: Section; text: string }[] = [];
      const changed: EnvFileChange[] = [];
      for (const file of SECTIONS) {
        const changes: EnvChange[] = [];
        for (const key of dirtyKeys) {
          const setting = SETTING_BY_KEY.get(key)!;
          if (setting.file !== file) continue;
          const value = effectiveSetting(setting, draft[key]);
          changes.push({ key, value: value === "" ? undefined : value });
        }
        for (const row of customDelta.removed) if (row.file === file) changes.push({ key: row.key, value: undefined });
        for (const row of customDelta.changed) {
          if (row.file === file && row.key.trim()) changes.push({ key: row.key.trim(), value: row.value });
        }
        if (!changes.length) continue;
        const text = mergeEnvChanges(fresh[file].text, changes, file);
        touched.push({ file, text });
        if (text !== fresh[file].text) changed.push({ name: file, content: text });
      }
      if (!changed.length) {
        for (const { file, text } of touched) commitFile(file, text);
        setNotice("The saved settings already match these values.");
        return;
      }
      if (lock.isLocked()) throw new Error("Advanced changes are locked while another task finishes.");
      const accepted: unknown = await applySettings(changed, adminPassword);
      if (accepted !== true) {
        setSaveProblem({ title: "The settings weren't applied",
          message: "Nothing was changed and your edits are still here. Check the message at the top of the page, wait for any other task to finish, then try again." });
        return;
      }
      submitted.current = touched;
      setApplying(true);
      toast("CARE is stopping to apply your settings.");
    } catch (cause) {
      const problem = advancedProblem(cause, "The settings couldn't be applied",
        "Nothing was changed. Review your settings and try again.");
      setSaveProblem(problem);
      toast(problem.title + ". " + problem.message);
    } finally {
      pending.current = false;
      setSaving(false);
    }
  };
  const discard = () => {
    if (locked || lock.isLocked()) return;
    setDraft(initial);
    setCustom(customInitial);
    setSaveProblem(null);
    setNotice("");
  };
  const toggleEditing = () => {
    if (busy || saving || applying || lock.isLocked()) return;
    if (editing && changeCount) setDiscarding(true);
    else setEditing(!editing);
  };
  const patchCustom = (uid: number, patch: Partial<CustomRow>) => {
    if (disabled || lock.isLocked()) return;
    setCustom((current) => current.map((row) => row.uid === uid ? { ...row, ...patch } : row));
  };
  const sections = GROUPS.map((entry, index) => ({ ...entry, Icon: ICONS[index] }));
  const expandedContent = <>
    {sections.map(({ id, title, Icon, settings }) => <section key={id} className="advanced-settings-section"
      data-advanced-section={id} aria-labelledby={`advanced-section-${id}`}>
      <header className="advanced-section-head">
        <span className="advanced-icon"><Icon aria-hidden="true" /></span>
        <h3 className="advanced-section-title advanced-grow" id={`advanced-section-${id}`}>{title}</h3>
      </header>
      <div className="advanced-settings-card">
        {settings.map((setting) => <SettingRow key={setting.key} setting={setting} changed={dirtyKeys.includes(setting.key)} error={errors[setting.key]}>
          <SettingControl setting={visibleOptions(setting, files)} value={draft[setting.key]} disabled={disabled} invalid={!!errors[setting.key]}
            onChange={(value) => {
              if (disabled || lock.isLocked()) return;
              setNotice(""); setDraft((current) => ({ ...current, [setting.key]: value }));
            }} />
        </SettingRow>)}
      </div>
    </section>)}
    <section className="advanced-settings-section" data-advanced-section="other" aria-labelledby="advanced-section-other">
      <header className="advanced-section-head">
        <span className="advanced-icon"><Settings2 aria-hidden="true" /></span>
        <h3 className="advanced-section-title advanced-grow" id="advanced-section-other">Extra settings (for support)</h3>
      </header>
      <div className="advanced-custom-list">
        <p className="advanced-card-description">Values here override CARE's standard defaults. Only add or change them with help from your support person. Protected connection and online-service settings cannot be changed here.</p>
        {custom.length === 0 ? <p className="advanced-field-hint">No extra settings added.</p> : null}
        {custom.map((row) => <CustomRowView key={row.uid} row={row} error={errors[`custom:${row.uid}`]} disabled={disabled}
          onKeyChange={(key) => patchCustom(row.uid, { key, file: fileForKey(key.trim()) })}
          onValueChange={(value) => patchCustom(row.uid, { value })}
          onRemove={() => { if (!disabled && !lock.isLocked()) setCustom((current) => current.filter((entry) => entry.uid !== row.uid)); }} />)}
        <Button type="button" className="self-start" disabled={disabled} onClick={() => {
          if (!disabled && !lock.isLocked()) setCustom((current) =>
            [...current, { uid: take(), key: "", value: "", file: "backend", isNew: true }]);
        }}>Add setting</Button>
      </div>
    </section>
    <footer className="advanced-settings-foot">
      <p role="status">{saving ? "Checking your settings…" : applying ? "Applying settings — CARE is stopped, then started on the new settings. Staff need to wait." :
        "CARE is stopped while your changes are applied, then started again on the new settings. If it can't start, your previous settings come back."}</p>
      <div className="advanced-actions">
        <Button type="button" disabled={disabled || changeCount === 0} onClick={discard}>Discard</Button>
        <Button type="button" variant="primary" disabled={!canSave} onClick={() => void save()}>
          {saving || applying ? <Spinner /> : null}Save changes
        </Button>
      </div>
    </footer>
  </>;

  return <div className="advanced-env" data-editing={editing}>
    {restorePending ? <AdvancedNotice title="Finish the earlier restore first">
      Start CARE from Overview to recover the unfinished restore. Settings can be read, but not saved or applied yet.
    </AdvancedNotice> : null}
    {loading ? <div className="advanced-busy" role="status"><Spinner />Reading clinic settings…</div> : null}
    <AdvancedError problem={loadProblem} />
    {loadProblem ? <Button type="button" className="self-start" disabled={busy || loading} onClick={() => void load()}>Try reading settings again</Button> : null}
    <AdvancedError problem={saveProblem} />
    {notice ? <AdvancedNotice title={notice} tone="success" /> : null}

    <section className="advanced-card" aria-labelledby="advanced-settings-title">
      <header className="advanced-settings-head" data-expanded={editing}>
        <span className="advanced-icon"><Settings2 aria-hidden="true" /></span>
        <span className="advanced-grow">
          <h2 className="advanced-card-title" id="advanced-settings-title">
            <button type="button" ref={toggleRef} className="advanced-settings-toggle" data-advanced-settings-toggle
              aria-expanded={editing} aria-controls="advanced-settings-content" aria-describedby="advanced-settings-description"
              disabled={!files || loading || saving || applying || busy || lock.disabled}
              onClick={toggleEditing}>Clinic settings</button>
          </h2>
          <p className="advanced-card-description" id="advanced-settings-description">Everyday choices for your clinic, all in one place. Saving stops CARE, applies every change together, then starts it again, so staff will have to wait.</p>
        </span>
        {editing ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
      </header>
      <div id="advanced-settings-content" role="region" aria-labelledby="advanced-settings-title" hidden={!editing}>
        {editing ? expandedContent : null}
      </div>
    </section>

    <AlertDialog open={discarding} onOpenChange={setDiscarding}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onOpenAutoFocus={(event) => { event.preventDefault(); cancelRef.current?.focus(); }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          toggleRef.current?.focus();
        }}>
        <AlertDialogTitle>Discard unsaved settings?</AlertDialogTitle>
        <AlertDialogDescription>Your unsaved edits will be lost. The settings CARE is running on aren't changed.</AlertDialogDescription>
        <div className="advanced-dialog-foot">
          <Button type="button" ref={cancelRef} onClick={() => setDiscarding(false)}>Keep editing</Button>
          <Button type="button" disabled={locked} onClick={() => {
            if (locked || lock.isLocked()) return;
            discard(); setDiscarding(false); setEditing(false);
          }}>Discard changes</Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  </div>;
}

function SettingRow({ setting, changed, error, children }: {
  setting: Setting; changed: boolean; error?: string; children: ReactNode;
}) {
  return <div className="advanced-setting">
    <div>
      <div className="advanced-setting-label">{setting.label}{changed ? <span className="advanced-setting-changed" aria-label="Unsaved change" /> : null}</div>
      {setting.help ? <p className="advanced-setting-help">{setting.help}</p> : null}
    </div>
    <div className="advanced-setting-control">
      {children}
      {error ? <p className="advanced-field-error" role="alert">{error}</p> : null}
    </div>
  </div>;
}

function CustomRowView({ row, error, disabled, onKeyChange, onValueChange, onRemove }: {
  row: CustomRow; error?: string; disabled: boolean;
  onKeyChange: (key: string) => void; onValueChange: (value: string) => void; onRemove: () => void;
}) {
  const nameId = `advanced-custom-name-${row.uid}`;
  return <div className="advanced-custom-row">
    <div>
      <label htmlFor={nameId}>Setting name</label>
      <Input id={nameId} placeholder="SETTING_NAME" value={row.key} spellCheck={false} autoComplete="off"
        readOnly={!row.isNew} onChange={(event) => onKeyChange(event.target.value)} disabled={disabled}
        aria-invalid={!!error} className="font-mono" />
      <span className="advanced-custom-file">{row.file === "frontend" ? "CARE app setting" : "CARE server setting"}</span>
    </div>
    <AdvancedSecretInput label={`Value${row.key.trim() ? ` for ${row.key.trim()}` : ""}`} value={row.value}
      onChange={onValueChange} disabled={disabled} invalid={!!error} />
    <Button type="button" size="icon" className="advanced-outline-danger" disabled={disabled}
      aria-label={`Remove ${row.key.trim() || "new setting"}`} onClick={onRemove}><Trash2 aria-hidden="true" className="size-4" /></Button>
    {error ? <p className="advanced-field-error" role="alert">{error}</p> : null}
  </div>;
}

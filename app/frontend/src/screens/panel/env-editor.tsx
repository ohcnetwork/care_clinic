import {
  Archive, ChevronDown, ChevronRight, LockKeyhole,
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
import type { Section } from "@/types";
import {
  editableEnvLines, effectiveSetting, groupSummary, mergeEnvChanges, validateExtraSetting, validateSetting, type EnvDraft,
} from "./advanced-env";
import {
  AdvancedError, AdvancedNotice, AdvancedSecretInput, advancedProblem, useAdvancedLock, type AdvancedProblem,
} from "./advanced-ui";
import { SettingControl, splitList } from "./env-controls";
import { fileForKey, GROUPS, isHiddenKey, MANAGED_NOTES, SETTING_BY_KEY, SETTINGS, VISIT_OPTIONS, type Setting } from "./env-schema";

const SECTIONS: Section[] = ["backend", "frontend"];
const ICONS = [Palette, UserPlus, Receipt, Archive, LockKeyhole];
type Files = Record<Section, { text: string; lines: EnvLine[] }>;
type CustomRow = { uid: number; key: string; value: string; file: Section; isNew: boolean };

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

export function EnvEditor({ adminPassword, groupId, onGroupChange, onWorkingChange, pendingFiles, onPendingFilesChange, onApplicationAccepted }: {
  adminPassword: string;
  groupId?: string | null;
  onGroupChange?: (group: string | null) => void;
  onWorkingChange?: (working: boolean) => void;
  pendingFiles?: Section[];
  onPendingFilesChange?: (files: Section[]) => void;
  onApplicationAccepted?: () => void;
}) {
  const { busy, restorePending, runAction, operationError } = useCare();
  const lock = useAdvancedLock();
  const [localGroup, setLocalGroup] = useState<string | null>(null);
  const selected = groupId === undefined ? localGroup : groupId;
  const group = GROUPS.find((entry) => entry.id === selected);
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
  const [needsApply, setNeedsApply] = useState<Section[]>(pendingFiles ?? []);
  const [notice, setNotice] = useState("");
  const [discarding, setDiscarding] = useState(false);
  const nextUid = useRef(0);
  const pending = useRef(false);
  const loadVersion = useRef(0);
  const loadingRequest = useRef<{ password: string; promise: Promise<Files> } | null>(null);
  const savedFiles = useRef(new Set<Section>(pendingFiles ?? []));
  const cancelRef = useRef<HTMLButtonElement>(null);
  const previousGroup = useRef<string | null>(null);
  const pendingGroup = useRef<string | null>(null);
  const take = () => nextUid.current++;
  const editing = selected !== null;
  const working = busy || saving || applying || restorePending;
  const locked = working || lock.disabled;

  const changePage = (next: string | null) => {
    setLocalGroup(next);
    onGroupChange?.(next);
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
        "Try again when CARE Desktop is idle. No settings have been changed."));
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
  useEffect(() => {
    if (!pendingFiles || pending.current || applying) return;
    savedFiles.current = new Set(pendingFiles);
    setNeedsApply(pendingFiles);
  }, [pendingFiles, applying]);
  useEffect(() => {
    if (!applying || busy) return;
    setApplying(false);
    if (operationError) {
      setSaveProblem({ title: "Settings were saved, but applying them didn't finish",
        message: "Your changes are still saved. Check the log, then retry applying them." });
    } else {
      savedFiles.current.clear();
      setNeedsApply([]);
      onPendingFilesChange?.([]);
      setNotice("Settings applied.");
    }
  }, [applying, busy, operationError, onPendingFilesChange]);

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
    for (const setting of group?.settings ?? []) {
      if (!dirtyKeys.includes(setting.key)) continue;
      const error = validateSetting(setting, draft[setting.key]);
      if (error) out[setting.key] = error;
    }
    if (group?.id === "visits" && dirtyKeys.includes("REACT_DEFAULT_ENCOUNTER_TYPE") && files) {
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
      else if (isHiddenKey(key)) out[id] = MANAGED_NOTES[key] ?? "This setting is protected by CARE Desktop.";
      else if (SETTING_BY_KEY.has(key)) out[id] = `Change “${SETTING_BY_KEY.get(key)!.label}” in its settings group instead.`;
      else if ((names.get(`${row.file}:${key}`) ?? 0) > 1) out[id] = "This setting is listed twice.";
      else if (customDelta.changed.some((changed) => changed.uid === row.uid)) {
        const error = validateExtraSetting(key, row.value);
        if (error) out[id] = error;
      }
    }
    return out;
  }, [group, draft, custom, dirtyKeys, customDelta, files]);
  const changeCount = dirtyKeys.length + customDelta.changed.length + customDelta.removed.length;
  const hasErrors = Object.keys(errors).some((key) => selected === "other" ? key.startsWith("custom:") : !key.startsWith("custom:"));
  const disabled = locked || loading || !files;
  const canSave = !disabled && (changeCount > 0 || needsApply.length > 0) && !hasErrors;
  const touched = new Set([...dirtyKeys.map((key) => SETTING_BY_KEY.get(key)!.file),
    ...customDelta.changed.map((row) => row.file), ...customDelta.removed.map((row) => row.file), ...needsApply]);
  const frontendOnly = touched.size > 0 ? !touched.has("backend") : !!group && group.settings.every((setting) => setting.file === "frontend");

  const commitFile = (file: Section, text: string) => {
    const next = { text, lines: editableEnvLines(text) };
    setFiles((current) => current ? { ...current, [file]: next } : current);
    const values = initialDraft({ ...files!, [file]: next });
    const committed = Object.fromEntries(SETTINGS.filter((setting) => setting.file === file).map((setting) => [setting.key, values[setting.key]]));
    setInitial((current) => ({ ...current, ...committed }));
    setDraft((current) => ({ ...current, ...committed }));
    const rows = customRows({ ...files!, [file]: next }, take).filter((row) => row.file === file)
      .map((row) => ({ ...row, uid: custom.find((before) => before.file === file && before.key.trim() === row.key)?.uid ?? row.uid }));
    setCustomInitial((current) => [...current.filter((row) => row.file !== file), ...rows]);
    setCustom((current) => [...current.filter((row) => row.file !== file), ...rows]);
  };
  const save = async () => {
    if (!canSave || lock.isLocked() || pending.current || !files) return;
    const requireUnlocked = () => {
      if (lock.isLocked()) throw new Error("Advanced changes are locked while another task finishes.");
    };
    pending.current = true;
    setSaving(true);
    setSaveProblem(null);
    setNotice("");
    try {
      if (changeCount) {
        const fresh = await readFiles();
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
          if (text !== fresh[file].text) {
            requireUnlocked();
            await bridge.WriteEnv(file, text, adminPassword);
            savedFiles.current.add(file);
            setNeedsApply([...savedFiles.current]);
            onPendingFilesChange?.([...savedFiles.current]);
          }
          commitFile(file, text);
        }
      }
      if (!savedFiles.current.size) {
        setNotice("The saved settings already match these values.");
        return;
      }
      const action = savedFiles.current.has("backend") ? "start" : "rebuild-frontend";
      requireUnlocked();
      const accepted: unknown = await runAction(action, adminPassword);
      if (accepted !== true) {
        setSaveProblem({ title: "Settings were saved, but applying them didn't start",
          message: "Your changes are still saved. Wait for any other task to finish, then retry applying them." });
        return;
      }
      setApplying(true);
      onApplicationAccepted?.();
      toast("Settings saved. CARE is applying your changes.");
    } catch (cause) {
      const partial = savedFiles.current.size > 0;
      const problem = advancedProblem(cause, partial ? "Some settings were saved" : "The settings couldn't be saved",
        partial ? "CARE hasn't applied these changes. Retry saving the remaining changes, or discard them and apply the settings already saved."
          : "No changes were confirmed. Review your settings and try again.");
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
  const toggleGroup = (id: string) => {
    if (busy || saving || applying || lock.isLocked()) return;
    const next = selected === id ? null : id;
    if (changeCount) {
      previousGroup.current = id;
      pendingGroup.current = next;
      setDiscarding(true);
    } else changePage(next);
  };
  const patchCustom = (uid: number, patch: Partial<CustomRow>) => {
    if (disabled || lock.isLocked()) return;
    setCustom((current) => current.map((row) => row.uid === uid ? { ...row, ...patch } : row));
  };
  const categories = [
    ...GROUPS.map((entry, index) => ({
      id: entry.id, title: entry.title, Icon: ICONS[index],
      summary: files ? groupSummary(entry, initial) : "Settings haven't been read",
    })),
    { id: "other", title: "Extra settings (for support)", Icon: Settings2,
      summary: "Only open this if your CARE support person asks you to" },
  ];
  const expandedContent = <>
    <h3 className="advanced-expanded-title">{group?.title ?? "Extra settings (for support)"}</h3>
    {group ? <div className="advanced-settings-card">
      {group.settings.map((setting) => <SettingRow key={setting.key} setting={setting} changed={dirtyKeys.includes(setting.key)} error={errors[setting.key]}>
        <SettingControl setting={setting} value={draft[setting.key]} disabled={disabled} invalid={!!errors[setting.key]}
          onChange={(value) => {
            if (disabled || lock.isLocked()) return;
            setNotice(""); setDraft((current) => ({ ...current, [setting.key]: value }));
          }} />
      </SettingRow>)}
    </div> : <div className="advanced-custom-list">
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
    </div>}
    <footer className="advanced-settings-foot">
      <p role="status">{saving ? "Saving settings…" : applying ? "Applying settings — staff may need to wait." :
        needsApply.length > 0 && !changeCount ? "Saved changes still need to be applied." :
          frontendOnly ? "CARE may be unavailable for a few minutes while changes are applied." : "CARE may be unavailable for about a minute while changes are applied."}</p>
      <div className="advanced-actions">
        <Button type="button" disabled={disabled || changeCount === 0} onClick={discard}>Discard</Button>
        <Button type="button" variant="primary" disabled={!canSave} onClick={() => void save()}>
          {saving || applying ? <Spinner /> : null}{needsApply.length > 0 && !changeCount ? "Apply saved changes" : "Save changes"}
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
    {needsApply.length > 0 && !applying && !saveProblem ? <AdvancedNotice title="Saved changes still need to be applied" tone="neutral">
      {changeCount ? "Save the remaining changes when you're ready." : "Apply the saved settings when staff are ready for a short interruption."}
    </AdvancedNotice> : null}

    <section className="advanced-card" aria-labelledby="advanced-settings-title">
      <header className="advanced-groups-head">
        <h2 className="advanced-card-title" id="advanced-settings-title">Clinic settings</h2>
        <p className="advanced-card-description">Everyday choices for your clinic. Applying changes may briefly interrupt staff using CARE.</p>
      </header>
      {categories.map(({ id, title, summary, Icon }) => {
        const expanded = selected === id;
        return <div key={id} className="advanced-group-item">
          <button type="button" className="advanced-group" data-advanced-group={id}
            id={`advanced-group-${id}`} aria-expanded={expanded} aria-controls={`advanced-group-content-${id}`}
            disabled={!files || loading || saving || applying || busy || lock.disabled}
            onClick={() => toggleGroup(id)}>
            <span className="advanced-icon"><Icon aria-hidden="true" /></span>
            <span className="advanced-grow"><strong>{title}</strong><span className="advanced-group-summary">{summary}</span></span>
            {expanded ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
          </button>
          <div id={`advanced-group-content-${id}`} role="region" aria-labelledby={`advanced-group-${id}`} hidden={!expanded}>
            {expanded ? expandedContent : null}
          </div>
        </div>;
      })}
    </section>
    {!editing && needsApply.length > 0 ? <Button type="button" className="self-start" disabled={!canSave} onClick={() => void save()}>Apply saved changes</Button> : null}

    <AlertDialog open={discarding} onOpenChange={setDiscarding}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onOpenAutoFocus={(event) => { event.preventDefault(); cancelRef.current?.focus(); }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          document.querySelector<HTMLButtonElement>(`[data-advanced-group="${previousGroup.current}"]`)?.focus();
        }}>
        <AlertDialogTitle>Discard unsaved settings?</AlertDialogTitle>
        <AlertDialogDescription>Your unsaved edits will be lost. Settings already saved to this computer aren't undone.</AlertDialogDescription>
        <div className="advanced-dialog-foot">
          <Button type="button" ref={cancelRef} onClick={() => setDiscarding(false)}>Keep editing</Button>
          <Button type="button" disabled={locked} onClick={() => {
            if (locked || lock.isLocked()) return;
            discard(); setDiscarding(false); changePage(pendingGroup.current);
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

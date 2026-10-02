import { Hammer, LockKeyhole, LockKeyholeOpen, ScrollText, Trash2, TriangleAlert } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";
import { bridge } from "@/lib/bridge";
import { useCare } from "@/state/care-store";
import type { Section } from "@/types";
import { AdminPasswordForm, AdminRecoverySettings } from "./admin-recovery";
import {
  AdvancedError, AdvancedLockProvider, AdvancedLogButton, AdvancedNotice, AdvancedSecretInput, advancedProblem, useAdvancedLock, type AdvancedProblem,
} from "./advanced-ui";
import { EnvEditor } from "./env-editor";
import { GROUPS } from "./env-schema";
import { PanelPageHeader } from "./panel-ui";

const UNLOCK_MS = 15 * 60 * 1000;

export function AdvancedTab({ disabled = false }: { disabled?: boolean } = {}) {
  return <AdvancedLockProvider disabled={disabled}><AdvancedContent /></AdvancedLockProvider>;
}

function AdvancedContent() {
  const { disabled } = useAdvancedLock();
  const { tab, flow, busy, restorePending, operationError } = useCare();
  const active = flow === "panel" && tab === "advanced";
  const [adminPassword, setAdminPassword] = useState<string | null>(null);
  const [unlockedAt, setUnlockedAt] = useState(0);
  const [groupId, setGroupId] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  const [expired, setExpired] = useState(false);
  const [pendingSettings, setPendingSettings] = useState<Section[]>([]);
  const [applyingSettings, setApplyingSettings] = useState(false);
  const headingRef = useRef<HTMLDivElement>(null);
  const unlock = (password: string) => {
    setAdminPassword(password);
    setUnlockedAt(Date.now());
    setExpired(false);
    setGroupId(null);
  };
  const lock = () => {
    setAdminPassword(null);
    setGroupId(null);
    setWorking(false);
  };
  useEffect(() => {
    if (active) return;
    setAdminPassword(null);
    setGroupId(null);
    setWorking(false);
    setExpired(false);
  }, [active]);
  useEffect(() => {
    if (!adminPassword || !active) return;
    const timer = window.setTimeout(() => {
      setAdminPassword(null);
      setGroupId(null);
      setWorking(false);
      setExpired(true);
    }, Math.max(0, unlockedAt + UNLOCK_MS - Date.now()));
    return () => window.clearTimeout(timer);
  }, [adminPassword, active, unlockedAt]);
  useEffect(() => {
    if (active && adminPassword !== null) headingRef.current?.focus();
  }, [active, adminPassword, groupId]);
  useEffect(() => {
    if (!applyingSettings || busy) return;
    setApplyingSettings(false);
    if (!operationError) setPendingSettings([]);
  }, [applyingSettings, busy, operationError]);
  if (!active) return null;
  const group = GROUPS.find((entry) => entry.id === groupId);
  return <div className="care-advanced" data-page={groupId ? "group" : "overview"} data-panel-blocked={disabled} tabIndex={-1}>
    <div className="advanced-heading" ref={headingRef} tabIndex={-1}>
      {groupId ? <div className="advanced-kicker">Advanced · Clinic settings</div> : null}
      <PanelPageHeader title={group?.title ?? (groupId === "other" ? "Extra settings (for support)" : "Advanced")}
        subtitle={group?.summary ?? (groupId === "other" ? "Only use this page with help from your CARE support person."
          : "Clinic preferences and tools for your administrator.")}>
        {adminPassword !== null ? <div className="advanced-unlocked">
          <span>Locks after 15 minutes</span>
          <Button type="button" variant="ghost" size="sm" aria-label="Lock Advanced settings"
            title="Lock Advanced and clear passwords and unsaved settings" disabled={disabled || busy || working} onClick={lock}>
            <LockKeyholeOpen aria-hidden="true" />Unlocked · Lock
          </Button>
        </div> : undefined}
      </PanelPageHeader>
    </div>
    {adminPassword === null ? <>
      {expired ? <AdvancedNotice title="Advanced has locked" tone="neutral">Enter the Desktop password again. Unsaved settings and sensitive fields have been cleared.</AdvancedNotice> : null}
      <AdminGate onUnlock={unlock} />
    </> : <>
      {!groupId ? <AdminRecoverySettings adminPassword={adminPassword} onPasswordChanged={unlock} /> : null}
      <EnvEditor adminPassword={adminPassword} groupId={groupId} onGroupChange={setGroupId} onWorkingChange={setWorking}
        pendingFiles={pendingSettings} onPendingFilesChange={setPendingSettings} onApplicationAccepted={() => setApplyingSettings(true)} />
      {!groupId ? <>
        <LogRow />
        <section className="advanced-card advanced-card-pad advanced-danger" aria-labelledby="advanced-careful-title">
          <h2 className="advanced-danger-kicker" id="advanced-careful-title">Careful</h2>
          {restorePending ? <AdvancedNotice title="Finish the earlier restore first">
            Start CARE from Overview to recover it before rebuilding or removing this installation.
          </AdvancedNotice> : null}
          <div className="advanced-danger-row">
            <div className="advanced-grow">
              <h3>Rebuild everything</h3>
              <p className="advanced-card-description">Re-creates CARE from this app's files. Patient data is kept; CARE is unavailable for a few minutes.</p>
            </div>
            <RebuildControl adminPassword={adminPassword} />
          </div>
          <div className="advanced-danger-row">
            <div className="advanced-grow">
              <h3>Remove CARE from this computer</h3>
              <p className="advanced-card-description">Deletes the clinic and all patient data here. Backups can be kept.</p>
            </div>
            <UninstallPanel adminPassword={adminPassword} />
          </div>
        </section>
      </> : null}
    </>}
  </div>;
}

export function AdminGate({ onUnlock }: { onUnlock: (password: string) => void }) {
  const { busy } = useCare();
  const lock = useAdvancedLock();
  const blocked = busy || lock.disabled;
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const [checking, setChecking] = useState(false);
  const [recovering, setRecovering] = useState(false);
  const pending = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => { live.current = false; };
  }, []);
  const unlock = async () => {
    if (pending.current || busy || lock.isLocked()) return;
    if (!password) {
      setProblem({ title: "Enter the Desktop admin password", message: "This is the password created for CARE Desktop, not necessarily your CARE web password." });
      return;
    }
    pending.current = true;
    setChecking(true);
    setProblem(null);
    const submitted = password;
    try {
      const matches = await bridge.VerifyAdminPassword(submitted);
      if (!live.current) return;
      setPassword("");
      if (lock.isLocked()) return;
      if (matches) onUnlock(submitted);
      else setProblem({ title: "That's not the Desktop admin password", message: "Try again, or use an unused recovery code from your latest sheet." });
    } catch (cause) {
      if (!live.current) return;
      setPassword("");
      setProblem(advancedProblem(cause, "The password couldn't be checked", "Try again when CARE Desktop is ready. Advanced stays locked."));
    } finally {
      pending.current = false;
      if (live.current) setChecking(false);
    }
  };
  if (recovering) return <section className="advanced-card advanced-gate-recovery">
    <h2>Reset the Desktop admin password</h2>
    <AdminPasswordForm onCancel={() => { setRecovering(false); setProblem(null); }} onSuccess={(next) => {
      toast("Desktop password reset. Mark that recovery code used. Your CARE web login is unchanged.");
      onUnlock(next);
    }} />
  </section>;
  return <section className="advanced-card advanced-gate" aria-labelledby="advanced-gate-title">
    <span className="advanced-icon advanced-gate-icon"><LockKeyhole aria-hidden="true" /></span>
    <h2 id="advanced-gate-title">Enter the admin password</h2>
    <p>These settings can change, rebuild or remove the clinic.</p>
    <form noValidate onSubmit={(event) => { event.preventDefault(); void unlock(); }}>
      <AdvancedSecretInput label="Desktop admin password" value={password} onChange={(next) => { setPassword(next); setProblem(null); }}
        placeholder="Desktop admin password" disabled={blocked || checking} invalid={!!problem} autoFocus />
      <AdvancedError problem={problem} />
      <Button type="submit" variant="primary" size="block" className="advanced-gate-submit" disabled={blocked || checking}>
        {checking ? <Spinner /> : null}{checking ? "Checking…" : "Unlock"}
      </Button>
    </form>
    <Button type="button" variant="ghost" className="advanced-forgot" disabled={blocked || checking}
      onClick={() => { if (!lock.isLocked() && !busy && !pending.current) { setPassword(""); setProblem(null); setRecovering(true); } }}>
      {problem ? "Use a recovery code instead" : "Forgot Desktop password?"}
    </Button>
  </section>;
}

function RebuildControl({ adminPassword }: { adminPassword: string }) {
  const { busy, restorePending, runAction } = useCare();
  const lock = useAdvancedLock();
  const blocked = busy || lock.disabled;
  const [open, setOpen] = useState(false);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const pending = useRef(false);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const close = () => { if (!pending.current) { setOpen(false); setProblem(null); } };
  const rebuild = async () => {
    if (busy || lock.isLocked() || restorePending || pending.current) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    try {
      const accepted: unknown = await runAction("rebuild-all", adminPassword);
      if (accepted === true) setOpen(false);
      else setProblem({ title: "The rebuild didn't start", message: "Wait for any other task to finish, then try again. Open the log file if it still won't start." });
    } catch (cause) {
      setProblem(advancedProblem(cause, "The rebuild didn't start", "Try again when CARE Desktop is ready."));
    } finally {
      pending.current = false;
      setWorking(false);
    }
  };
  return <>
    <Button type="button" ref={triggerRef} className="advanced-outline-danger" disabled={blocked || restorePending || working}
      onClick={() => { if (!lock.isLocked() && !busy && !restorePending && !pending.current) setOpen(true); }}>
      <Hammer aria-hidden="true" className="size-4" />Rebuild
    </Button>
    <AlertDialog open={open} onOpenChange={(next) => { if (!next) close(); }}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onOpenAutoFocus={(event) => { event.preventDefault(); cancelRef.current?.focus(); }}
        onCloseAutoFocus={(event) => { event.preventDefault(); triggerRef.current?.focus(); }}
        onEscapeKeyDown={(event) => { if (working) event.preventDefault(); }}>
        <span className="advanced-icon advanced-icon-danger"><Hammer aria-hidden="true" /></span>
        <AlertDialogTitle className="advanced-dialog-title">Rebuild CARE?</AlertDialogTitle>
        <AlertDialogDescription>CARE will be unavailable for a few minutes while it is re-created from this app's files and current settings. Patient data and backups are kept.</AlertDialogDescription>
        <div className="advanced-dialog-body"><AdvancedError problem={problem} /></div>
        <div className="advanced-dialog-foot">
          <Button type="button" ref={cancelRef} disabled={working} onClick={close}>Cancel</Button>
          <Button type="button" variant="destructive" disabled={blocked || restorePending || working} onClick={() => void rebuild()}>
            {working ? <Spinner /> : <Hammer aria-hidden="true" className="size-4" />}Rebuild CARE
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

export function UninstallPanel({ adminPassword }: { adminPassword: string }) {
  const { busy, restorePending, uninstall, clearOperationError } = useCare();
  const lock = useAdvancedLock();
  const blocked = busy || lock.disabled;
  const [open, setOpen] = useState(false);
  const [removeBackups, setRemoveBackups] = useState(false);
  const [removeImages, setRemoveImages] = useState(false);
  const [removeRancher, setRemoveRancher] = useState(false);
  const [removeApp, setRemoveApp] = useState(false);
  const [capabilities, setCapabilities] = useState<{ rancher: boolean; app: boolean } | null>(null);
  const [checking, setChecking] = useState(false);
  const [working, setWorking] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const pending = useRef(false);
  const checkVersion = useRef(0);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const readOptions = async () => {
    if (busy || lock.isLocked()) return;
    const version = ++checkVersion.current;
    setChecking(true);
    setProblem(null);
    setCapabilities(null);
    try {
      const [rancher, app] = await Promise.all([bridge.RancherDesktopInstalled(), bridge.CanRemoveApp()]);
      if (version === checkVersion.current) setCapabilities({ rancher, app });
    } catch (cause) {
      if (version === checkVersion.current) setProblem(advancedProblem(cause, "Removal options couldn't be checked",
        "No removal has started. Try checking again before continuing."));
    } finally {
      if (version === checkVersion.current) setChecking(false);
    }
  };
  const clear = () => {
    setConfirmation("");
    setRemoveBackups(false);
    setRemoveImages(false);
    setRemoveRancher(false);
    setRemoveApp(false);
    setCapabilities(null);
    setProblem(null);
  };
  const close = () => {
    if (pending.current) return;
    checkVersion.current++;
    setOpen(false);
    setChecking(false);
    clear();
  };
  useEffect(() => () => { checkVersion.current++; }, []);
  const begin = () => {
    if (busy || lock.isLocked() || restorePending || pending.current) return;
    clear();
    setOpen(true);
    void readOptions();
  };
  const canRemove = !blocked && !restorePending && !working && !checking && !!capabilities && confirmation === "DELETE";
  const remove = async () => {
    if (!canRemove || lock.isLocked() || pending.current || !capabilities) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    clearOperationError();
    try {
      const accepted: unknown = await uninstall(removeImages, removeBackups, capabilities.rancher && removeRancher,
        adminPassword, capabilities.app && removeApp);
      setConfirmation("");
      if (accepted === true) { setOpen(false); clear(); }
      else setProblem({ title: "Removal didn't start", message: "No removal was confirmed. Check the log, then type DELETE again if you want to retry." });
    } catch (cause) {
      setConfirmation("");
      setProblem(advancedProblem(cause, "Removal didn't start", "Check the log, then type DELETE again if you want to retry."));
    } finally {
      pending.current = false;
      setWorking(false);
    }
  };
  return <>
    <Button type="button" ref={triggerRef} className="advanced-outline-danger" disabled={blocked || restorePending || working} onClick={begin}>
      <Trash2 aria-hidden="true" className="size-4" />Uninstall…
    </Button>
    <AlertDialog open={open} onOpenChange={(next) => { if (!next) close(); }}>
      <AlertDialogContent className="advanced-dialog advanced-removal-dialog"
        onOpenAutoFocus={(event) => { event.preventDefault(); cancelRef.current?.focus(); }}
        onCloseAutoFocus={(event) => { event.preventDefault(); triggerRef.current?.focus(); }}
        onEscapeKeyDown={(event) => { if (working) event.preventDefault(); }}>
        <span className="advanced-icon advanced-icon-danger"><Trash2 aria-hidden="true" /></span>
        <AlertDialogTitle className="advanced-dialog-title">Remove CARE and all patient data from this computer?</AlertDialogTitle>
        <AlertDialogDescription>This deletes the clinic and everything in it. Backups are kept unless you tick the box below.</AlertDialogDescription>
        <div className="advanced-dialog-body">
          {checking ? <div className="advanced-busy" role="status"><Spinner />Checking removal options…</div> : null}
          <AdvancedError problem={problem} />
          {!checking && !capabilities ? <Button type="button" className="self-start" disabled={working || blocked} onClick={() => void readOptions()}>Check removal options again</Button> : null}
          {restorePending ? <AdvancedNotice title="Finish the earlier restore first">
            Start CARE from Overview before removing this installation.
          </AdvancedNotice> : null}
          <div className="advanced-removal-options">
            <label className="advanced-check">
              <Checkbox checked={removeBackups} disabled={working || blocked || checking}
                onCheckedChange={(checked) => setRemoveBackups(checked === true)} />
              <span>Also delete the backups — nothing can be recovered afterwards</span>
            </label>
            <label className="advanced-check">
              <Checkbox checked={removeImages} disabled={working || blocked || checking}
                onCheckedChange={(checked) => setRemoveImages(checked === true)} />
              <span>Also remove downloaded images and clear the shared build cache
                <small>Other projects on this computer may need to download or build their files again.</small>
              </span>
            </label>
            {capabilities?.rancher ? <label className="advanced-check">
              <Checkbox checked={removeRancher} disabled={working || blocked}
                onCheckedChange={(checked) => setRemoveRancher(checked === true)} />
              <span>Also remove Rancher Desktop and its settings
                <small>Leave this off if other programs on this computer use Rancher Desktop.</small>
              </span>
            </label> : null}
            {capabilities?.app ? <label className="advanced-check">
              <Checkbox checked={removeApp} disabled={working || blocked} onCheckedChange={(checked) => setRemoveApp(checked === true)} />
              <span>Also remove the CARE Desktop app</span>
            </label> : null}
          </div>
          {!removeBackups ? <p className="advanced-field-hint">Keep your separately saved backup recovery file. You will need it to restore encrypted backups.</p> : null}
          <div className="advanced-confirm">
            <TriangleAlert aria-hidden="true" />
            <div className="advanced-field">
              <label htmlFor="advanced-confirm-delete">Type DELETE to confirm</label>
              <Input id="advanced-confirm-delete" value={confirmation} onChange={(event) => setConfirmation(event.target.value)}
                autoComplete="off" spellCheck={false} placeholder="DELETE" disabled={working || blocked || checking} />
            </div>
          </div>
        </div>
        <div className="advanced-dialog-foot">
          <Button type="button" ref={cancelRef} disabled={working} onClick={close}>Cancel</Button>
          <Button type="button" variant="destructive" disabled={!canRemove} onClick={() => void remove()}>
            {working ? <Spinner /> : <Trash2 aria-hidden="true" className="size-4" />}
            {working ? "Starting removal…" : "Delete everything"}
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

function LogRow() {
  return <section className="advanced-card advanced-card-pad">
    <div className="advanced-card-row">
      <span className="advanced-icon"><ScrollText aria-hidden="true" /></span>
      <div className="advanced-grow">
        <h2 className="advanced-card-title">Log file</h2>
        <p className="advanced-card-description">Technical details of what CARE Desktop does — share it when asking for help.</p>
      </div>
      <AdvancedLogButton label="Open log folder" />
    </div>
  </section>;
}

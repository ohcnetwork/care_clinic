import { FileKey } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import { AdvancedSecretInput } from "./advanced-ui";
import { BackupNotice } from "./backup-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

function exportProblem(cause: unknown): string {
  const detail = errorText(cause);
  if (/admin password does not match/i.test(detail)) return "The CARE Clinic admin password didn't match. Enter it again; your CARE web password may be different.";
  if (/existing backup recovery file is unavailable/i.test(detail)) return "No usable local key is enrolled, and the original PEM is missing or unreadable. Connect its drive or select another saved copy. CARE cannot reconstruct a key lost before enrollment.";
  if (/encrypted backup key could not be unlocked/i.test(detail)) return "The encrypted local key couldn't be unlocked or verified. Select a surviving recovery PEM to re-enroll it. Your backup key has not changed.";
  if (/PEM was exported, but its encrypted local copy/i.test(detail)) return "The PEM was exported, but its encrypted local copy could not be saved. Keep that exported file safe and retry enrollment; password-only downloads are not yet confirmed.";
  if (/installed backup encryption key/i.test(detail)) return "The installed backup encryption key couldn't be verified. No file was exported. Ask your support person to check this installation.";
  if (/does not match|different clinic/i.test(detail)) return "That recovery file doesn't match this clinic. Select a saved copy of this clinic's original key.";
  if (/keep recovery materials outside/i.test(detail)) return "Choose a secure location outside CARE's installation, settings, logs and backup folder.";
  if (/choose a new filename|file exists/i.test(detail)) return "CARE won't overwrite an existing file. Choose a new filename or location.";
  if (/something else is still running|closing/i.test(detail)) return "CARE Clinic is busy. Wait for the current task to finish and try again.";
  if (/restore is unfinished/i.test(detail)) return "Finish the earlier restore first. Open Overview and start CARE before exporting the key.";
  if (/server role|not set up|cleanup is incomplete/i.test(detail)) return "This clinic isn't ready. Finish setup or cleanup before exporting its backup key.";
  return "The recovery file couldn't be saved or checked. Check the file and drive permissions, then try again. Your backup key has not changed.";
}

export function ExportBackupRecovery({ disabled }: { disabled: boolean }) {
  const { log } = useCare();
  const updateLock = usePanelUpdateLock();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [source, setSource] = useState("");
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState("");
  const [notice, setNotice] = useState("");
  const [stored, setStored] = useState<boolean | null>(null);
  const [needsEnrollment, setNeedsEnrollment] = useState(false);
  const pending = useRef(false);
  const mounted = useRef(true);
  const blocked = useRef(disabled);
  blocked.current = disabled || updateLock.active;

  useEffect(() => {
    mounted.current = true;
    const off = onCareEvent("app-update-progress", () => { setPassword(""); setSource(""); });
    return () => { mounted.current = false; off(); };
  }, []);

  const close = () => {
    if (pending.current) return;
    setOpen(false);
    setPassword("");
    setSource("");
    setProblem("");
    setNotice("");
  };
  const show = async () => {
    if (pending.current || blocked.current || updateLock.isActive()) return;
    const revision = updateLock.revision();
    pending.current = true;
    setWorking(true);
    setPassword(""); setSource(""); setProblem(""); setNotice(""); setStored(null);
    try {
      const status = await bridge.GetSetupRecoveryStatus();
      if (!mounted.current || blocked.current || revision !== updateLock.revision() || updateLock.isActive()) return;
      setStored(status.backup_key_stored);
      setNeedsEnrollment(status.backup_key_needs_enrollment);
      setOpen(true);
    } catch (cause) {
      if (mounted.current) {
        log(`backup key status: ${errorText(cause)}`);
        setProblem("The local backup key status couldn't be checked. Close this dialog and try again.");
        setOpen(true);
      }
    } finally {
      pending.current = false;
      if (mounted.current) setWorking(false);
    }
  };
  const chooseSource = async () => {
    if (pending.current || blocked.current || updateLock.isActive()) return;
    const revision = updateLock.revision();
    pending.current = true;
    setWorking(true);
    setProblem("");
    try {
      const path = await bridge.ChooseRecoveryFile();
      if (path && mounted.current && !blocked.current && revision === updateLock.revision() && !updateLock.isActive()) setSource(path);
    } catch (cause) {
      if (mounted.current) {
        log(`backup key selection: ${errorText(cause)}`);
        setProblem(exportProblem(cause));
      }
    } finally {
      pending.current = false;
      if (mounted.current) setWorking(false);
    }
  };
  const save = async () => {
    if (!password || stored === null || pending.current || blocked.current || updateLock.isActive()) return;
    pending.current = true;
    setWorking(true);
    setProblem("");
    setNotice("");
    const submittedPassword = password;
    setPassword("");
    try {
      const saved = await bridge.ExportBackupRecovery(submittedPassword, source);
      if (!mounted.current) return;
      if (saved) {
        setOpen(false);
        setSource("");
        setNotice("Backup recovery file saved. Its encrypted local copy supports password-only downloads. The key is unchanged: existing backups still use the same recovery file.");
      } else {
        setNotice("No file was saved. Your backup key has not changed.");
      }
    } catch (cause) {
      if (mounted.current) {
        log(`backup key export: ${errorText(cause)}`);
        setProblem(exportProblem(cause));
      }
    } finally {
      pending.current = false;
      if (mounted.current) setWorking(false);
    }
  };

  return <>
    <section className="care-backups-card care-backups-card-row" aria-label="Backup recovery key">
      <span className="care-backups-icon"><FileKey aria-hidden="true" /></span>
      <div className="care-backups-grow">
        <h2>Backup recovery key</h2>
        <p>Re-download your key with your CARE Clinic admin password. Older installations need a surviving PEM for one-time encrypted enrollment.</p>
      </div>
      <Button disabled={disabled || working || updateLock.active} onClick={() => void show()}>Re-download backup key</Button>
    </section>
    {notice && !open ? <BackupNotice title={notice} tone="success" log={false} /> : null}
    <AlertDialog open={open} onOpenChange={(next) => { if (!next) close(); }}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onEscapeKeyDown={(event) => { if (pending.current) event.preventDefault(); }}>
        <AlertDialogTitle>Re-download backup key</AlertDialogTitle>
        <AlertDialogDescription>
          CARE unlocks its encrypted local copy using your CARE Clinic admin password, without replacing the key.
          Keep an off-device PEM: a lost computer or forgotten-password reset can make the local copy unusable.
        </AlertDialogDescription>
        <form className="advanced-dialog-body" onSubmit={(event) => { event.preventDefault(); void save(); }}>
          <AdvancedSecretInput label="CARE Clinic admin password" value={password} onChange={setPassword}
            disabled={disabled || working || updateLock.active} autoComplete="current-password" autoFocus />
          <div className="advanced-field">
            <Button type="button" disabled={disabled || working || updateLock.active} onClick={() => void chooseSource()}>Select another saved copy</Button>
            <p className="advanced-path">{source || (stored ? "Using the password-encrypted local key; the original PEM is not needed." : "Using the original saved recovery file location for enrollment.")}</p>
          </div>
          {stored !== null && (!stored || source) ? <BackupNotice title={needsEnrollment ? "Re-enroll after your password reset" : "Enable password-only downloads"} tone="info" log={false}>
            When you save, CARE will retain a local copy encrypted with this password.
            Select a surviving matching PEM if the original is unavailable. Keys lost before enrollment cannot be reconstructed.
          </BackupNotice> : null}
          {problem ? <BackupNotice title="The backup key couldn't be exported">{problem}</BackupNotice> : null}
          {notice ? <BackupNotice title={notice} tone="info" log={false} /> : null}
          <div className="advanced-dialog-foot">
            <Button type="button" disabled={working} onClick={close}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={!password || stored === null || disabled || working || updateLock.active}>
              {working ? "Working…" : "Choose where to save"}
            </Button>
          </div>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

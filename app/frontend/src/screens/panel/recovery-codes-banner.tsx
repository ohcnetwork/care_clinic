import { KeyRound } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import { AdvancedError, AdvancedNotice, AdvancedSecretInput, advancedProblem, type AdvancedProblem } from "./advanced-ui";
import { PanelLogButton } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

export function RecoveryCodesBanner({ disabled }: { disabled: boolean }) {
  const { log } = useCare();
  const updateLock = usePanelUpdateLock();
  const [remaining, setRemaining] = useState<number | null>(null);
  const [countError, setCountError] = useState(false);
  const [checking, setChecking] = useState(false);
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [working, setWorking] = useState(false);
  const [cancelled, setCancelled] = useState(false);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const generation = useRef(0);
  const pending = useRef(false);
  const live = useRef(false);
  const blocked = useRef(disabled);
  blocked.current = disabled;

  const reload = useCallback(async () => {
    const request = ++generation.current;
    setChecking(true);
    try {
      const count = await bridge.GetAdminRecoveryCodeCount();
      if (request !== generation.current) return;
      setRemaining(count);
      setCountError(false);
    } catch (cause) {
      if (request !== generation.current) return;
      log(`recovery code count: ${errorText(cause)}`);
      setCountError(true);
    } finally {
      if (request === generation.current) setChecking(false);
    }
  }, [log]);

  useEffect(() => {
    live.current = true;
    const off = onCareEvent("admin-recovery-codes-changed", (count: number) => {
      generation.current++;
      setRemaining(count);
      setCountError(false);
      setChecking(false);
    });
    void reload();
    return () => { live.current = false; generation.current++; off(); };
  }, [reload]);
  useEffect(() => {
    if (disabled || updateLock.active) setPassword("");
  }, [disabled, updateLock.active]);

  const close = () => {
    if (pending.current) return;
    setOpen(false);
    setPassword("");
    setProblem(null);
    setCancelled(false);
  };
  const save = async () => {
    if (!password || pending.current || blocked.current || updateLock.isActive()) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    setCancelled(false);
    const submitted = password;
    setPassword("");
    try {
      const saved = await bridge.SaveAdminRecoveryCodes(submitted, "");
      if (!live.current) return;
      if (!saved) {
        setCancelled(true);
        return;
      }
      generation.current++;
      setRemaining(6);
      setCountError(false);
      setChecking(false);
      setOpen(false);
      toast("Six new recovery codes saved. All old codes are now invalid. Keep the new sheet somewhere secure.");
    } catch (cause) {
      if (!live.current) return;
      const error = advancedProblem(cause, "The new recovery codes couldn't be saved",
        "Your previous codes have not been replaced. Try saving again.");
      if (error.title === "The Desktop admin password didn't match") {
        error.message = "Enter your Desktop admin password again. Your CARE web password may be different.";
      }
      setProblem(error);
    } finally {
      pending.current = false;
      if (live.current) setWorking(false);
    }
  };
  const locked = disabled || working || updateLock.active;

  return <>
    {countError ? <div className="panel-banner panel-tone-warning" role="status">
      <KeyRound aria-hidden="true" />
      <div className="panel-grow">The number of unused recovery codes couldn't be checked.</div>
      <Button size="sm" disabled={locked || checking} onClick={() => void reload()}>Check again</Button>
      <PanelLogButton />
    </div> : null}
    {remaining !== null && remaining <= 2 ? <div
      className={`panel-banner panel-tone-${remaining === 0 ? "danger" : "warning"}`}
      role={remaining === 0 ? "alert" : "status"} aria-label="Recovery codes reminder">
      <KeyRound aria-hidden="true" />
      <div className="panel-grow">
        <strong>{remaining === 0 ? "No recovery codes left." : `Only ${remaining} recovery ${remaining === 1 ? "code" : "codes"} left.`} </strong>
        Save a new set so you can reset your Desktop password if you forget it.
      </div>
      <Button size="sm" disabled={locked} onClick={() => {
        if (pending.current || blocked.current || updateLock.isActive()) return;
        setPassword(""); setProblem(null); setCancelled(false); setOpen(true);
      }}>Save new recovery codes</Button>
    </div> : null}
    <AlertDialog open={open} onOpenChange={(next) => { if (!next) close(); }}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onEscapeKeyDown={(event) => { if (pending.current) event.preventDefault(); }}>
        <AlertDialogTitle>Save new recovery codes</AlertDialogTitle>
        <AlertDialogDescription>
          Enter your Desktop admin password to save six new recovery codes.
          A successfully saved set replaces all old codes, including unused ones.
        </AlertDialogDescription>
        <form className="advanced-dialog-body" onSubmit={(event) => { event.preventDefault(); void save(); }}>
          <AdvancedSecretInput label="Desktop admin password" value={password} onChange={setPassword}
            disabled={locked} autoComplete="current-password" autoFocus />
          <p>Keep the new sheet somewhere secure, preferably away from this computer. These codes reset the Desktop password; they do not unlock backups.</p>
          {cancelled ? <AdvancedNotice title="No new recovery codes were saved" tone="neutral">
            Your existing unused codes still work. Enter your password again to retry.
          </AdvancedNotice> : null}
          <AdvancedError problem={problem} />
          <div className="advanced-dialog-foot">
            <Button type="button" disabled={working} onClick={close}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={locked || !password}>
              {working ? <Spinner /> : null}{working ? "Saving codes…" : "Choose where to save"}
            </Button>
          </div>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

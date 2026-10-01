import { Check, KeyRound, Printer } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { usePasswordStrength } from "@/hooks/use-password-strength";
import { bridge } from "@/lib/bridge";
import { useCare } from "@/state/care-store";
import {
  AdvancedError, AdvancedNotice, AdvancedSecretInput, advancedProblem, useAdvancedLock, type AdvancedProblem,
} from "./advanced-ui";

export function AdminPasswordForm({
  currentPassword, onSuccess, onCancel, onWorkingChange,
}: {
  currentPassword?: string;
  onSuccess: (password: string) => void;
  onCancel: () => void;
  onWorkingChange?: (working: boolean) => void;
}) {
  const { busy } = useCare();
  const lock = useAdvancedLock();
  const id = useId();
  const live = useRef(true);
  const pending = useRef(false);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const [retryAt, setRetryAt] = useState(0);
  const [now, setNow] = useState(Date.now());
  const strength = usePasswordStrength(password);
  const recovering = currentPassword === undefined;
  const waitSeconds = Math.max(0, Math.ceil((retryAt - now) / 1000));
  const locked = busy || lock.disabled || working;
  const canSubmit = !locked && waitSeconds === 0 && strength.strong && password === confirm &&
    (!recovering || code.trim() !== "");

  useEffect(() => {
    live.current = true;
    return () => { live.current = false; };
  }, []);
  useEffect(() => {
    if (!retryAt) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [retryAt]);

  const clear = () => { setCode(""); setPassword(""); setConfirm(""); };
  const cancel = () => {
    if (pending.current) return;
    clear();
    setProblem(null);
    onCancel();
  };
  const submit = async () => {
    if (!canSubmit || lock.isLocked() || pending.current) return;
    pending.current = true;
    setWorking(true);
    onWorkingChange?.(true);
    setProblem(null);
    const nextPassword = password;
    try {
      if (recovering) await bridge.ResetAdminPassword(code, nextPassword);
      else await bridge.ChangeAdminPassword(currentPassword, nextPassword);
      if (!live.current) return;
      clear();
      onSuccess(nextPassword);
    } catch (cause) {
      if (!live.current) return;
      const next = advancedProblem(cause, recovering ? "The password couldn't be reset" : "The password couldn't be changed",
        "Nothing was confirmed. Try again, or open the log file for support.");
      setProblem(next);
      if (next.retryAfter) {
        const time = Date.now();
        setNow(time);
        setRetryAt(time + next.retryAfter * 1000);
      }
      clear();
    } finally {
      pending.current = false;
      if (live.current) { setWorking(false); onWorkingChange?.(false); }
    }
  };
  const rules = [
    { label: "8–20 characters", met: [...password].length >= 8 && [...password].length <= 20 },
    { label: "Uppercase", met: /\p{Lu}/u.test(password) },
    { label: "Lowercase", met: /\p{Ll}/u.test(password) },
    { label: "A number", met: /\p{Nd}/u.test(password) },
  ];

  return <form className="advanced-admin-form" noValidate onSubmit={(event) => { event.preventDefault(); void submit(); }}>
    <p>{recovering ? "Use one unused code from your latest sheet. " : ""}
      Your CARE web login and backup recovery file won't change.</p>
    <fieldset disabled={locked}>
      {recovering ? <AdvancedSecretInput label="Unused recovery code" value={code} onChange={setCode}
        icon={<KeyRound aria-hidden="true" />} disabled={locked} autoFocus
        hint="Each code works once. Spaces and hyphens are accepted." /> : null}
      <AdvancedSecretInput id={`${id}-password`} label="New Desktop admin password" value={password}
        onChange={setPassword} disabled={locked} autoComplete="new-password" autoFocus={!recovering}
        hint={<div className="advanced-password-rules">
          {rules.map((rule) => <span key={rule.label} className={`advanced-password-rule${rule.met ? " is-met" : ""}`}>
            <Check aria-hidden="true" />{rule.label}
          </span>)}
        </div>} />
      {password && !strength.strong ? <p className="advanced-field-hint" role="status">
        {strength.message}
      </p> : null}
      <AdvancedSecretInput id={`${id}-confirm`} label="Confirm new Desktop admin password" value={confirm}
        onChange={setConfirm} disabled={locked} autoComplete="new-password"
        invalid={!!confirm && password !== confirm}
        hint={confirm && password !== confirm ? <span className="advanced-field-error">The passwords don't match. Type the same password again.</span> : undefined} />
    </fieldset>
    <AdvancedError problem={problem} />
    {waitSeconds > 0 ? <p className="advanced-field-hint" role="status">
      Try again in {waitSeconds} {waitSeconds === 1 ? "second" : "seconds"}.
    </p> : null}
    {recovering ? <p>Mark this code used after resetting. Other unused codes still work.</p> : null}
    <div className="advanced-form-footer">
      <Button type="button" disabled={working} onClick={cancel}>Cancel</Button>
      <Button type="submit" variant="primary" disabled={!canSubmit}>
        {working ? <Spinner /> : null}
        {working ? "Saving…" : recovering ? "Reset Desktop password" : "Change Desktop password"}
      </Button>
    </div>
  </form>;
}

export function AdminRecoverySettings({
  adminPassword, onPasswordChanged,
}: {
  adminPassword: string;
  onPasswordChanged: (password: string) => void;
}) {
  const { busy } = useCare();
  const lock = useAdvancedLock();
  const blocked = busy || lock.disabled;
  const [dialog, setDialog] = useState<"password" | "codes" | null>(null);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const [notice, setNotice] = useState("");
  const [savedPath, setSavedPath] = useState("");
  const [cancelled, setCancelled] = useState(false);
  const pending = useRef(false);
  const live = useRef(true);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const passwordWorking = useCallback((next: boolean) => {
    pending.current = next;
    setWorking(next);
  }, []);
  useEffect(() => {
    live.current = true;
    return () => { live.current = false; };
  }, []);
  const close = () => {
    if (working || pending.current) return;
    setDialog(null);
    setProblem(null);
    setCancelled(false);
  };
  const replaceCodes = async () => {
    if (busy || lock.isLocked() || working || pending.current) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    setCancelled(false);
    setSavedPath("");
    try {
      const saved = await bridge.SaveAdminRecoveryCodes(adminPassword, "");
      if (!live.current) return;
      if (!saved) {
        setCancelled(true);
        return;
      }
      setNotice("Six new codes saved. Every previous code is now invalid. Keep the new sheet safe and don't share it.");
      setDialog(null);
      try {
        const status = await bridge.GetSetupRecoveryStatus();
        if (live.current) setSavedPath(status.codes_path);
      } catch (cause) {
        if (live.current) setProblem(advancedProblem(cause, "The new codes were saved, but their location couldn't be checked",
          "Keep the sheet in the location you chose. Don't use any older recovery sheet."));
      }
    } catch (cause) {
      if (live.current) setProblem(advancedProblem(cause, "The new codes couldn't be saved",
        "Keep your previous recovery sheet. Choose a new filename or location and try again."));
    } finally {
      pending.current = false;
      if (live.current) setWorking(false);
    }
  };
  const open = (next: "password" | "codes") => {
    if (busy || lock.isLocked() || working || pending.current) return;
    setProblem(null);
    setNotice("");
    setSavedPath("");
    setCancelled(false);
    triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setDialog(next);
  };

  return <>
    <section className="advanced-card advanced-card-pad" aria-labelledby="advanced-recovery-title">
      <div className="advanced-card-row">
        <span className="advanced-icon advanced-icon-brand"><KeyRound aria-hidden="true" /></span>
        <div className="advanced-grow">
          <h2 className="advanced-card-title" id="advanced-recovery-title">Admin password and recovery codes</h2>
          <p className="advanced-card-description">Manage the Desktop password, or save six new recovery codes. Your CARE web login is separate.</p>
        </div>
        <div className="advanced-actions">
          <Button type="button" disabled={blocked || working} onClick={() => open("password")}>Change password</Button>
          <Button type="button" disabled={blocked || working} onClick={() => open("codes")}><Printer aria-hidden="true" className="size-4" />New recovery codes</Button>
        </div>
      </div>
    </section>
    {notice ? <AdvancedNotice title={notice} tone="success">
      {savedPath ? <span className="advanced-path">Saved to {savedPath}</span> : null}
    </AdvancedNotice> : null}
    {!dialog ? <AdvancedError problem={problem} /> : null}
    <AlertDialog open={dialog !== null} onOpenChange={(next) => { if (!next) close(); }}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onOpenAutoFocus={(event) => {
          if (dialog === "codes") { event.preventDefault(); cancelRef.current?.focus(); }
        }}
        onCloseAutoFocus={(event) => { event.preventDefault(); triggerRef.current?.focus(); }}
        onEscapeKeyDown={(event) => { if (working || pending.current) event.preventDefault(); }}>
        <AlertDialogTitle>{dialog === "password" ? "Change the Desktop admin password" : "Save six new recovery codes?"}</AlertDialogTitle>
        <AlertDialogDescription>
          {dialog === "password" ? "Staff sign-in to the CARE website stays the same."
            : "A successfully saved replacement set immediately invalidates every previous code, including unused ones."}
        </AlertDialogDescription>
        <div className="advanced-dialog-body">
          {dialog === "password" ? <AdminPasswordForm currentPassword={adminPassword}
            onWorkingChange={passwordWorking} onCancel={close} onSuccess={(password) => {
              pending.current = false;
              setWorking(false);
              onPasswordChanged(password);
              setDialog(null);
              setNotice("Desktop password changed. Your CARE web login and unused recovery codes are unchanged.");
            }} /> : <>
            <AdvancedNotice title="Keep the new sheet somewhere secure" tone="neutral">
              Save it outside CARE's folders and the backup folder, preferably away from this computer.
              You can print the saved sheet. Recovery codes don't unlock backups or change the CARE web password.
            </AdvancedNotice>
            {cancelled ? <AdvancedNotice title="No new codes were saved" tone="neutral">
              Your existing unused codes still work.
            </AdvancedNotice> : null}
            <AdvancedError problem={problem} />
            <div className="advanced-dialog-foot">
              <Button type="button" ref={cancelRef} disabled={working} onClick={close}>Cancel</Button>
              <Button type="button" variant="primary" disabled={blocked || working} onClick={() => void replaceCodes()}>
                {working ? <Spinner /> : null}{working ? "Saving codes…" : "Choose where to save"}
              </Button>
            </div>
          </>}
        </div>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

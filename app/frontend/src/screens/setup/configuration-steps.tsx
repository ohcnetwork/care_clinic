import { Check, CheckCircle2, Cpu, Folder, FolderOpen, Globe, KeyRound, Lock, Minus, RefreshCw, ShieldAlert } from "lucide-react";
import { useState } from "react";

import { Callout, ClinicAddressInput, LogButton, StatusBadge } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { PasswordStrength } from "@/hooks/use-password-strength";
import { diskSize } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { SetupForm } from "@/state/forms";
import { useCare } from "@/state/care-store";
import type { BackupSpace, SetupIssue, SetupPage, SetupRecoveryStatus } from "@/types";
import { isRequirement, SETUP_LABELS } from "./setup-model";

export type AddressResult = { name: string; state: "waiting" | "checking" | "ready" | "bad" | "failed"; message: string };

export function AddressStep({ value, result, disabled, onChange, onCheck }: {
  value: string; result: AddressResult; disabled: boolean;
  onChange: (value: string) => void; onCheck: () => void;
}) {
  const invalid = result.state === "bad" || result.state === "failed";
  return (
    <>
      <section className="on-card on-pad">
        <div className="on-field">
          <label htmlFor="mdnsname">Clinic address</label>
          <ClinicAddressInput id="mdnsname" prefix value={value} invalid={invalid} disabled={disabled}
            aria-describedby="setup-address-hint" onValueChange={onChange} />
          <p id="setup-address-hint" className={cn("on-hint", result.state === "ready" ? "on-success" : invalid ? "on-error" : "")} role="status">
            {result.state === "ready" ? <CheckCircle2 aria-hidden="true" /> : result.state === "checking" ? <Spinner /> : null}
            {result.message || "Checking whether this name is free on your network…"}
          </p>
        </div>
        {invalid ? <div className="on-actions"><Button disabled={disabled} onClick={onCheck}><RefreshCw aria-hidden="true" />Check again</Button>{result.state === "failed" ? <LogButton /> : null}</div> : null}
      </section>
      {result.state === "ready" ? <Callout title="Staff will open"><span className="on-mono">https://{result.name}</span></Callout> : null}
    </>
  );
}

function spaceDescription(space: BackupSpace, windows: boolean): string {
  if (space.total === 0) return "The available space couldn't be measured. Check the location again.";
  if (windows) return `${diskSize(space.free)} free`;
  const room = space.days_left > 365 ? `room for about ${Math.floor(space.days_left / 365)} years of backups`
    : space.days_left > 0 ? `room for about ${space.days_left} days of backups`
    : space.need > 0 ? `each backup needs about ${diskSize(space.need)}` : "";
  return `${diskSize(space.free)} free${room ? ` · ${room}` : ""}`;
}

export function BackupStep({ form, space, folderProblem, recovery, recoveryError, busy, action, onChoose, onCheck, onSave, onVerify, onReplace, onReload, onOpenFolder }: {
  form: SetupForm; space: BackupSpace | null; folderProblem: string; recovery: SetupRecoveryStatus;
  recoveryError: string; busy: boolean; action: string;
  onChoose: () => void; onCheck: () => void; onSave: () => void; onVerify: () => void; onReplace: () => void; onReload: () => void;
  onOpenFolder: () => void;
}) {
  const { platform } = useCare();
  const folderReady = space !== null && !folderProblem;
  const checked = recovery.backup_verified && !recovery.backup_problem && !recoveryError && action !== "verify-backup";
  return (
    <>
      <section className="on-card">
        <div className="on-data-row" style={{ paddingTop: 20, paddingBottom: 20 }}>
          <span className="on-tile"><Folder aria-hidden="true" /></span>
          <div className="on-grow"><div className="on-eyebrow">Backups are saved to</div>
            <strong className="on-mono">{space?.dir || form.backupDir || "Checking the backup location…"}</strong>
            {space ? <p>{spaceDescription(space, platform === "windows")}</p> : null}
          </div>
          <Button disabled={busy} onClick={onChoose}><FolderOpen aria-hidden="true" />Change folder</Button>
        </div>
        {folderProblem ? <div style={{ padding: "0 20px 16px" }}><p role="alert">{space && space.total > 0 && space.free < space.need
          ? `Only ${diskSize(space.free)} is free there. Each backup needs about ${diskSize(space.need)} to start with. Choose a different location with more room.`
          : folderProblem}</p><div className="on-actions"><Button disabled={busy} onClick={onCheck}>Check again</Button></div></div> : null}
      </section>
      <section className="on-card on-backup-recovery" aria-label="Your backup recovery file">
        <div className="on-card-title"><h3>Your backup recovery file</h3><p>This small file unlocks your backups if you ever need to restore. When installation starts, CARE also keeps a local copy encrypted with your Desktop admin password. Keep this separate file safe for recovery without this computer or password.</p>
          {!folderReady ? <p>Sort the folder out first — the file is made once CARE knows where the backups go.</p> : null}
        </div>
        <div className="on-data-row">
          <span className={cn("on-tile", recovery.backup_saved && !recovery.backup_problem && "on-solid")}>{recovery.backup_saved && !recovery.backup_problem ? <Check /> : <KeyRound />}</span>
          <div className="on-grow"><strong>{recovery.backup_saved ? recovery.backup_problem ? "The saved recovery file needs checking" : "Recovery file saved" : "1. Save the recovery file"}</strong>
            <p className={recovery.backup_saved ? "on-mono" : ""}>{recovery.backup_saved ? recovery.backup_path || "The saved location isn't available." : "You choose where it goes — anywhere except CARE's own folders."}</p>
          </div>
          {recovery.backup_saved ? !recovery.backup_problem ? <StatusBadge tone="ok"><Check />Saved</StatusBadge> : null
            : <Button variant="primary" className="on-save-primary" disabled={busy || !folderReady} onClick={onSave}>{action === "save-backup" ? <Spinner /> : <FolderOpen aria-hidden="true" />}Choose where to save</Button>}
          {recovery.backup_saved ? <Button disabled={busy || !folderReady} onClick={onReplace}>{action === "replace-backup" ? <Spinner /> : <RefreshCw aria-hidden="true" />}Save a new recovery file</Button> : null}
          {platform === "windows" && recovery.backup_saved && !recovery.backup_problem ? <Button disabled={busy} onClick={onOpenFolder}>Open folder</Button> : null}
        </div>
        <div className="on-data-row">
          <span className={cn("on-tile", checked && "on-solid", recoveryError && "on-bad")}>{checked ? <Check /> : <ShieldAlert />}</span>
          <div className="on-grow"><strong>{checked ? "Recovery file checked" : "2. Check the file you saved"}</strong><p>{checked ? "The file matches this clinic." : "Select the saved file so CARE can confirm it's the right one."}</p></div>
          {checked ? <StatusBadge tone="ok"><Check />Checked</StatusBadge> : null}
          <Button variant={checked || !recovery.backup_saved ? "default" : "primary"} disabled={busy || !folderReady || !recovery.backup_saved} onClick={onVerify}>{action === "verify-backup" ? <Spinner /> : checked ? <RefreshCw aria-hidden="true" /> : <FolderOpen aria-hidden="true" />}{checked ? "Check again" : "Select saved file"}</Button>
        </div>
        {recovery.backup_saved ? <p className="on-small" style={{ padding: "0 20px 14px" }}>Lost the saved file? Save a new recovery file here. This replaces the old key; select and check the new file before continuing.</p> : null}
      </section>
      {recovery.backup_problem ? <Callout title="The recovery file isn't ready">
        {recovery.backup_problem === "mismatch" ? "That file doesn't match this clinic. Select the file you saved for this setup." : "The saved file couldn't be opened. Select it in its new location, or save a new recovery file."}
        <p>A new recovery file replaces the old one. Only the new file will unlock the backups this setup makes.</p>
      </Callout> : null}
      {recoveryError ? <Callout tone="danger" title="The recovery file couldn't be checked">{recoveryError}<div className="on-actions"><Button disabled={busy} onClick={onReload}>Check saved files again</Button><LogButton /></div></Callout> : null}
      <Callout tone="danger" title="Keep this file safe, and don't share it with anyone">It unlocks every backup this clinic makes.</Callout>
      {platform === "windows" ? <Callout title="Choose a secure location">An external drive is recommended for backups. Desktop may sync to OneDrive; saving recovery files there can upload them to your cloud account.</Callout> : null}
    </>
  );
}

export function AdminStep({ form, patch, strength, passwordError, folderProblem, recovery, recoveryError, busy, action, onSave, onPrint, onReload, onBackups, onOpenFolder }: {
  form: SetupForm; patch: (form: Partial<SetupForm>) => void; strength: PasswordStrength;
  passwordError: string; folderProblem: string;
  recovery: SetupRecoveryStatus; recoveryError: string; busy: boolean; action: string;
  onSave: () => void; onPrint: () => void; onReload: () => void; onBackups: () => void;
  onOpenFolder: () => void;
}) {
  const { platform } = useCare();
  const [show, setShow] = useState(false);
  const length = [...form.adminPassword].length;
  const rules = [
    { label: "8 to 20 characters", ok: length >= 8 && length <= 20 },
    { label: "Upper and lowercase letter", ok: /\p{Lu}/u.test(form.adminPassword) && /\p{Ll}/u.test(form.adminPassword) },
    { label: "A number", ok: /\p{Nd}/u.test(form.adminPassword) },
  ];
  const matching = !!form.adminConfirm && form.adminConfirm === form.adminPassword;
  const passwordReady = matching && strength.strong && !passwordError;
  const saved = recovery.codes_saved && !recovery.codes_problem;
  return (
    <>
      {folderProblem ? <Callout tone="danger" title="The backup location needs another look">{folderProblem}
        <div className="on-actions"><Button disabled={busy} onClick={onBackups}>Check backup location</Button></div>
      </Callout> : null}
      <section className="on-card on-pad on-password-card">
        <div className="on-row" style={{ flexWrap: "wrap" }}><span className="on-eyebrow">Username</span><StatusBadge><span className="on-mono">admin</span></StatusBadge><span className="on-small">You can add more staff logins inside CARE later.</span></div>
        <div className="on-field">
          <label htmlFor="adminpw">Password</label>
          <div className="on-input"><Lock aria-hidden="true" /><Input className="on-text-input" id="adminpw" type={show ? "text" : "password"} autoComplete="new-password" placeholder="Choose a password" value={form.adminPassword} disabled={busy} aria-describedby="password-rules password-verdict" onChange={(e) => patch({ adminPassword: e.target.value })} />
            <Button variant="ghost" aria-label={show ? "Hide passwords" : "Show passwords"} aria-pressed={show} disabled={busy} onClick={() => setShow((v) => !v)}>{show ? "Hide" : "Show"}</Button>
          </div>
          <div id="password-rules">
            <div className="on-strength" aria-hidden="true">{rules.map((rule) => <span key={rule.label} className={rule.ok ? "on-filled" : ""} />)}</div>
            <div className="on-chips">{rules.map((rule) => <span key={rule.label} className={cn("on-chip", rule.ok && "on-ok")}>{rule.ok ? <Check aria-hidden="true" /> : <Minus aria-hidden="true" />}{rule.label}<span className="sr-only">{rule.ok ? ": met" : ": not met"}</span></span>)}</div>
          </div>
          <p id="password-verdict" className={cn("on-hint", passwordError && "on-error")} role="status">{passwordError || (form.adminPassword && !strength.strong ? strength.message : null)}</p>
        </div>
        <div className="on-field">
          <label htmlFor="adminpw-confirm">Confirm password</label>
          <div className={cn("on-input", form.adminConfirm && !matching && "on-invalid")}><Lock aria-hidden="true" /><Input className="on-text-input" id="adminpw-confirm" type={show ? "text" : "password"} autoComplete="new-password" placeholder="Type it again" value={form.adminConfirm} disabled={busy} aria-invalid={!!form.adminConfirm && !matching} aria-describedby="confirm-verdict" onChange={(e) => patch({ adminConfirm: e.target.value })} /></div>
          <p id="confirm-verdict" role="status" className={cn("on-hint", matching ? "on-success" : "on-error")}>{matching ? <><CheckCircle2 aria-hidden="true" />Both passwords match.</> : form.adminConfirm ? "These don't match. Type the same password again, including capital letters." : null}</p>
        </div>
        <p className="on-small">Save this password in your password manager or write it down somewhere secure before starting installation.</p>
      </section>
      <section className="on-card">
        <div className="on-data-row" style={{ paddingTop: 20, paddingBottom: 20 }}>
          <span className={cn("on-tile", saved && "on-solid")}>{saved ? <Check /> : <KeyRound />}</span>
          <div className="on-grow"><h3>{saved ? "Recovery codes saved" : "Recovery codes"}</h3>
            {saved ? <p>Six codes, each usable once. Saved to <span className="on-mono">{recovery.codes_path}</span> — print it or keep it somewhere separate.</p>
              : <p>Forgot your <strong>CARE Desktop</strong> password? Use one of these six codes to reset it. Each code works once. Save them somewhere safe.</p>}
          </div>
          {saved ? <><StatusBadge tone="ok"><Check />Saved</StatusBadge><Button disabled={busy} onClick={onPrint}>Open to print</Button><Button disabled={busy || !passwordReady || !!folderProblem} onClick={onSave}>Save a new set</Button></>
            : <div className="on-admin-save">
              <Button variant="primary" className="on-save-primary" disabled={busy || !passwordReady || !!folderProblem} onClick={onSave}>{action === "save-codes" ? <Spinner /> : <FolderOpen aria-hidden="true" />}Choose where to save</Button>
              {!passwordReady ? <p className="on-small">{strength.strong ? "Confirm your password first." : "Set your password first."}</p> : null}
            </div>}
          {platform === "windows" && saved ? <Button disabled={busy} onClick={onOpenFolder}>Open folder</Button> : null}
        </div>
        {saved ? <p className="on-small" style={{ padding: "0 20px 14px" }}>Saving a new set cancels every code on the old sheet.</p> : null}
      </section>
      {recovery.codes_problem ? <Callout title="The recovery codes file isn't available">Save a fresh set before continuing. The old codes stop working once the new set is saved.</Callout> : null}
      {recoveryError ? <Callout tone="danger" title="The recovery codes couldn't be saved or checked">{recoveryError}<div className="on-actions"><Button disabled={busy} onClick={onReload}>Check saved files again</Button><LogButton /></div></Callout> : null}
      {platform === "windows" ? <Callout title="Keep recovery codes private">Desktop may sync to OneDrive. Choose a secure location if you don't want these codes uploaded to your cloud account.</Callout> : null}
    </>
  );
}

export function ReviewStep({ steps, form, backupPath, issues, verified, busy, onEdit }: {
  steps: SetupPage[]; form: SetupForm; backupPath: string; issues: SetupIssue[];
  verified: boolean; busy: boolean; onEdit: (page: SetupPage) => void;
}) {
  const computerIssues = issues.filter((issue) => isRequirement(issue.step));
  const computerCount = steps.filter(isRequirement).length;
  const rows = [
    { id: "space" as const, icon: Cpu, label: "This computer", value: computerIssues.length ? "Needs attention" : `${computerCount} computer checks passed`, detail: verified ? "Checked just now" : "Checking before installation", failures: computerIssues },
    { id: "address" as const, icon: Globe, label: "Clinic address", value: `https://${form.hostInput.trim().toLowerCase()}.local`, detail: "What staff type in their browser", failures: issues.filter((issue) => issue.step === "address") },
    { id: "backup" as const, icon: Folder, label: "Backups", value: backupPath, detail: "Every 24 hours · recovery file saved and checked", failures: issues.filter((issue) => issue.step === "backup") },
    { id: "admin" as const, icon: KeyRound, label: "Admin login", value: "admin", detail: "Password set · six recovery codes saved", failures: issues.filter((issue) => issue.step === "admin") },
  ];
  return (
    <>
      {issues.length ? <Callout tone="danger" title={issues.length === 1 ? "One step needs another look" : "Some steps need another look"}>Something changed since you checked. Fix it takes you to that step and brings you straight back here — without walking through the other steps again.</Callout> : null}
      <div className="on-card">
        {rows.map((row) => <section key={row.id} className={cn("on-review-row", row.failures.length > 0 && "on-invalid")} aria-label={row.label}>
          <span className="on-tile"><row.icon aria-hidden="true" /></span><span className="on-review-label">{row.label}</span>
          <div className="on-grow on-review-value"><strong className={row.id !== "space" ? "on-mono" : ""}>{row.value || "Not yet confirmed"}</strong>
            {row.failures.length ? row.failures.map((issue) => <div key={issue.step}><p>Step {steps.indexOf(issue.step) + 1} · {issue.message}</p>{row.id === "space" ? <Button variant="ghost" disabled={busy} aria-label={`Fix ${SETUP_LABELS[issue.step]}`} onClick={() => onEdit(issue.step)}>Fix it</Button> : null}</div>)
              : <p>{row.detail}</p>}
          </div>
          {row.id !== "space" ? <Button variant="ghost" disabled={busy} aria-label={`Edit ${row.label}`} onClick={() => onEdit(row.id)}>{row.failures.length ? "Fix it" : "Edit"}</Button> : null}
        </section>)}
      </div>
      {verified && !issues.length ? <Callout tone="ok" title="Everything is ready">Installing takes 5 to 20 minutes. Don't quit CARE Desktop or turn the computer off while it runs.</Callout> : null}
    </>
  );
}

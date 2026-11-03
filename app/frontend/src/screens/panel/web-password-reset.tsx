import { Globe } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import {
  AdvancedError, AdvancedNotice, AdvancedSecretInput, advancedProblem, useAdvancedLock, type AdvancedProblem,
} from "./advanced-ui";
import { WebUserPicker, type WebUserList } from "./web-user-picker";

const DEFAULT_USERNAME = "admin";

function webPasswordProblem(cause: unknown, username: string): AdvancedProblem {
  const detail = errorText(cause);
  if (/no CARE user named/i.test(detail)) return {
    title: "No CARE user has that username",
    message: `There is no CARE user named "${username}". Pick them from the list, or type the username exactly as staff use it to sign in; capital letters matter.`,
  };
  const rejected = detail.match(/CARE didn't accept that password: ([\s\S]+)/);
  if (rejected) return { title: "CARE didn't accept that password", message: rejected[1].trim() };
  if (/CARE isn't running/i.test(detail)) return {
    title: "Start CARE first",
    message: "CARE needs to be running to change a web password. Start it from Overview, then try again.",
  };
  return advancedProblem(cause, "The web password couldn't be changed",
    "Nothing was confirmed. Try again, or open the log file for support.");
}

export function WebPasswordReset({ adminPassword }: { adminPassword: string }) {
  const { busy } = useCare();
  const lock = useAdvancedLock();
  const id = useId();
  const blocked = busy || lock.disabled;
  const [open, setOpen] = useState(false);
  const [username, setUsername] = useState(DEFAULT_USERNAME);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<AdvancedProblem | null>(null);
  const [changed, setChanged] = useState("");
  const [users, setUsers] = useState<WebUserList>({ status: "loading" });
  const [listOpen, setListOpen] = useState(false);
  const listRequest = useRef(0);
  const pending = useRef(false);
  const live = useRef(true);
  const triggerRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    live.current = true;
    return () => { live.current = false; };
  }, []);
  const mismatch = !!confirm && password !== confirm;
  const canSubmit = !blocked && !working && username.trim() !== "" && password !== "" && password === confirm;
  const loadUsers = async () => {
    const request = ++listRequest.current;
    setUsers({ status: "loading" });
    try {
      const list = await bridge.ListWebUsers(adminPassword);
      if (live.current && request === listRequest.current) setUsers({ status: "ready", users: list ?? [] });
    } catch (cause) {
      if (!live.current || request !== listRequest.current) return;
      const detail = errorText(cause);
      setUsers({
        status: "failed",
        message: /CARE isn't running/i.test(detail) ? "Start CARE to see the staff list." : "The staff list couldn't be loaded.",
      });
    }
  };
  const begin = () => {
    if (blocked || lock.isLocked() || pending.current) return;
    setListOpen(false);
    void loadUsers();
    setUsername(DEFAULT_USERNAME);
    setPassword("");
    setConfirm("");
    setProblem(null);
    setChanged("");
    setOpen(true);
  };
  const close = () => {
    if (pending.current) return;
    listRequest.current++;
    setListOpen(false);
    setOpen(false);
    setPassword("");
    setConfirm("");
    setProblem(null);
  };
  const submit = async () => {
    if (!canSubmit || lock.isLocked() || pending.current) return;
    pending.current = true;
    setWorking(true);
    setProblem(null);
    const target = username.trim();
    try {
      await bridge.ResetWebPassword(target, password, adminPassword);
      if (!live.current) return;
      setPassword("");
      setConfirm("");
      setChanged(target);
      setOpen(false);
    } catch (cause) {
      if (!live.current) return;
      setPassword("");
      setConfirm("");
      setProblem(webPasswordProblem(cause, target));
    } finally {
      pending.current = false;
      if (live.current) setWorking(false);
    }
  };
  return <>
    <section className="advanced-card advanced-card-pad" aria-labelledby="advanced-web-password-title">
      <div className="advanced-card-row">
        <span className="advanced-icon"><Globe aria-hidden="true" /></span>
        <div className="advanced-grow">
          <h2 className="advanced-card-title" id="advanced-web-password-title">CARE web sign-in</h2>
          <p className="advanced-card-description">Set a new password for anyone locked out of CARE in the browser. CARE can't email reset links in this setup.</p>
        </div>
        <div className="advanced-actions">
          <Button type="button" ref={triggerRef} disabled={blocked || working} onClick={begin}>Reset a CARE web password</Button>
        </div>
      </div>
    </section>
    {changed ? <AdvancedNotice title={`CARE web password changed for ${changed}`} tone="success">
      They can sign in to CARE in the browser with the new password now. The CARE Clinic admin password is unchanged.
    </AdvancedNotice> : null}
    <AlertDialog open={open} onOpenChange={(next) => { if (!next) close(); }}>
      <AlertDialogContent className="advanced-dialog advanced-dialog-narrow"
        onCloseAutoFocus={(event) => { event.preventDefault(); triggerRef.current?.focus(); }}
        onEscapeKeyDown={(event) => {
          if (listOpen) { event.preventDefault(); setListOpen(false); return; }
          if (working || pending.current) event.preventDefault();
        }}>
        <AlertDialogTitle>Reset a CARE web password</AlertDialogTitle>
        <AlertDialogDescription>
          This changes the password staff use to sign in to CARE in the browser. It doesn't change the CARE Clinic admin password.
        </AlertDialogDescription>
        <div className="advanced-dialog-body"><form className="advanced-admin-form" noValidate onSubmit={(event) => { event.preventDefault(); void submit(); }}>
          <p>CARE can't email or text password reset links in this setup, so use this when a staff member forgets their password.
            Anyone already signed in as this user stays signed in until they sign out or their session expires.</p>
          <fieldset disabled={blocked || working}>
            <WebUserPicker id={`${id}-username`} value={username} list={users} disabled={blocked || working}
              onChange={(next) => { if (!lock.isLocked()) setUsername(next); }}
              open={listOpen} onOpenChange={setListOpen} />
            <AdvancedSecretInput id={`${id}-password`} label="New CARE web password" value={password}
              onChange={setPassword} disabled={blocked || working} autoComplete="new-password"
              hint="CARE checks the password: at least 8 characters, not a common password, not only numbers and not too like the username." />
            <AdvancedSecretInput id={`${id}-confirm`} label="Confirm new CARE web password" value={confirm}
              onChange={setConfirm} disabled={blocked || working} autoComplete="new-password" invalid={mismatch}
              hint={mismatch ? <span className="advanced-field-error">The passwords don't match. Type the same password again.</span> : undefined} />
          </fieldset>
          <AdvancedError problem={problem} />
          <div className="advanced-form-footer">
            <Button type="button" disabled={working} onClick={close}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={!canSubmit}>
              {working ? <Spinner /> : null}{working ? "Saving…" : "Reset web password"}
            </Button>
          </div>
        </form></div>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

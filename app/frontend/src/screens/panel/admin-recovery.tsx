import { useState } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { usePasswordStrength } from "@/hooks/use-password-strength";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import { PasswordPair } from "@/screens/setup/password-pair";

export function AdminPasswordForm({
  currentPassword, onSuccess, onCancel,
}: {
  currentPassword?: string;
  onSuccess: (password: string) => void;
  onCancel: () => void;
}) {
  const { busy } = useCare();
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState("");
  const strength = usePasswordStrength(password);
  const recovering = currentPassword === undefined;
  const submit = async () => {
    setWorking(true);
    setProblem("");
    try {
      if (recovering) await bridge.ResetAdminPassword(code, password);
      else await bridge.ChangeAdminPassword(currentPassword, password);
      onSuccess(password);
    } catch (e) {
      setProblem(errorText(e));
    } finally {
      setWorking(false);
    }
  };
  return (
    <div className="flex flex-col gap-3 text-left">
      <p className="text-[13px] text-muted-foreground">
        {recovering ? "Use one unused code from your latest saved recovery sheet. " : ""}
        This changes only the Desktop admin password, not your CARE web login.
      </p>
      {recovering ? (
        <div className="flex flex-col gap-2">
          <Label htmlFor="admin-recovery-code">Unused recovery code</Label>
          <Input
            id="admin-recovery-code" value={code} autoComplete="off" spellCheck={false}
            onChange={(e) => setCode(e.target.value)} disabled={working}
          />
        </div>
      ) : null}
      <div>
        <Label htmlFor="new-desktop-password" className="mb-2 block">New Desktop admin password</Label>
        <PasswordPair id="new-desktop-password" password={password} confirm={confirm}
          strength={strength} onPasswordChange={setPassword} onConfirmChange={setConfirm} />
      </div>
      {problem ? <Alert variant="danger">{problem}</Alert> : null}
      <div className="flex gap-3">
        <Button disabled={working} onClick={onCancel}>Cancel</Button>
        <Button variant="primary"
          disabled={busy || working || !strength.strong || password !== confirm || (recovering && !code.trim())}
          onClick={() => void submit()}>
          {working ? "Saving..." : recovering ? "Reset Desktop password" : "Change Desktop password"}
        </Button>
      </div>
      {recovering ? (
        <p className="text-[12px] text-muted-foreground">
          Mark this code used after the reset succeeds. The other unused codes remain valid.
          Without your password or an unused code, this recovery flow cannot grant access.
        </p>
      ) : null}
    </div>
  );
}

export function AdminRecoverySettings({
  adminPassword, onPasswordChanged,
}: {
  adminPassword: string;
  onPasswordChanged: (password: string) => void;
}) {
  const { busy } = useCare();
  const [changing, setChanging] = useState(false);
  const [replacing, setReplacing] = useState(false);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState("");
  const [notice, setNotice] = useState("");
  const replaceCodes = async () => {
    setWorking(true);
    setProblem("");
    setNotice("");
    try {
      if (await bridge.SaveAdminRecoveryCodes(adminPassword, "")) {
        setNotice("Six new codes saved. All previous codes are now invalid. You can print the new sheet.");
        setReplacing(false);
      }
    } catch (e) {
      setProblem(errorText(e));
    } finally {
      setWorking(false);
    }
  };
  return (
    <Card className="flex flex-col gap-3 p-4">
      <CardTitle>Desktop password and recovery</CardTitle>
      <CardDescription>
        Manage access to this installation. These actions do not change your CARE web login.
      </CardDescription>
      {notice ? <Alert>{notice}</Alert> : null}
      {problem ? <Alert variant="danger">{problem}</Alert> : null}
      {changing ? (
        <AdminPasswordForm currentPassword={adminPassword}
          onCancel={() => setChanging(false)}
          onSuccess={(password) => {
            onPasswordChanged(password);
            setChanging(false);
            setNotice("Desktop password changed. Your CARE web login and unused recovery codes are unchanged.");
          }} />
      ) : replacing ? (
        <>
          <Alert variant="danger">
            Saving a new set immediately invalidates every previous recovery code.
            Keep the new sheet secure, outside this computer.
          </Alert>
          <div className="flex gap-3">
            <Button disabled={working} onClick={() => setReplacing(false)}>Cancel</Button>
            <Button disabled={busy || working} onClick={() => void replaceCodes()}>Save replacement codes</Button>
          </div>
        </>
      ) : (
        <div className="flex flex-wrap gap-3">
          <Button disabled={busy} onClick={() => { setNotice(""); setChanging(true); }}>Change Desktop password</Button>
          <Button disabled={busy} onClick={() => { setNotice(""); setReplacing(true); }}>Replace recovery codes</Button>
        </div>
      )}
    </Card>
  );
}

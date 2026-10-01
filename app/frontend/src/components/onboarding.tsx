import { AlertCircle, Check, FileText, Globe } from "lucide-react";
import { useRef, useState, type ComponentProps, type ReactNode } from "react";

import logoMark from "@/assets/care-logo-mark.svg";
import { StartUpdateCard } from "@/components/start-update-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { AppUpdateController } from "@/hooks/use-app-update";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";

import "@/screens/role-screen.css";
import "./onboarding.css";

export function OnboardingBrand({ wizard = false }: { wizard?: boolean }) {
  return (
    <div className="on-brand">
      <img src={logoMark} alt="" />
      <div><strong>CARE Desktop</strong>{wizard ? <p>Setting up your clinic</p> : null}</div>
    </div>
  );
}

export function OnboardingUpdates({ controller, context }: {
  controller: AppUpdateController;
  context: "setup" | "client";
}) {
  return <div className="start-screen onboarding-update"><StartUpdateCard controller={controller} context={context} /></div>;
}

export function StatusBadge({ children, tone = "" }: { children: ReactNode; tone?: "" | "ok" | "bad" | "warn" }) {
  return <span className={cn("on-badge", tone && `on-${tone}`)}>{children}</span>;
}

export function Callout({ title, children, tone = "neutral" }: {
  title?: string;
  children: ReactNode;
  tone?: "neutral" | "danger" | "info" | "ok" | "warn";
}) {
  return (
    <div className={cn("on-notice", `on-${tone}`)} role={tone === "danger" ? "alert" : undefined}>
      {tone === "ok" ? <Check aria-hidden="true" /> : <AlertCircle aria-hidden="true" />}
      <div className="on-grow">{title ? <strong>{title}</strong> : null}<div>{children}</div></div>
    </div>
  );
}

export function LogButton({ label = "Open log file" }: { label?: string }) {
  const { log } = useCare();
  const pending = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <span className="on-log">
      <Button variant="ghost" disabled={busy} onClick={() => {
        if (pending.current) return;
        pending.current = true;
        setBusy(true);
        setError("");
        void bridge.OpenLogFolder().catch((e) => {
          log(`open log: ${errorText(e)}`);
          setError("Couldn't open the log file. Try again, or ask the person who looks after this computer.");
        }).finally(() => { pending.current = false; setBusy(false); });
      }}><FileText aria-hidden="true" />{label}</Button>
      {error ? <span role="alert" className="on-error">{error}</span> : null}
    </span>
  );
}

export function isClinicName(value: string): boolean {
  return /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(value.trim());
}

export function pastedClinicName(value: string): string | null {
  const match = /^(?:https?:\/\/)?([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.local)?\/?$/i.exec(value.trim());
  return match ? match[1].toLowerCase() : null;
}

export function ClinicAddressInput({ prefix = false, invalid, onValueChange, ...props }: Omit<ComponentProps<"input">, "onChange" | "prefix"> & {
  prefix?: boolean;
  invalid?: boolean;
  onValueChange: (value: string) => void;
}) {
  return (
    <div className={cn("on-input on-address", invalid && "on-invalid")}>
      {prefix ? <span className="on-prefix">https://</span> : <Globe aria-hidden="true" />}
      <Input {...props} className="on-text-input" aria-invalid={invalid || undefined}
        autoCapitalize="none" autoComplete="off" autoCorrect="off" spellCheck={false}
        onChange={(e) => onValueChange(e.target.value)}
        onPaste={(e) => {
          const name = pastedClinicName(e.clipboardData.getData("text"));
          if (name !== null) { e.preventDefault(); onValueChange(name); }
        }}
      />
      <span className="on-suffix">.local</span>
    </div>
  );
}

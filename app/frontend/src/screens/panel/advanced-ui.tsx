import { Check, FileText, LockKeyhole, TriangleAlert } from "lucide-react";
import { createContext, useContext, useEffect, useId, useMemo, useRef, useState, type ComponentProps, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";

import "./advanced.css";

export type AdvancedProblem = { title: string; message: string; retryAfter?: number };

const AdvancedLockContext = createContext<{ disabled: boolean; isLocked: () => boolean }>({ disabled: false, isLocked: () => false });

export function AdvancedLockProvider({ disabled, children }: { disabled: boolean; children: ReactNode }) {
  const current = useRef(disabled);
  current.current = disabled;
  const lock = useMemo(() => ({ disabled, isLocked: () => current.current }), [disabled]);
  return <AdvancedLockContext.Provider value={lock}>{children}</AdvancedLockContext.Provider>;
}

export function useAdvancedLock() {
  return useContext(AdvancedLockContext);
}

export function advancedProblem(cause: unknown, title: string, message: string): AdvancedProblem {
  const detail = errorText(cause);
  const wait = detail.match(/too many recovery attempts; try again in (\d+) seconds/i);
  if (wait) return {
    title: "Please wait before trying another code",
    message: "Too many recovery codes were tried. Wait, then use an unused code from your latest sheet.",
    retryAfter: Number(wait[1]),
  };
  if (/recovery code is invalid or already used/i.test(detail)) return {
    title: "That recovery code didn't work",
    message: "Use an unused code from your latest sheet. Codes that have been used or replaced no longer work.",
  };
  if (/admin password does not match/i.test(detail)) return {
    title: "The Desktop admin password didn't match",
    message: "Lock Advanced and enter this installation's Desktop admin password again. Your CARE web password may be different.",
  };
  if (/restore is unfinished|restore.*pending/i.test(detail)) return {
    title: "Finish the earlier restore first",
    message: "Go to Overview and start CARE to recover the unfinished restore before making other changes.",
  };
  if (/something else is still running|CARE Desktop is closing/i.test(detail)) return {
    title: "CARE Desktop is busy",
    message: "Wait for the current task to finish, then try again.",
  };
  if (/cleanup is incomplete|only for an installed clinic|not set up yet/i.test(detail)) return {
    title: "This installation isn't ready for that",
    message: "Finish setting up or removing this installation before changing these settings.",
  };
  if (/keep recovery materials outside|keep.*outside CARE|keep.*recovery.*separate/i.test(detail)) return {
    title: "Choose a separate place for the codes",
    message: "Save the sheet outside CARE's settings, installation, logs and backup folder. Keep it somewhere secure.",
  };
  if (/could not activate the new recovery codes/i.test(detail)) return {
    title: "The new codes weren't activated",
    message: "Keep your previous recovery sheet. Choose a new filename and try saving a replacement set again.",
  };
  if (/choose a new filename|file exists|already exists/i.test(detail)) return {
    title: "Choose a new filename",
    message: "CARE won't overwrite a recovery sheet. Choose another name or location and try again.",
  };
  if (/environment file has invalid syntax/i.test(detail)) return {
    title: "The settings couldn't be saved",
    message: "One of the settings isn't in a format CARE can read. Review your changes, or ask the person who supports this computer.",
  };
  if (/protected|managed.*setting|setting.*managed/i.test(detail)) return {
    title: "That setting is managed by CARE Desktop",
    message: "Leave CARE's connection and security settings unchanged. Use the relevant settings page instead.",
  };
  return { title, message };
}

export function AdvancedNotice({ title, children, tone = "danger" }: {
  title: string;
  children?: ReactNode;
  tone?: "danger" | "success" | "neutral";
}) {
  return <div className={`advanced-notice advanced-notice-${tone}`} role={tone === "danger" ? "alert" : "status"}>
    {tone === "success" ? <Check aria-hidden="true" /> : <TriangleAlert aria-hidden="true" />}
    <div><strong>{title}</strong>{children ? <div>{children}</div> : null}</div>
  </div>;
}

export function AdvancedLogButton({ label = "Open log file for support" }: { label?: string }) {
  const pending = useRef(false);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState(false);
  const open = async () => {
    if (pending.current) return;
    pending.current = true;
    setWorking(true);
    setProblem(false);
    try {
      await bridge.OpenLogFolder();
    } catch {
      setProblem(true);
    } finally {
      pending.current = false;
      setWorking(false);
    }
  };
  return <div className="advanced-log-action">
    <Button type="button" variant="ghost" size="sm" disabled={working} onClick={() => void open()}>
      <FileText aria-hidden="true" />{label}
    </Button>
    {problem ? <p className="advanced-field-error" role="alert">The log folder couldn't be opened. Try again.</p> : null}
  </div>;
}

export function AdvancedError({ problem }: { problem: AdvancedProblem | null }) {
  return problem ? <AdvancedNotice title={problem.title}>
    <p>{problem.message}</p><AdvancedLogButton />
  </AdvancedNotice> : null;
}

export function AdvancedSecretInput({
  label, value, onChange, hint, invalid, icon, inputRef, ...props
}: Omit<ComponentProps<typeof Input>, "value" | "onChange" | "type"> & {
  label: string;
  value: string;
  onChange: (value: string) => void;
  hint?: ReactNode;
  invalid?: boolean;
  icon?: ReactNode;
  inputRef?: React.Ref<HTMLInputElement>;
}) {
  const generated = useId();
  const id = props.id ?? generated;
  const lock = useAdvancedLock();
  const disabled = props.disabled || lock.disabled;
  const [reveal, setReveal] = useState(false);
  useEffect(() => { if (!value || disabled) setReveal(false); }, [value, disabled]);
  return <div className="advanced-field">
    <label htmlFor={id}>{label}</label>
    <div className={`advanced-secret${invalid ? " is-invalid" : ""}`}>
      {icon ?? <LockKeyhole aria-hidden="true" />}
      <Input {...props} id={id} ref={inputRef} value={value} disabled={disabled}
        onChange={(event) => { if (!props.disabled && !lock.isLocked()) onChange(event.target.value); }}
        type={reveal ? "text" : "password"} autoComplete={props.autoComplete ?? "off"} spellCheck={false}
        aria-invalid={invalid || undefined} aria-describedby={hint ? `${id}-hint` : props["aria-describedby"]} />
      <button type="button" disabled={disabled} aria-label={`${reveal ? "Hide" : "Show"} ${label.toLowerCase()}`}
        aria-pressed={reveal} onClick={() => { if (!props.disabled && !lock.isLocked()) setReveal((shown) => !shown); }}>{reveal ? "Hide" : "Show"}</button>
    </div>
    {hint ? <div id={`${id}-hint`} className="advanced-field-hint">{hint}</div> : null}
  </div>;
}

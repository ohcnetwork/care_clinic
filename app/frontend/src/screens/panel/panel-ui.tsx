import { ArrowUpRight, Check, FileText, Globe, TriangleAlert } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";

import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import { panelStatus, type PanelTone } from "./panel-status";

export function usePanelTask() {
  const { log } = useCare();
  const pending = useRef(false);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");

  const run = async (task: () => Promise<unknown>, message: string) => {
    if (pending.current) return false;
    pending.current = true;
    setWorking(true);
    setError("");
    try {
      return (await task()) !== false;
    } catch (cause) {
      log(`panel: ${errorText(cause)}`);
      setError(message);
      return false;
    } finally {
      pending.current = false;
      setWorking(false);
    }
  };
  return { run, working, error, clearError: () => setError("") };
}

export function PanelBadge({ children, tone = "neutral", working = false }: {
  children: ReactNode;
  tone?: PanelTone;
  working?: boolean;
}) {
  return <span className={`panel-badge panel-tone-${tone}`}>
    {working ? <Spinner /> : tone === "ok" ? <Check aria-hidden="true" /> : null}
    {children}
  </span>;
}

export function PanelNotice({ title, children, tone = "danger", actions }: {
  title: string;
  children?: ReactNode;
  tone?: PanelTone;
  actions?: ReactNode;
}) {
  return <div className={`panel-notice panel-tone-${tone}`} role={tone === "danger" ? "alert" : "status"}>
    {tone === "ok" ? <Check aria-hidden="true" /> : <TriangleAlert aria-hidden="true" />}
    <div className="panel-grow">
      <strong>{title}</strong>
      {children ? <div>{children}</div> : null}
      {actions ? <div className="panel-actions">{actions}</div> : null}
    </div>
  </div>;
}

export function PanelLogButton() {
  const task = usePanelTask();
  return <div className="panel-log">
    <Button variant="ghost" size="sm" disabled={task.working}
      onClick={() => void task.run(() => bridge.OpenLogFolder(),
        "Couldn't open the log file. Try again, or ask the person who looks after this computer.")}>
      <FileText aria-hidden="true" />Open log file
    </Button>
    {task.error ? <p className="panel-inline-error" role="alert">{task.error}</p> : null}
  </div>;
}

export function PanelPageHeader({ title, subtitle, children }: {
  title: string;
  subtitle?: string;
  children?: ReactNode;
}) {
  const care = useCare();
  const task = usePanelTask();
  const status = panelStatus(care);
  return <header className="panel-page-header">
    <div className="panel-grow">
      <h1 className="panel-page-title">{title}</h1>
      {subtitle ? <p className="panel-page-subtitle">{subtitle}</p> : null}
    </div>
    {children ?? <Button variant="default" size="sm" className="panel-address-pill"
      aria-label={`Open ${care.mdnsName}`} disabled={!status.available || task.working}
      onClick={() => void task.run(() => bridge.OpenURL(`https://${care.mdnsName}/`),
        "Couldn't open CARE in your browser. Try again, or type the clinic address into your browser.")}>
      <Globe aria-hidden="true" /><span>{care.mdnsName}</span><ArrowUpRight aria-hidden="true" />
    </Button>}
    {task.error ? <p className="panel-inline-error panel-header-error" role="alert">{task.error}</p> : null}
  </header>;
}

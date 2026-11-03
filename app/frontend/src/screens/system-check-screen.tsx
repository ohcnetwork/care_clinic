import {
  AlertTriangle,
  CheckCircle2,
  Cpu,
  HardDrive,
  HelpCircle,
  Layers,
  MemoryStick,
  Monitor,
  RefreshCw,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import logoMark from "@/assets/care-logo-mark.svg";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import type { SystemCheck, SystemReport } from "@/types";

import "./role-screen.css";
import "./system-check-screen.css";

export const SYSTEM_CHECK_REVIEWED = "care.systemCheckReviewed";

const ICONS: Record<SystemCheck["id"], LucideIcon> = {
  os: Monitor,
  processor: Cpu,
  memory: MemoryStick,
  storage: HardDrive,
  virtualization: Layers,
};

const STATUS: Record<SystemCheck["level"], { label: string; Icon: LucideIcon }> = {
  ok: { label: "Meets", Icon: CheckCircle2 },
  warn: { label: "Below requirement", Icon: AlertTriangle },
  unknown: { label: "Not checked", Icon: HelpCircle },
};

const PLACEHOLDERS: { id: SystemCheck["id"]; label: string }[] = [
  { id: "os", label: "Operating system" },
  { id: "processor", label: "Processor" },
  { id: "memory", label: "Memory (RAM)" },
  { id: "storage", label: "Free storage" },
  { id: "virtualization", label: "Virtualization" },
];

/**
 * Compares this computer with what CARE needs before anyone picks a role. It
 * only warns: a computer that falls short can still continue.
 */
export function SystemCheckScreen({ onContinue }: { onContinue: () => void }) {
  const [report, setReport] = useState<SystemReport | null>(null);
  const [error, setError] = useState("");
  const [checking, setChecking] = useState(true);
  const run = useRef(0);

  const check = useCallback(async () => {
    const id = ++run.current;
    setChecking(true);
    setError("");
    try {
      const next = await bridge.SystemRequirements();
      if (id !== run.current) return;
      setReport(next);
    } catch (err) {
      if (id !== run.current) return;
      setError(errorText(err));
    } finally {
      if (id === run.current) setChecking(false);
    }
  }, []);

  useEffect(() => {
    void check();
  }, [check]);

  useEffect(() => {
    if (report && !report.applicable) onContinue();
  }, [report, onContinue]);

  const checks = report?.checks ?? [];
  const warnings = checks.filter((c) => c.level === "warn");
  const unknown = checks.filter((c) => c.level === "unknown");
  const shortfall = warnings.length > 0;

  return (
    <div className="start-screen system-screen">
      <aside className="start-brand-panel" aria-label="CARE Clinic">
        <div className="start-brand">
          <img src={logoMark} alt="" />
          <span>CARE Clinic</span>
        </div>
        <div className="start-hero">
          <h2>First, a quick look at this computer.</h2>
          <p className="system-hero-note">
            CARE runs the whole clinic's records on this computer, so it needs enough memory, space and a
            supported system to stay fast and reliable.
          </p>
        </div>
      </aside>
      <main className="start-main" aria-labelledby="system-title">
        <div className="start-body system-body">
          <div className="start-kicker">Before you start</div>
          <h1 id="system-title">System requirements</h1>
          <p className="start-subtitle">How this computer compares with what CARE needs.</p>

          <Summary checking={checking} error={error} warnings={warnings.length} unknown={unknown.length} />

          <ul className="system-list" aria-label="Requirements" aria-busy={checking}>
            {checking && !report
              ? PLACEHOLDERS.map((p) => <PlaceholderRow key={p.id} id={p.id} label={p.label} />)
              : checks.map((c) => <Row key={c.id} check={c} />)}
          </ul>

          <div className="system-actions">
            <Button
              variant="primary"
              className="start-primary system-continue"
              disabled={checking && !report && !error}
              onClick={onContinue}
            >
              {shortfall || error ? "Continue anyway" : "Continue"}
            </Button>
            <Button variant="ghost" className="system-recheck" disabled={checking} onClick={() => void check()}>
              <RefreshCw aria-hidden="true" />
              Check again
            </Button>
          </div>
        </div>
      </main>
    </div>
  );
}

function Summary({ checking, error, warnings, unknown }: {
  checking: boolean;
  error: string;
  warnings: number;
  unknown: number;
}) {
  let tone = "ok";
  let Icon: LucideIcon | null = CheckCircle2;
  let title = "This computer meets CARE's requirements";
  let detail = "Everything looks good. Continue to set up CARE.";
  if (checking) {
    tone = "pending";
    Icon = null;
    title = "Checking this computer\u2026";
    detail = "This takes a few seconds.";
  } else if (error) {
    tone = "unknown";
    Icon = HelpCircle;
    title = "Couldn't check this computer";
    detail = `${error} You can still continue; setup checks what it needs again.`;
  } else if (warnings > 0) {
    tone = "warn";
    Icon = AlertTriangle;
    title = "This computer doesn't meet all of CARE's requirements";
    detail = `CARE may not work as expected. Review the ${warnings === 1 ? "item" : `${warnings} items`} below. You can still continue.`;
  } else if (unknown > 0) {
    tone = "unknown";
    Icon = HelpCircle;
    title = "Some things couldn't be checked";
    detail = "Everything else meets CARE's requirements. Setup checks what it needs again.";
  }
  return (
    <div className={`system-summary system-summary-${tone}`} role="status" aria-live="polite">
      <span className="system-summary-icon">
        {Icon ? <Icon aria-hidden="true" /> : <Spinner className="size-5" />}
      </span>
      <div>
        <p className="system-summary-title">{title}</p>
        <p className="system-summary-detail">{detail}</p>
      </div>
    </div>
  );
}

function Row({ check }: { check: SystemCheck }) {
  const Icon = ICONS[check.id] ?? Monitor;
  const status = STATUS[check.level];
  const messageId = `system-${check.id}-message`;
  return (
    <li className={`system-row system-row-${check.level}`} aria-describedby={check.message ? messageId : undefined}>
      <span className="system-row-icon"><Icon aria-hidden="true" /></span>
      <div className="system-row-main">
        <div className="system-row-head">
          <strong className="system-row-label">{check.label}</strong>
          <span className={`system-pill system-pill-${check.level}`}>
            <status.Icon aria-hidden="true" />
            {status.label}
          </span>
        </div>
        <div className="system-row-facts">
          <span className="system-row-value">{check.value}</span>
          <span className="system-row-requirement">Required: {check.requirement}</span>
        </div>
        {check.message ? <p id={messageId} className="system-row-message">{check.message}</p> : null}
      </div>
    </li>
  );
}

function PlaceholderRow({ id, label }: { id: SystemCheck["id"]; label: string }) {
  const Icon = ICONS[id];
  return (
    <li className="system-row system-row-pending">
      <span className="system-row-icon"><Icon aria-hidden="true" /></span>
      <div className="system-row-main">
        <div className="system-row-head">
          <strong className="system-row-label">{label}</strong>
          <span className="system-skeleton system-skeleton-pill" />
        </div>
        <div className="system-row-facts">
          <span className="system-skeleton" />
        </div>
      </div>
    </li>
  );
}

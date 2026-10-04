import { memo } from "react";
import { Archive, ArrowDownToLine, ArrowUpRight, HardDrive, LayoutDashboard, Puzzle, ScrollText, Settings } from "lucide-react";

import logoMark from "@/assets/care-logo-mark.svg";
import { bridge } from "@/lib/bridge";
import { cn } from "@/lib/utils";
import { panelStatus } from "@/screens/panel/panel-status";
import { usePanelTask } from "@/screens/panel/panel-ui";
import { useCare, type PanelTab, type SetupStep, type SystemState } from "@/state/care-store";

const SETUP_STEPS: { id: SetupStep; label: string }[] = [
  { id: "checks", label: "Computer check" },
  { id: "backup", label: "Backup" },
  { id: "admin", label: "Admin login" },
  { id: "install", label: "Install and start" },
];

const PANEL_TABS: { id: PanelTab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "backups", label: "Backups" },
  { id: "plugins", label: "Plugins" },
  { id: "updates", label: "Updates" },
  { id: "advanced", label: "Advanced" },
];

export const Rail = memo(function Rail({ variant, locked = false, updateCount = 0 }: {
  variant?: "panel";
  locked?: boolean;
  updateCount?: number;
} = {}) {
  const { flow, openStep, stepsDone, tab, setTab, busy, busyLabel, system, systemDetail, version } =
    useCare();
  const inPanel = flow === "panel";
  // Everything past the setup form is "install and start" as far as the rail
  // is concerned — clinic details, the run itself and the failure screen.
  const activeStep: SetupStep = flow === "setup" ? openStep : "install";

  if (variant === "panel") return <PanelRail locked={locked} updateCount={updateCount} />;

  return (
    <aside className="flex w-[318px] flex-none flex-col bg-brand-deep px-6 py-[26px] text-brand-bg">
      <div className="flex items-center gap-[13px]">
        <img
          src={logoMark}
          alt="CARE"
          className="block h-[52px] w-auto brightness-0 invert"
        />
        <div>
          <div className="flex items-baseline gap-2">
            <div className="text-base leading-tight font-bold text-white">CARE Clinic</div>
            {version ? (
              <span className="font-mono text-[11px] text-brand-pale/70">{version}</span>
            ) : null}
          </div>
          <div className="text-[12.5px] text-brand-pale">
            {inPanel ? "Control panel" : "First-time setup"}
          </div>
        </div>
      </div>

      <div className="mt-6 mb-5 h-px bg-white/[0.18]" />

      {inPanel ? (
        <nav className="flex flex-col gap-[3px]">
          {PANEL_TABS.map((item) => {
            const active = tab === item.id;
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => setTab(item.id)}
                className={cn(
                  "flex cursor-pointer items-center gap-[11px] rounded-lg px-3 py-[11px] text-left text-[13.5px] font-semibold text-[#d3f5e5] transition-colors",
                  active ? "bg-white/[0.14] text-white" : "hover:bg-white/[0.07]",
                )}
              >
                <span
                  className={cn(
                    "size-[7px] flex-none rounded-full",
                    active ? "bg-white" : "bg-white/35",
                  )}
                />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>
      ) : (
        <div className="flex flex-col gap-0.5">
          {SETUP_STEPS.map((step, i) => {
            const done = stepsDone[step.id];
            return (
              <div
                key={step.id}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-2.5 py-[11px] text-[13.5px] text-[#d3f5e5]",
                  activeStep === step.id && "bg-white/[0.14]",
                  done && "text-white",
                )}
              >
                <span
                  className={cn(
                    "flex size-[22px] flex-none items-center justify-center rounded-full text-xs font-bold",
                    done ? "bg-white text-brand-ink" : "bg-white/[0.18] text-[#e3fbf0]",
                  )}
                >
                  {done ? "✓" : i + 1}
                </span>
                <span>{step.label}</span>
              </div>
            );
          })}
        </div>
      )}

      <div className="flex-1" />

      {inPanel ? (
        <RailStatus busy={busy} busyLabel={busyLabel} system={system} unreachable={!!systemDetail} />
      ) : null}
    </aside>
  );
});

const CLINIC_TABS = [
  { id: "overview", label: "Overview", icon: LayoutDashboard },
  { id: "backups", label: "Backups", icon: Archive },
  { id: "storage", label: "Storage", icon: HardDrive },
  { id: "plugins", label: "Plugins", icon: Puzzle },
  { id: "updates", label: "Updates", icon: ArrowDownToLine },
  { id: "advanced", label: "Advanced", icon: Settings },
] as const;

function PanelRail({ locked, updateCount }: { locked: boolean; updateCount: number }) {
  const care = useCare();
  const task = usePanelTask();
  const status = panelStatus(care);
  return <aside className="care-panel-rail" aria-label="CARE Clinic control panel">
    <div className="care-panel-rail-brand">
      <img src={logoMark} alt="" />
      <div>CARE Clinic<p>Control panel</p></div>
    </div>
    <div className="care-panel-rail-sep" />
    <div className="care-panel-rail-kicker">Clinic</div>
    <nav className="care-panel-rail-nav" aria-label="Clinic sections">
      {CLINIC_TABS.map(({ id, label, icon: Icon }) => <button key={id} type="button"
        className="care-panel-rail-link" aria-current={care.tab === id ? "page" : undefined}
        disabled={locked} onClick={() => care.setTab(id)}>
        <Icon aria-hidden="true" /><span>{label}</span>
        {id === "updates" && updateCount > 0 ? <span className="care-panel-rail-count"
          aria-label={`${updateCount} ${updateCount === 1 ? "update" : "updates"} available`}>{updateCount}</span> : null}
      </button>)}
    </nav>
    <div className="care-panel-rail-spacer" />
    <button type="button" className="care-panel-rail-link care-panel-rail-docs" disabled={task.working}
      onClick={() => void task.run(() => bridge.OpenURL("https://docs.ohc.network/"),
        "Couldn't open the documentation. Try again when this computer is connected to the internet.")}>
      <ScrollText aria-hidden="true" /><span>Docs</span><ArrowUpRight aria-hidden="true" />
    </button>
    {task.error ? <p className="care-panel-rail-error" role="alert">{task.error}</p> : null}
    <div className="care-panel-rail-status" role="status" aria-label={`Clinic status: ${status.label}`}>
      <span className={`care-panel-rail-dot panel-tone-${status.tone}`} />
      <div>{status.label}
        {care.busy && status.available ? <p>{care.busyLabel}…</p> : null}
      </div>
    </div>
    {care.version ? <div className="care-panel-rail-version">v{care.version.replace(/^v/, "")}</div> : null}
  </aside>;
}

function RailStatus({
  busy,
  busyLabel,
  system,
  unreachable,
}: {
  busy: boolean;
  busyLabel: string;
  system: SystemState;
  unreachable: boolean;
}) {
  const dot = busy
    ? "bg-[#fdba8c]"
    : system === "running"
      ? "bg-[#31c48d]"
      : system === "partial"
        ? "bg-[#fdba8c]"
        : "bg-[#f98080]";
  const label = busy
    ? `${busyLabel}…`
    : system === "running"
      ? "Running"
      : system === "partial"
        ? "Starting…"
        : system === "unknown"
          ? unreachable
            ? "Can't check"
            : "checking…"
          : "Stopped";

  return (
    <div className="flex items-center gap-2.5 rounded-[11px] bg-white/10 px-[13px] py-3 text-[13px] font-semibold text-white">
      <span className={cn("size-[9px] flex-none rounded-full", dot)} />
      <span>{label}</span>
    </div>
  );
}

import { memo } from "react";

import logoMark from "@/assets/care-logo-mark.svg";
import { cn } from "@/lib/utils";
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

export const Rail = memo(function Rail() {
  const { flow, openStep, stepsDone, tab, setTab, busy, busyLabel, system, systemDetail, version } =
    useCare();
  const inPanel = flow === "panel";
  // Everything past the setup form is "install and start" as far as the rail
  // is concerned — clinic details, the run itself and the failure screen.
  const activeStep: SetupStep = flow === "setup" ? openStep : "install";

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
            <div className="text-base leading-tight font-bold text-white">CARE Desktop</div>
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

import { ArrowDownToLine, ArrowUpRight, HardDrive, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";

import { Screen, ScreenBody } from "@/components/screen";
import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { useCare, type PanelTab } from "@/state/care-store";
import type { StorageReport } from "@/types";
import { AdvancedTab } from "./advanced-tab";
import { BackupsTab } from "./backups-tab";
import { OverviewTab } from "./overview-tab";
import { TroubleDialog } from "./trouble-dialog";

const TAB_META: Record<PanelTab, { title: string; subtitle: string }> = {
  overview: { title: "Overview", subtitle: "Your clinic server at a glance." },
  backups: { title: "Backups", subtitle: "Safe copies of your patient data." },
  advanced: { title: "Advanced", subtitle: "Technical options for this clinic." },
};

export function PanelScreen() {
  const {
    tab,
    setTab,
    mdnsName,
    reloadBackups,
    trouble,
    careUpdate,
    applyCareUpdate,
    dismissCareUpdate,
    busy,
    storage,
  } = useCare();
  const [diagnosing, setDiagnosing] = useState(false);
  const meta = TAB_META[tab];

  // Opening the tab is the refresh gesture, as it was before.
  useEffect(() => {
    if (tab === "backups") void reloadBackups();
  }, [tab, reloadBackups]);

  return (
    <Screen>
      {diagnosing ? <TroubleDialog onClose={() => setDiagnosing(false)} /> : null}

      {/* A banner, not a pop-up: this panel often sits minimised on a shelf PC,
          and a window that steals focus on a blip gets dismissed unread. It
          stays until the clinic answers again - there is nothing to dismiss. */}
      {trouble ? (
        <div className="flex items-center gap-3 border-b border-danger-bg bg-danger-tint px-[34px] py-3">
          <TriangleAlert className="size-4 flex-none text-danger-ink" strokeWidth={2.2} />
          <div className="min-w-0 flex-1 text-[13px] leading-[1.45] text-danger-ink">
            Staff can&apos;t reach the clinic right now.
          </div>
          <Button variant="primary" onClick={() => setDiagnosing(true)}>
            See what&apos;s wrong
          </Button>
        </div>
      ) : null}

      {/* The update is already downloaded and built - this asks for a moment of
          downtime, not for a wait. "Later" defers it to the next start, where
          it costs nothing, so neither answer is the wrong one. */}
      {storage?.level === "critical" && storage.headline && !trouble ? (
        <div className="flex items-center gap-3 border-b border-danger-bg bg-danger-tint px-[34px] py-3">
          <HardDrive className="size-4 flex-none text-danger-ink" strokeWidth={2.2} />
          <div className="min-w-0 flex-1 text-[13px] leading-[1.45] text-danger-ink">
            {storage.headline}{" "}
            <span className="text-muted-foreground">{storageAdvice(storage)}</span>
          </div>
          {tab !== storageTab(storage) ? (
            <Button variant="primary" onClick={() => setTab(storageTab(storage))}>
              See details
            </Button>
          ) : null}
        </div>
      ) : null}

      {careUpdate && !trouble ? (
        <div className="flex items-center gap-3 border-b border-line bg-brand-bg px-[34px] py-3">
          <ArrowDownToLine className="size-4 flex-none text-brand-ink" strokeWidth={2.2} />
          <div className="min-w-0 flex-1 text-[13px] leading-[1.45] text-brand-ink">
            A CARE update is ready to install.{" "}
            <span className="text-muted-foreground">
              {careUpdate.backend
                ? "Takes about a minute; staff are signed out briefly."
                : "Takes a few seconds."}
            </span>
          </div>
          <Button disabled={busy} onClick={() => void dismissCareUpdate()}>
            Later
          </Button>
          <Button variant="primary" disabled={busy} onClick={() => void applyCareUpdate()}>
            Install now
          </Button>
        </div>
      ) : null}

      <div className="flex items-center gap-4 px-[34px] pt-[26px] pb-[18px]">
        <div className="min-w-0 flex-1">
          <h1 className="text-[23px] font-bold tracking-[-0.015em] text-ink">{meta.title}</h1>
          <p className="mt-[5px] text-[13.5px] text-muted-foreground">{meta.subtitle}</p>
        </div>
        <Button variant="soft" onClick={() => void bridge.OpenURL(`https://${mdnsName}/`)}>
          <span className="font-mono">{mdnsName}</span>
          <ArrowUpRight className="size-3.5" strokeWidth={2.2} />
        </Button>
      </div>

      {/* All three stay mounted so half-finished edits survive a tab switch. */}
      <ScreenBody>
        <div hidden={tab !== "overview"}>
          <OverviewTab />
        </div>
        <div hidden={tab !== "backups"}>
          <BackupsTab />
        </div>
        <div hidden={tab !== "advanced"}>
          <AdvancedTab />
        </div>
      </ScreenBody>
    </Screen>
  );
}

function backupProblem(storage: StorageReport): boolean {
  return (
    storage.last_run.state === "failed" || storage.stale || storage.backup.level === "critical"
  );
}

function storageTab(storage: StorageReport): PanelTab {
  const driveFull = (storage.drives ?? []).some((d) => d.level === "critical");
  return !driveFull && backupProblem(storage) ? "backups" : "overview";
}

function storageAdvice(storage: StorageReport): string {
  if ((storage.drives ?? []).some((d) => d.level === "critical")) {
    return "Free up space now, or the clinic will stop saving data.";
  }
  const run = storage.last_run;
  if (run.state === "failed" && run.reason !== "disk_full") {
    return run.message ? `Cause: ${run.message}. Try Back up now.` : "Try Back up now.";
  }
  if (run.state === "failed" || storage.backup.level === "critical") {
    return "Free up space on the backup drive or choose another folder.";
  }
  return "Check the backup folder is plugged in and CARE is running.";
}

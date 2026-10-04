import type { SystemState } from "@/state/care-store";
import type { BackupRun, StorageReport } from "@/types";

export type PanelTone = "ok" | "warning" | "danger" | "neutral";

type StatusInput = {
  system: SystemState;
  systemDetail: string;
  trouble: boolean;
  busy: boolean;
  busyLabel: string;
};

export function panelStatus({ system, systemDetail, trouble, busy, busyLabel }: StatusInput) {
  const operation = busy ? ({
    Starting: ["Starting", "CARE is starting. Keep this computer on."],
    Stopping: ["Stopping", "Staff won't be able to use CARE until it is started again."],
    Restarting: ["Restarting", "Staff will be signed out briefly. Keep this computer on."],
    Rebuilding: ["Updating", "CARE is being rebuilt. Keep this computer on."],
    "Applying plugins": ["Updating", "Changes are being applied. Staff may be signed out briefly."],
    "Updating CARE": ["Updating", "The CARE software update is being installed. Keep this computer on."],
    Restoring: ["Restoring", "CARE is paused while the backup is restored. Keep this computer on."],
    Uninstalling: ["Removing", "CARE is being removed from this computer."],
  } as Record<string, [string, string]>)[busyLabel] : undefined;

  if (operation) return {
    label: operation[0], detail: operation[1], tone: "warning" as PanelTone,
    working: true, available: false,
  };
  if (system === "unknown") return {
    label: systemDetail ? "Can't check" : "Checking",
    detail: systemDetail
      ? "CARE Clinic couldn't check the clinic. Check again, or open the log file for support."
      : "Checking whether CARE is running on this computer.",
    tone: (systemDetail ? "danger" : "neutral") as PanelTone,
    working: !systemDetail, available: false,
  };
  if (trouble) return {
    label: "Not responding", detail: "The clinic isn't answering on this computer.",
    tone: "danger" as PanelTone, working: false, available: false,
  };
  if (system === "running") return {
    label: "Running", detail: "Staff can open CARE on the clinic Wi-Fi.",
    tone: "ok" as PanelTone, working: false, available: true,
  };
  if (system === "partial") return {
    label: "Starting", detail: "CARE isn't ready yet. Check what's needed if it doesn't become ready.",
    tone: "warning" as PanelTone, working: true, available: false,
  };
  return {
    label: "Stopped", detail: "Staff can't use CARE until it's started.",
    tone: "neutral" as PanelTone, working: false, available: false,
  };
}

export function backupFailureDetail(reason: BackupRun["reason"]): string {
  if (reason === "disk_full") return "Make room in the backup folder or choose another location.";
  if (reason === "database_unavailable") return "The database wasn't ready. Wait for CARE to be running, then try Back up now. Saved backups are unaffected.";
  return "Check the backup folder, then try Back up now.";
}

export function storageProblem(report: StorageReport | null) {
  if (!report) return null;
  const drives = report.drives ?? [];
  if (drives.some((item) => item.level === "critical")) return {
    title: "The clinic's drive is almost full.",
    detail: "Free up space so CARE can keep saving data.",
    tab: "storage" as const,
    tone: "danger" as PanelTone,
  };
  if (report.last_run.state === "failed") return {
    title: "The last backup didn't finish.",
    detail: backupFailureDetail(report.last_run.reason),
    tab: "backups" as const, tone: "danger" as PanelTone,
  };
  if (report.backup.level === "critical") return {
    title: "The next backup won't fit.",
    detail: "Free up space or choose another backup location.",
    tab: "backups" as const,
    tone: "danger" as PanelTone,
  };
  if (report.stale) return {
    title: "No recent backup.",
    detail: "Check the backup folder and that CARE is running.",
    tab: "backups" as const, tone: "danger" as PanelTone,
  };
  if (drives.some((item) => item.level === "low")) return {
    title: "The clinic's drive is getting full.",
    detail: "Check Storage and free up space soon.",
    tab: "storage" as const, tone: "warning" as PanelTone,
  };
  if (report.backup.level === "low") return {
    title: "The backup folder is running out of room.",
    detail: "Free up space or choose another backup location.",
    tab: "backups" as const, tone: "warning" as PanelTone,
  };
  return null;
}

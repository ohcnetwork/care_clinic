import { AlertCircle, Check, Info, ScrollText } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { Backup, BackupSpace } from "@/types";

export type BackupProblem = { title: string; detail: string };

export function backupProblem(
  error: unknown,
  context: "folder" | "backup" | "inspect" | "restore" | "recovery" | "storage",
): BackupProblem {
  const text = errorText(error).toLowerCase();
  if (/something else is still running|wait for it to finish|another .*running/.test(text)) {
    return { title: "Please wait for the current task", detail: "Something else is still running. Try again after it finishes." };
  }
  if (/restore.*unfinished|restore.*recovery data|unfinished restore|restore recovery metadata/.test(text)) {
    return { title: "An earlier restore needs attention", detail: "Open Overview and start CARE to recover safely before making any other changes." };
  }
  if (/server role|requires.*server|not set up|not.*installed|cleanup is incomplete|closing/.test(text)) {
    return { title: "This clinic isn't ready for that", detail: "Return to Overview and check the clinic before trying again." };
  }
  if (context === "folder") {
    if (/recovery file.*separate|recovery materials/.test(text)) {
      return { title: "Keep the recovery file separate", detail: "The backup recovery file cannot be inside the backup folder. Choose a different location." };
    }
    if (/another.*installation|another.*clinic|foreign/.test(text)) {
      return { title: "This folder belongs to another clinic", detail: "Leave its existing backups intact and choose a different location." };
    }
    if (/outside|protected/.test(text)) {
      return { title: "Choose a folder outside CARE", detail: "Backups cannot be kept inside CARE's own folders. Choose a different location." };
    }
    if (/read.only/.test(text)) {
      return { title: "That folder is read-only", detail: "CARE cannot save backups there. Choose a different location." };
    }
    if (/permission|not allowed|isn['’]t allowed|access.*denied/.test(text)) {
      return { title: "CARE can't write to that folder", detail: "This computer does not have permission to save there. Choose a different location." };
    }
    if (/not enough|more free space|no space|disk full/.test(text)) {
      return { title: "There isn't enough room in that folder", detail: "Free up space or choose a different location." };
    }
    if (/not.*folder|not a directory|absolute.*path/.test(text)) {
      return { title: "Choose a backup folder", detail: "Select a folder, not a file, in a different location." };
    }
    if (/isn't there|no longer|no such file|couldn't open/.test(text)) {
      return { title: "That folder isn't available", detail: "Check that you can open it, or choose a different location." };
    }
    return { title: "The backup folder couldn't be changed", detail: "Your earlier backups have not been moved. Try again or choose a different location." };
  }
  if (context === "inspect") {
    if (/no file chosen|isn't a care|not a database|invalid backup timestamp|regular.*file|not.*regular|nonempty/.test(text)) {
      return { title: "Choose a CARE backup file", detail: "Select the database backup ending in .dump or .dump.enc from a clinic's backup folder." };
    }
    return { title: "That backup file couldn't be opened", detail: "Check that the file is still available, then choose it again." };
  }
  if (context === "recovery") {
    return /different clinic|does not match|doesn't match/.test(text)
      ? { title: "That recovery file doesn't match", detail: "Choose the recovery file saved when this backup's clinic was set up." }
      : { title: "That recovery file couldn't be used", detail: "Choose the original backup recovery file saved during setup, not the Desktop recovery codes." };
  }
  if (context === "restore") {
    if (/password.*does not match|password.*incorrect|password.*doesn't match/.test(text)) {
      return { title: "That Desktop admin password doesn't match", detail: "Try again, or use a Desktop recovery code in Advanced to reset it." };
    }
    if (/recovery file|recovery key/.test(text)) {
      return backupProblem(error, "recovery");
    }
    if (/decryption|dump validation|archive validation|staged extraction/.test(text)) {
      return { title: "The backup couldn't be checked", detail: "The backup or recovery file may not match or may be damaged. Open Overview to check whether CARE needs to recover before trying again." };
    }
    if (/cannot read backup|couldn't open that file|nonempty regular|not a database|invalid backup timestamp|same backup|not a files archive/.test(text)) {
      return { title: "The selected backup is no longer usable", detail: "Choose the backup again. Keep its matching uploaded-files archive beside it, if you have one." };
    }
    return { title: "Restore didn't finish", detail: "Open Overview to check the clinic before staff use CARE again. If recovery is needed, start CARE there before trying another restore." };
  }
  if (context === "storage") {
    return { title: "Storage couldn't be checked", detail: "Try Refresh again. Open the log file for support if this keeps happening." };
  }
  return { title: "The backup didn't finish", detail: "Check that the backup folder is available and has enough space, then try again." };
}

export function BackupLogButton() {
  const { log } = useCare();
  const opening = useRef(false);
  const [working, setWorking] = useState(false);
  const [failed, setFailed] = useState(false);
  const open = async () => {
    if (opening.current) return;
    opening.current = true;
    setWorking(true);
    setFailed(false);
    try {
      await bridge.OpenLogFolder();
    } catch (error) {
      log(`backup log: ${errorText(error)}`);
      setFailed(true);
    } finally {
      opening.current = false;
      setWorking(false);
    }
  };
  return (
    <div className="care-backups-log">
      <Button type="button" variant="ghost" size="sm" disabled={working} onClick={() => void open()}>
        <ScrollText aria-hidden="true" />
        {working ? "Opening log…" : "Open log file for support"}
      </Button>
      {failed ? <span role="alert">The log file couldn't be opened. Try again.</span> : null}
    </div>
  );
}

export function BackupNotice({
  title, children, tone = "danger", actions, log = true,
}: {
  title: string;
  children?: ReactNode;
  tone?: "danger" | "warning" | "success" | "info";
  actions?: ReactNode;
  log?: boolean;
}) {
  const Icon = tone === "success" ? Check : tone === "info" ? Info : AlertCircle;
  return (
    <div className={`care-backups-notice care-backups-notice--${tone}`} role={tone === "danger" ? "alert" : "status"}>
      <Icon aria-hidden="true" />
      <div className="care-backups-notice-copy">
        <strong>{title}</strong>
        {children ? <p>{children}</p> : null}
        {actions || log ? (
          <div className="care-backups-notice-actions">
            {actions}
            {log ? <BackupLogButton /> : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function backupDate(name: string): Date | null {
  const parts = /^care-(?:manual-)?(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})\.dump(?:\.enc)?$/.exec(name);
  if (!parts) return null;
  const [year, month, day, hour, minute, second] = parts.slice(1).map(Number);
  // Names carry a wall-clock time, not a timezone. Preserve the host's label.
  const date = new Date(year, month - 1, day, hour, minute, second);
  return date.getFullYear() === year && date.getMonth() === month - 1 &&
    date.getDate() === day && date.getHours() === hour &&
    date.getMinutes() === minute && date.getSeconds() === second ? date : null;
}

export function backupDay(date: Date, now = new Date()): string {
  if (date.toDateString() === now.toDateString()) return "Today";
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) return "Yesterday";
  return "Earlier";
}

export function backupWhen(date: Date | null, now = new Date()): string {
  if (!date || !Number.isFinite(date.getTime())) return "Date unavailable";
  const day = backupDay(date, now);
  const label = day === "Earlier" ? date.toLocaleDateString("en-GB", {
    weekday: "long", day: "numeric", month: "short",
    ...(date.getFullYear() !== now.getFullYear() ? { year: "numeric" as const } : {}),
  }) : day;
  return `${label}, ${date.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false })}`;
}

export function backupGroups(backups: Backup[], now: Date) {
  const groups = new Map<string, { backup: Backup; date: Date | null }[]>();
  const sorted = backups.map((backup) => ({ backup, date: backupDate(backup.db_dump) }))
    .sort((left, right) => (right.date?.getTime() ?? 0) - (left.date?.getTime() ?? 0));
  for (const entry of sorted) {
    const group = entry.date ? backupDay(entry.date, now) : "Date unavailable";
    groups.set(group, [...(groups.get(group) ?? []), entry]);
  }
  return [...groups];
}

export function backupSpaceSummary(space: BackupSpace): string {
  if (space.level === "unknown" || !space.total) return "Free space couldn't be checked";
  if (space.free < space.need) return "Not enough room for the next backup";
  if (space.free < 2 * space.need) return "Room for only one more backup";
  if (space.days_left >= 365) {
    const years = Math.round(space.days_left / 365);
    return `Room for about ${years} ${years === 1 ? "year" : "years"} of backups`;
  }
  if (space.days_left > 0) {
    return `Room for about ${space.days_left} ${space.days_left === 1 ? "day" : "days"} of backups`;
  }
  return "Space available for backups";
}

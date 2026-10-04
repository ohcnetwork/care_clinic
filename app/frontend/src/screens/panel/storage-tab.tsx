import { Archive, Box, HardDrive, RefreshCw, Trash2 } from "lucide-react";
import { useEffect } from "react";

import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { diskSize } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { StorageDrive, StorageLevel } from "@/types";
import { usePanelRequirements } from "./panel-requirements";
import { PanelLogButton, PanelNotice, PanelPageHeader, usePanelTask } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

function usage(free: number, total: number, level: StorageLevel) {
  if (level === "unknown" || !Number.isFinite(total) || total <= 0 || !Number.isFinite(free) || free < 0 || free > total) return null;
  return { used: total - free, percent: Math.round((total - free) / total * 100) };
}

function driveAdvice(drive: StorageDrive) {
  if (drive.level === "unknown") return "Couldn't read this drive's free space. Check again, or open the log file for support.";
  if (drive.level === "critical") return "This drive is almost full. Free up space so CARE can keep saving data.";
  if (drive.level === "low") return "Free space is running low. Make room soon so CARE can keep saving data.";
  return drive.id === "vm"
    ? "Includes clinic records, uploads and the software that runs CARE. Other apps using Rancher Desktop may also be included."
    : "Room for the clinic's data and Rancher Desktop's disk to grow.";
}

export function StorageTab() {
  const care = useCare();
  const requirements = usePanelRequirements();
  const updateLock = usePanelUpdateLock();
  const refresh = usePanelTask();
  const cleanup = usePanelTask();
  const report = care.storage;
  const checkedAt = report?.checked_at ? new Date(report.checked_at * 1000).toLocaleTimeString([], {
    hour: "2-digit", minute: "2-digit",
  }) : "";
  const recheck = () => refresh.run(care.recheckStorage,
    "Couldn't check storage. Try again, or open the log file for support.");
  useEffect(() => {
    if (care.tab === "storage") void recheck();
    // A tab visit is a refresh; busy transitions are handled by the store.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [care.tab, care.recheckStorage]);
  const busy = care.busy || requirements.working;
  const freeing = care.busy && care.busyLabel === "Freeing space";
  const free = () => {
    if (busy || updateLock.isActive() || cleanup.working || care.restorePending) return;
    void cleanup.run(() => care.runAction("free-space"),
      "Couldn't start freeing up space. Try again, or open the log file for support.");
  };
  const backup = report?.backup;
  const backupUsage = backup ? usage(backup.free, backup.total, backup.level) : null;

  return <div className="panel-page" aria-label="Storage">
    <PanelPageHeader title="Storage"
      subtitle={checkedAt ? `Checked at ${checkedAt} · rechecked every 5 minutes.` : "Space for the clinic and its backups."}>
      <Button size="sm" disabled={refresh.working || busy} onClick={() => void recheck()}>
        {refresh.working ? <Spinner /> : <RefreshCw aria-hidden="true" />}
        {refresh.working ? "Checking…" : "Check now"}
      </Button>
    </PanelPageHeader>
    {care.storageError || refresh.error ? <PanelNotice title="Couldn't check storage" actions={<PanelLogButton />}>
      {report ? "The figures below are from the last successful check. " : ""}
      Try again. If it keeps failing, share the log file with support.
    </PanelNotice> : null}
    {!report && !care.storageError && !refresh.error ? <div className="panel-card panel-card-pad panel-row" role="status">
      <Spinner /><span>Checking storage…</span>
    </div> : null}
    {report && !report.drives?.length ? <PanelNotice tone="warning" title="Drive information isn't available">
      Check again. If it keeps happening, share the log file with support.
    </PanelNotice> : null}
    {(report?.drives ?? []).map((drive) => {
      const data = usage(drive.free, drive.total, drive.level);
      const Icon = drive.id === "vm" ? Box : HardDrive;
      return <section key={drive.id}
        className={`panel-card panel-card-pad panel-storage-card ${drive.id === "vm" ? "panel-storage-vm" : ""} panel-storage-${drive.level === "critical" ? "danger" : drive.level === "low" ? "warning" : "neutral"}`}>
        <div className="panel-row">
          <span className={`panel-tile panel-tile-large ${drive.id === "vm" ? "" : "panel-tile-neutral"}`}><Icon aria-hidden="true" /></span>
          <div className="panel-grow">
            <div className="panel-row panel-between">
              <h2 className="panel-title">{drive.id === "vm" ? "CARE Clinic storage" : "This computer's drive"}</h2>
              {!data ? <span className="panel-small">Space unavailable</span> : drive.id !== "vm" ? <span className="panel-small panel-mono">
                {diskSize(drive.free)} free of {diskSize(drive.total)}
              </span> : null}
            </div>
            {data && drive.id === "vm" ? <>
              <p className="panel-storage-usage"><strong>{diskSize(data.used)}</strong> currently used</p>
              <p className="panel-small">Storage capacity: {diskSize(drive.total)} — this is the space available to grow into, not the amount used.</p>
            </> : data ? <Progress value={data.percent} aria-label="This computer's drive space used" /> : null}
            <p className="panel-small">{driveAdvice(drive)}</p>
            {drive.cleanable ? <div className="panel-actions">
              <Button disabled={busy || updateLock.active || cleanup.working || care.restorePending} onClick={free}>
                {freeing ? <Spinner /> : <Trash2 aria-hidden="true" />}{freeing ? "Freeing up space…" : "Free up space"}
              </Button>
              <span className="panel-small">Removes temporary files and unused software. Clinic records and backups are kept.</span>
            </div> : null}
          </div>
        </div>
      </section>;
    })}
    {backup?.dir ? <section
      className={`panel-card panel-card-pad panel-storage-card panel-storage-${backup.level === "critical" ? "danger" : backup.level === "low" ? "warning" : "neutral"}`}>
      <div className="panel-row">
        <span className="panel-tile panel-tile-large panel-tile-neutral"><Archive aria-hidden="true" /></span>
        <div className="panel-grow">
          <div className="panel-row panel-between">
            <h2 className="panel-title">Backup drive</h2>
            {backupUsage ? <span className="panel-small panel-mono">{diskSize(backup.free)} free of {diskSize(backup.total)}</span>
              : <span className="panel-small">Space unavailable</span>}
          </div>
          {backupUsage ? <Progress value={backupUsage.percent} aria-label="Backup drive space used" /> : null}
          <span className="panel-storage-path">{backup.dir}</span>
          <p className="panel-small">{backup.level === "unknown"
            ? "Couldn't check the backup location. Check that the folder is available, or choose another location."
            : backup.level === "critical" ? "The next backup won't fit. Make room or choose another location."
              : backup.days_left >= 0 ? `Room for about ${backup.days_left} ${backup.days_left === 1 ? "day" : "days"} of backups.`
                : backup.need > 0 ? `The next backup needs about ${diskSize(backup.need)}.`
                  : "Space is checked again before a backup runs."}</p>
          {backup.shares_docker_drive ? <p className="panel-small">This shares the clinic's drive. Keeping a copy on a separate drive protects it if this drive fails.</p> : null}
          {backup.level !== "ok" ? <div className="panel-actions">
            <Button onClick={() => care.setTab("backups")}>Choose another folder</Button>
          </div> : null}
        </div>
      </div>
    </section> : report ? <PanelNotice tone="warning" title="Backup location isn't available"
      actions={<Button onClick={() => care.setTab("backups")}>View backups</Button>}>
      Open Backups to check where copies are being saved.
    </PanelNotice> : null}
    {cleanup.error ? <PanelNotice title="Couldn't free up space" actions={<PanelLogButton />}>{cleanup.error}</PanelNotice> : null}
  </div>;
}

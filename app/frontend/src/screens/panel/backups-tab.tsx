import { Archive, Check, Clock3, Database, FolderOpen, LoaderCircle, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { bridge, onCareEvent } from "@/lib/bridge";
import { diskSize, errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { BackupPolicy } from "@/types";

import {
  backupGroups, BackupNotice, backupProblem, backupSpaceSummary, backupWhen,
  type BackupProblem,
} from "./backup-ui";
import { PanelPageHeader } from "./panel-ui";
import { backupFailureDetail } from "./panel-status";
import { usePanelUpdateLock } from "./panel-update-lock";
import { RestoreBackupFile } from "./restore-backup-dialog";
import "./backups.css";

export function BackupsTab() {
  const updateLock = usePanelUpdateLock();
  const {
    backups, backupsError, busy, busyLabel, restorePending, storage, storageError, log, tab, operationError,
    runAction, reloadBackups, recheckStorage, setTab,
  } = useCare();
  const [dir, setDir] = useState("");
  const [folderLoading, setFolderLoading] = useState(true);
  const [folderProblem, setFolderProblem] = useState<BackupProblem | null>(null);
  const [folderChanged, setFolderChanged] = useState(false);
  const [policy, setPolicy] = useState<BackupPolicy | null>(null);
  const [policyProblem, setPolicyProblem] = useState(false);
  const [working, setWorking] = useState<"folder" | "refresh" | "backup" | null>(null);
  const [actionProblem, setActionProblem] = useState<BackupProblem | null>(null);
  const [backupComplete, setBackupComplete] = useState(false);
  const [now, setNow] = useState(() => new Date());
  const operation = useRef(false);
  const blocked = useRef(busy || restorePending);
  const mounted = useRef(true);
  const space = storage?.backup;
  const run = storage?.last_run;
  const automaticRunning = run?.state === "running" && run.at > 0 &&
    now.getTime() - run.at * 1000 < 6 * 60 * 60 * 1000;
  blocked.current = busy || restorePending || automaticRunning || folderLoading || updateLock.active;

  const readPolicy = useCallback(async () => {
    setPolicy(null);
    try {
      const settings = await bridge.GetBackupPolicy();
      if (mounted.current) {
        setPolicy(settings);
        setPolicyProblem(false);
      }
    } catch (error) {
      if (mounted.current) {
        log(`backup settings: ${errorText(error)}`);
        setPolicy(null);
        setPolicyProblem(true);
      }
    }
  }, [log]);

  const readFolder = useCallback(async () => {
    try {
      const path = await bridge.GetBackupDir();
      if (mounted.current) {
        setDir(path);
        setFolderProblem(null);
      }
    } catch (error) {
      if (mounted.current) {
        log(`backup folder: ${errorText(error)}`);
        setDir("");
        setFolderProblem({
          title: "The backup folder couldn't be checked",
          detail: "Try Refresh again. Your existing backups have not been moved.",
        });
      }
    } finally {
      if (mounted.current) setFolderLoading(false);
    }
  }, [log]);

  useEffect(() => {
    mounted.current = true;
    const clock = window.setInterval(() => setNow(new Date()), 60_000);
    const off = onCareEvent("care-done", (code: number, label?: string) => {
      if (label !== "backup-now") return;
      setBackupComplete(code === 0);
      setActionProblem(code === 0 ? null : backupProblem("", "backup"));
    });
    return () => {
      mounted.current = false;
      window.clearInterval(clock);
      off();
    };
  }, []);

  useEffect(() => {
    if (tab !== "backups") return;
    setFolderLoading(true);
    void readFolder();
    void readPolicy();
  }, [tab, readFolder, readPolicy]);

  const refresh = async () => {
    if (operation.current || blocked.current || updateLock.isActive()) return;
    operation.current = true;
    setWorking("refresh");
    setActionProblem(null);
    try {
      await Promise.all([readFolder(), readPolicy(), reloadBackups(), recheckStorage()]);
      if (mounted.current) setNow(new Date());
    } catch (error) {
      if (mounted.current) setActionProblem(backupProblem(error, "storage"));
    } finally {
      operation.current = false;
      if (mounted.current) setWorking(null);
    }
  };

  const changeFolder = async () => {
    if (operation.current || blocked.current || updateLock.isActive()) return;
    const revision = updateLock.revision();
    operation.current = true;
    setWorking("folder");
    try {
      const chosen = await bridge.ChooseFolder("Choose where backups should go");
      if (!chosen || !mounted.current) return;
      if (revision !== updateLock.revision() || updateLock.isActive()) {
        setFolderProblem({
          title: "Choose the backup folder again",
          detail: "A CARE Desktop update interrupted this change. Your backup folder has not changed.",
        });
        return;
      }
      if (blocked.current) {
        setFolderProblem(backupProblem(restorePending ? "a restore is unfinished" : "something else is still running", "folder"));
        return;
      }
      const target = await bridge.SetBackupDir(chosen);
      if (!mounted.current) return;
      setDir(target);
      setFolderProblem(null);
      setFolderChanged(target !== dir);
      setBackupComplete(false);
      try {
        await Promise.all([reloadBackups(), recheckStorage()]);
      } catch (error) {
        if (mounted.current) setActionProblem(backupProblem(error, "storage"));
      }
    } catch (error) {
      if (mounted.current) {
        log(`backup folder: ${errorText(error)}`);
        await readFolder();
        if (!mounted.current) return;
        setFolderProblem(backupProblem(error, "folder"));
      }
    } finally {
      operation.current = false;
      if (mounted.current) setWorking(null);
    }
  };

  const backUpNow = async () => {
    if (operation.current || blocked.current || updateLock.isActive()) return;
    operation.current = true;
    setWorking("backup");
    setActionProblem(null);
    setBackupComplete(false);
    try {
      await runAction("backup-now");
    } catch (error) {
      if (mounted.current) setActionProblem(backupProblem(error, "backup"));
    } finally {
      operation.current = false;
      if (mounted.current) setWorking(null);
    }
  };

  const backingUp = (busy && busyLabel === "Backing up") || working === "backup";
  const disabled = busy || restorePending || !!working || automaticRunning || folderLoading || updateLock.active;
  const measured = !storageError && !!space && space.total > 0 && space.level !== "unknown" &&
    !!dir && space.dir === dir;
  const tone = measured ? space.level : "unknown";
  const usedPercent = measured ? Math.min(100, Math.max(0, (1 - space.free / space.total) * 100)) : 0;
  const groups = backupGroups(backups, now);
  const lastBackup = storage?.newest_backup_at
    ? backupWhen(new Date(storage.newest_backup_at * 1000), now) : "";
  const lastRun = run?.at ? backupWhen(new Date(run.at * 1000), now) : "";
  const badge = tone === "critical" ? "Low space" : tone === "low" ? "Low space"
    : tone === "ok" ? "Healthy" : folderLoading ? "Checking" : "Not checked";
  const localActionProblem = operationError?.action === "backup-now" ? null : actionProblem;

  return (
    <>
    <PanelPageHeader title="Backups" subtitle="Safe copies of your patient data, made automatically." />
    <div className="care-backups">
      <section className={`care-backups-card care-backups-folder care-backups-folder--${tone}`} aria-label="Backup folder">
        <div className="care-backups-card-row">
          <span className={`care-backups-icon care-backups-icon--${tone}`}><FolderOpen aria-hidden="true" /></span>
          <div className="care-backups-grow">
            <div className="care-backups-eyebrow">Backups are saved to</div>
            <p className="care-backups-folder-path" title={dir}>{dir || (folderLoading ? "Checking backup folder…" : "Folder unavailable")}</p>
            {measured ? (
              <>
                <div className={`care-backups-meter care-backups-meter--${tone}`}
                  role="meter" aria-label="Backup folder space used" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(usedPercent)}>
                  <span style={{ width: `${usedPercent}%` }} />
                </div>
                <div className="care-backups-space">
                  <span>{backupSpaceSummary(space)}</span>
                  <span>{diskSize(space.free)} free of {diskSize(space.total)}</span>
                </div>
              </>
            ) : <p className="care-backups-space-unavailable">Free space {folderLoading ? "is being checked" : "couldn't be checked"}</p>}
          </div>
          <span className={`care-backups-badge care-backups-badge--${tone}`}>
            {tone === "ok" ? <Check aria-hidden="true" /> : null}{badge}
          </span>
          <Button disabled={disabled} onClick={() => void changeFolder()}>
            {working === "folder" ? "Changing folder…" : "Change folder"}
          </Button>
        </div>
        {measured && space.need > 0 ? <p className="care-backups-folder-note">Each backup needs about {diskSize(space.need)}.</p> : null}
        {measured && space.shares_docker_drive ? (
          <p className="care-backups-folder-note">This folder shares the drive with the clinic's data. Keep another copy on a different drive to protect against drive failure.</p>
        ) : null}
      </section>

      {folderProblem ? <BackupNotice title={folderProblem.title}>{folderProblem.detail}</BackupNotice>
        : folderChanged ? (
          <BackupNotice title="The backup folder has changed" tone="success" log={false}>
            New backups will be saved here. Earlier backups stay in the previous folder.
          </BackupNotice>
        ) : null}

      {storageError ? (
        <BackupNotice title="Storage couldn't be checked">
          The last reading may be out of date. Try Refresh again before relying on the available space.
        </BackupNotice>
      ) : null}

      <div className="care-backups-toolbar">
        <Button variant="primary" disabled={disabled} onClick={() => void backUpNow()}>
          {backingUp || automaticRunning ? <LoaderCircle className="care-backups-spin" aria-hidden="true" /> : <Archive aria-hidden="true" />}
          {backingUp || automaticRunning ? "Backing up…" : "Back up now"}
        </Button>
        <div className="care-backups-schedule">
          <span>{policy?.interval_seconds === 86400 ? "Automatic every 24 hours"
            : policy ? `Automatic every ${policy.interval_seconds / 3600} hours`
              : "Automatic backups"}</span>
          <span className="care-backups-retention">{policy
            ? policy.retention_days === 0 ? "Kept forever"
              : `Kept for ${policy.retention_days} ${policy.retention_days === 1 ? "day" : "days"}`
            : policyProblem ? "Retention couldn't be checked" : "Checking retention…"}</span>
        </div>
        {!backupsError ? <span className="care-backups-count">{backups.length} {backups.length === 1 ? "backup" : "backups"}</span> : null}
        <Button variant="ghost" size="sm" disabled={disabled} onClick={() => void refresh()}>
          <RefreshCw aria-hidden="true" className={working === "refresh" ? "care-backups-spin" : undefined} />
          {working === "refresh" ? "Refreshing…" : "Refresh"}
        </Button>
      </div>

      {policyProblem ? (
        <BackupNotice title="The backup settings couldn't be read">
          Refresh to check how long backups are kept, or check Clinic settings in Advanced.
        </BackupNotice>
      ) : null}

      {restorePending ? (
        <BackupNotice title="An earlier restore needs attention"
          actions={<Button onClick={() => setTab("overview")}>Open Overview</Button>}>
          Start CARE from Overview to recover safely before backing up, changing folders or restoring another file.
        </BackupNotice>
      ) : null}

      {localActionProblem ? <BackupNotice title={localActionProblem.title}>{localActionProblem.detail}</BackupNotice>
        : backingUp || automaticRunning ? (
          <BackupNotice title="A backup is running" tone="info" log={false}>
            Keep CARE Desktop open. The new copy will appear after the backup finishes.
          </BackupNotice>
        ) : backupComplete && run?.state === "ok" && !storage?.stale ? (
          <BackupNotice title="The backup finished" tone="success" log={false}>
            Refresh the list if the new copy hasn't appeared yet.
          </BackupNotice>
        ) : null}

      {run?.state === "failed" && !backingUp && !automaticRunning ? (
        <BackupNotice title={run.reason === "disk_full" ? "The last backup didn't finish — there wasn't enough space" : "The last backup didn't finish"}
          actions={<>
            {run.reason !== "database_unavailable" ? <Button disabled={disabled} onClick={() => void changeFolder()}>Choose another folder</Button> : null}
            <Button disabled={disabled} onClick={() => void backUpNow()}>Try again now</Button>
          </>}>
          {run.reason === "disk_full"
            ? `${run.need_bytes > 0 ? `It needed about ${diskSize(run.need_bytes)} and ${diskSize(run.free_bytes)} was free. ` : ""}Free up space or choose another folder.`
            : backupFailureDetail(run.reason)}
          {lastRun ? ` Last attempt: ${lastRun}.` : ""}
        </BackupNotice>
      ) : storage?.stale && !backingUp && !automaticRunning ? (
        <BackupNotice title={lastBackup ? `No backup since ${lastBackup}` : "No recent backup"} tone="warning"
          actions={<>
            <Button variant="primary" disabled={disabled} onClick={() => void backUpNow()}><Archive aria-hidden="true" />Back up now</Button>
            <Button disabled={disabled} onClick={() => void changeFolder()}>Choose another folder</Button>
          </>} log={false}>
          Check that the backup folder is available, then back up now. Automatic backups continue while CARE is running.
        </BackupNotice>
      ) : lastRun && run?.state === "ok" ? (
        <p className="care-backups-last-run"><Clock3 aria-hidden="true" />Last completed backup: {lastRun}</p>
      ) : lastBackup ? (
        <p className="care-backups-last-run"><Clock3 aria-hidden="true" />Latest backup: {lastBackup}</p>
      ) : null}

      {backupsError ? (
        <BackupNotice title="The backup list couldn't be read">
          Check that the backup folder is available, then try Refresh. Your existing backups have not been changed.
        </BackupNotice>
      ) : backups.length === 0 ? (
        <div className="care-backups-card care-backups-empty">
          <span className="care-backups-icon"><Archive aria-hidden="true" /></span>
          <h2>{measured ? "No backups yet" : "No backups listed"}</h2>
          <p>{measured
            ? "The first backup runs automatically after the clinic starts. Or choose Back up now."
            : "Check that the backup folder is available, or choose another folder."}</p>
        </div>
      ) : (
        <section className="care-backups-list" aria-label="Saved backups">
          {groups.map(([day, entries]) => (
            <div key={day}>
              <h2>{day}</h2>
              <ul>
                {entries.map(({ backup, date }) => (
                  <li key={backup.db_dump}>
                    <span className="care-backups-icon care-backups-icon--small"><Database aria-hidden="true" /></span>
                    <div className="care-backups-grow">
                      <h3 title={backup.db_dump}>{backupWhen(date, now)}</h3>
                      <p>{backup.size_bytes > 0 ? <>
                        {diskSize(backup.size_bytes)} database · {backup.files_archive ? "database and files" : "database only"}
                        {backup.encrypted ? " · encrypted" : " · not encrypted"}
                      </> : "Empty database file · not ready to restore"}</p>
                    </div>
                    <span className={`care-backups-kind${backup.manual ? " care-backups-kind--manual" : ""}`}>
                      {backup.manual ? "Manual" : "Automatic"}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
      )}

      <RestoreBackupFile disabled={!!working || automaticRunning || folderLoading} />
    </div>
    </>
  );
}

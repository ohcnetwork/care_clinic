import { Database, FolderOpen, HardDrive } from "lucide-react";
import { Fragment, useEffect, useState } from "react";

import { InfoButton } from "@/components/field";
import { RecoveryFilePicker } from "@/components/recovery-file-picker";
import { LEVEL_BADGE, StorageMeter } from "@/components/storage-meter";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { bridge } from "@/lib/bridge";
import { diskSize, errorText, firstLine, megabytes } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";
import type { ImportedBackup } from "@/types";

const RESTORE_INFO =
  "Restoring replaces today's data with the chosen copy. CARE pauses for a moment while it restores, and you confirm before anything changes.";

export function BackupsTab() {
  const { backups, backupsError, busy, restorePending, runAction, reloadBackups, restore } = useCare();
  const [showInfo, setShowInfo] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [recoveryFile, setRecoveryFile] = useState("");
  const [adminPassword, setAdminPassword] = useState("");

  return (
    <div className="flex flex-col gap-3">
      <BackupFolderRow />
      <Alert>
        Restoring encrypted backups requires the separate backup recovery file saved during
        setup. Keep it secure and separate from backups. Lost every copy? Old backups
        cannot be unlocked. Forgot the Desktop password? Open Advanced to use a recovery code.
      </Alert>

      <div className="flex items-center gap-[11px]">
        <Button
          variant="primary"
          disabled={busy || restorePending}
          onClick={() => void runAction("backup-now")}
        >
          Back up now
        </Button>
        <span className="text-[13px] text-muted-foreground">Automatic, daily</span>
        <InfoButton
          title="Restoring replaces current data"
          pressed={showInfo}
          onClick={() => setShowInfo((v) => !v)}
        />
        <span className="flex-1" />
        <span className="text-[13px] text-muted-foreground">
          {!backupsError && backups.length ? `${backups.length} kept` : ""}
        </span>
        <Button
          disabled={busy}
          onClick={() => {
            setConfirming(null);
            void reloadBackups();
          }}
        >
          Refresh
        </Button>
      </div>

      {showInfo ? <Alert>{RESTORE_INFO}</Alert> : null}

      <div className="overflow-hidden rounded-xl border border-line bg-card shadow-card">
        {backupsError ? (
          <Alert variant="danger">Could not read the backup folder: {backupsError}</Alert>
        ) : backups.length === 0 ? (
          <div className="p-5 text-center text-[13px] text-faint">
            No backups yet. Click <b>Back up now</b> or wait for the daily backup.
          </div>
        ) : (
          backups.map((backup, i) => {
            const meta = [
              megabytes(backup.size_bytes),
              backup.files_archive ? "database and files" : "database only",
              backup.encrypted ? "encrypted" : "",
            ]
              .filter(Boolean)
              .join("  ·  ");
            return (
              <Fragment key={backup.db_dump}>
                <div
                  className={cn(
                    "flex items-center gap-[13px] px-4 py-3.5",
                    i > 0 && "border-t border-hair",
                  )}
                >
                  <span className="flex size-[30px] flex-none items-center justify-center rounded-sm bg-hair text-muted-foreground">
                    <Database className="size-[15px]" strokeWidth={2} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="font-mono text-[13.5px] font-semibold text-ink">
                      {backup.label}
                    </div>
                    <div className="mt-0.5 text-[12.5px] text-muted-foreground">{meta}</div>
                  </div>
                  <Badge variant={backup.manual ? "plainOk" : "plain"} size="sm">
                    {backup.manual ? "Manual" : "Automatic"}
                  </Badge>
                  <Button disabled={busy || restorePending} onClick={() => {
                      setRecoveryFile("");
                      setAdminPassword("");
                      setConfirming(backup.db_dump);
                    }}>
                    Restore
                  </Button>
                </div>
                {confirming === backup.db_dump ? (
                  <div className="flex flex-col gap-3 border-t border-danger-bg bg-danger-tint px-4 py-[13px] text-[12.5px] text-danger-ink">
                    <label className="flex items-center gap-3">
                      <span className="flex-none">Desktop admin password</span>
                      <input
                        type="password"
                        autoComplete="current-password"
                        value={adminPassword}
                        onChange={(e) => setAdminPassword(e.target.value)}
                        className="min-w-0 flex-1 rounded-sm border border-danger-bg bg-white px-2 py-1 font-mono text-[12.5px] text-ink"
                      />
                    </label>
                    {backup.encrypted ? (
                      <RecoveryFilePicker value={recoveryFile} onChange={setRecoveryFile} disabled={busy} />
                    ) : null}
                    <div className="flex items-center gap-3">
                      <span className="flex-1">
                        Replace current data with this copy? This cannot be undone.
                      </span>
                      <Button onClick={() => setConfirming(null)}>Cancel</Button>
                      <Button
                        variant="destructive"
                        disabled={busy || !adminPassword || (backup.encrypted && !recoveryFile)}
                        onClick={() => {
                          setConfirming(null);
                          void restore(backup, recoveryFile, adminPassword);
                          setAdminPassword("");
                          setRecoveryFile("");
                        }}
                      >
                        Yes, restore
                      </Button>
                    </div>
                  </div>
                ) : null}
              </Fragment>
            );
          })
        )}
      </div>

      <ImportCard />
    </div>
  );
}

/** Where backups are written, and how to point them somewhere else (a USB drive). */
function BackupFolderRow() {
  const { busy, restorePending, log, storage, recheckStorage } = useCare();
  const [dir, setDir] = useState("");
  const [problem, setProblem] = useState("");
  const [working, setWorking] = useState(false);

  useEffect(() => {
    void bridge.GetBackupDir().then(setDir, () => setDir(""));
  }, []);

  const change = async () => {
    const chosen = await bridge.ChooseFolder("Choose where backups should go");
    if (!chosen) return;
    setWorking(true);
    setProblem("");
    try {
      setDir(await bridge.SetBackupDir(chosen));
      toast("Backups will go to the new folder");
      void recheckStorage();
    } catch (e) {
      setProblem(firstLine(errorText(e)));
      log(`backup folder: ${errorText(e)}`);
    } finally {
      setWorking(false);
    }
  };

  const space = storage?.backup;
  const run = storage?.last_run;
  const failed = run?.state === "failed";
  const tone = space?.level ?? "unknown";
  const canChange = !(busy || working || restorePending);

  return (
    <>
      {failed ? (
        <Alert variant="danger" className="items-start">
          <div className="min-w-0 flex-1">
            <div className="font-semibold">
              {run.reason === "disk_full"
                ? "The last automatic backup failed because the backup drive is full."
                : "The last automatic backup failed."}
            </div>
            <div className="mt-0.5">
              {run.reason === "disk_full"
                ? `It needed about ${diskSize(run.need_bytes)} and ${diskSize(run.free_bytes)} was free. Free up space on that drive, or choose another folder. The next backup runs automatically.`
                : `${run.message ? `Cause: ${run.message}. ` : ""}Try Back up now; if it fails again, check the log under Advanced.`}
            </div>
          </div>
          {run.reason === "disk_full" ? (
            <Button disabled={!canChange} onClick={() => void change()}>
              Choose another folder
            </Button>
          ) : null}
        </Alert>
      ) : storage?.stale ? (
        <Alert variant="danger">
          No backup in over a day. Check the backup drive is plugged in, then press Back up now.
        </Alert>
      ) : null}

      <div className="rounded-xl border border-line bg-card px-4 py-3.5 shadow-card">
        <div className="flex items-center gap-3">
          <span
            className={cn(
              "flex size-[30px] flex-none items-center justify-center rounded-sm",
              tone === "critical"
                ? "bg-danger-bg text-danger-ink"
                : tone === "low"
                  ? "bg-warn-bg text-warn-ink"
                  : "bg-brand-bg text-brand-ink",
            )}
          >
            <HardDrive className="size-4" strokeWidth={2} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-xs font-semibold tracking-[0.04em] text-muted-foreground uppercase">
              Backups are saved to
            </div>
            <div className="truncate font-mono text-[13.5px] font-semibold text-ink">
              {dir || "…"}
            </div>
          </div>
          {space && space.total > 0 ? (
            <Badge variant={LEVEL_BADGE[tone].variant} size="sm">
              {LEVEL_BADGE[tone].label}
            </Badge>
          ) : null}
          <Button disabled={!canChange} onClick={() => void change()}>
            {working ? "Switching…" : "Change"}
          </Button>
        </div>
        {space && space.total > 0 ? (
          <div className="mt-3 pl-[42px]">
            <StorageMeter free={space.free} total={space.total} level={tone} />
            <div className="mt-1.5 flex items-baseline justify-between gap-3 text-[12.5px]">
              <span
                className={cn(
                  tone === "critical"
                    ? "text-danger-ink"
                    : tone === "low"
                      ? "text-warn-ink"
                      : "text-muted-foreground",
                )}
              >
                {space.message}
              </span>
              <span className="flex-none font-mono text-[12px] text-faint">
                {diskSize(space.free)} free of {diskSize(space.total)}
              </span>
            </div>
            {space.shares_docker_drive ? (
              <div className="mt-1 text-[12.5px] text-muted-foreground">
                Same drive as the clinic&apos;s data. A USB drive keeps the backups safe if this
                computer&apos;s drive fails.
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
      {problem ? (
        <div className="-mt-1 text-[12.5px] leading-[1.5] text-danger-ink">{problem}</div>
      ) : null}
    </>
  );
}

/** Restore a backup that came from another computer, chosen with the file picker. */
function ImportCard() {
  const { busy, restorePending, restoreFile } = useCare();
  const [found, setFound] = useState<ImportedBackup | null>(null);
  const [problem, setProblem] = useState("");
  const [recoveryFile, setRecoveryFile] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [adminPassword, setAdminPassword] = useState("");

  const pick = async () => {
    const path = await bridge.ChooseBackupFile();
    if (!path) return;
    setProblem("");
    setConfirming(false);
    setRecoveryFile("");
    setAdminPassword("");
    try {
      setFound(await bridge.InspectBackupFile(path));
    } catch (e) {
      setFound(null);
      setProblem(firstLine(errorText(e)));
    }
  };

  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line bg-card p-4 shadow-card">
      <div className="flex items-center gap-3">
        <span className="flex size-[30px] flex-none items-center justify-center rounded-sm bg-hair text-muted-foreground">
          <FolderOpen className="size-4" strokeWidth={2} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[13.5px] font-semibold text-ink">
            Restore from a file
          </div>
          <div className="mt-px text-[12.5px] text-muted-foreground">
            For a backup brought from another computer, on a USB drive.
          </div>
        </div>
        <Button disabled={busy || restorePending} onClick={() => void pick()}>
          Choose file
        </Button>
      </div>

      {problem ? (
        <div className="text-[12.5px] leading-[1.5] text-danger-ink">{problem}</div>
      ) : null}

      {found ? (
        <div className="flex flex-col gap-2.5 rounded-lg border border-line bg-background px-3.5 py-3">
          <div className="font-mono text-[13px] font-semibold text-ink">{found.db_dump}</div>
          <div className="text-[12.5px] text-muted-foreground">
            {found.files_archive
              ? "Database and uploaded files."
              : "Database only — this backup has no uploaded files with it."}
          </div>
          {found.encrypted ? (
            <RecoveryFilePicker value={recoveryFile} onChange={setRecoveryFile} disabled={busy} />
          ) : null}
          <label className="flex items-center gap-3 text-[12.5px]">
            <span className="flex-none text-muted-foreground">Desktop admin password</span>
            <input
              type="password"
              autoComplete="current-password"
              value={adminPassword}
              onChange={(e) => setAdminPassword(e.target.value)}
              className="min-w-0 flex-1 rounded-sm border border-line bg-white px-2 py-1 font-mono text-[12.5px] text-ink"
            />
          </label>

          {confirming ? (
            <div className="flex items-center gap-3 rounded-lg border border-danger-bg bg-danger-tint px-3.5 py-[11px] text-[12.5px] text-danger-ink">
              <span className="flex-1">
                Replace this clinic&apos;s data with that file? This cannot be undone.
              </span>
              <Button onClick={() => setConfirming(false)}>Cancel</Button>
              <Button
                variant="destructive"
                disabled={busy || !adminPassword || (found.encrypted && !recoveryFile)}
                onClick={() => {
                  setConfirming(false);
                  void restoreFile(found.path, recoveryFile, adminPassword);
                  setAdminPassword("");
                  setRecoveryFile("");
                }}
              >
                Yes, restore
              </Button>
            </div>
          ) : (
            <Button
              variant="destructive"
              className="self-start"
              disabled={busy || !adminPassword || (found.encrypted && !recoveryFile)}
              onClick={() => setConfirming(true)}
            >
              Restore this file
            </Button>
          )}
        </div>
      ) : null}
    </div>
  );
}

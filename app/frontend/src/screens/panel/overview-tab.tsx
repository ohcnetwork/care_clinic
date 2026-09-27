import { Database, HardDrive, Server } from "lucide-react";
import { useState } from "react";

import { Spinner } from "@/components/spinner";
import { StorageRow } from "@/components/storage-meter";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import { Switch } from "@/components/ui/switch";
import { bridge } from "@/lib/bridge";
import { shortDate } from "@/lib/format";
import { cn } from "@/lib/utils";
import { RESTORE_PENDING_NOTICE, useCare, type SystemState } from "@/state/care-store";

const SYSTEM: Record<
  Exclude<SystemState, "unknown">,
  { label: string; sub: string; glyph: string; tone: string }
> = {
  running: {
    label: "Running",
    sub: "Live and reachable on the clinic WiFi.",
    glyph: "●",
    tone: "bg-brand-bg text-brand-ink",
  },
  stopped: {
    label: "Stopped",
    sub: "The clinic system is not running.",
    glyph: "■",
    tone: "bg-hair text-muted-foreground",
  },
  partial: {
    label: "Starting…",
    sub: "Some services are still coming up.",
    glyph: "■",
    tone: "bg-warn-bg text-warn-ink",
  },
};

export function OverviewTab() {
  const {
    system,
    systemDetail,
    restorePending,
    busy,
    busyLabel,
    autostart,
    setAutostart,
    runAction,
    backups,
    mdnsName,
    setTab,
    storage,
  } = useCare();

  const view = SYSTEM[system === "unknown" ? "stopped" : system];
  const running = system === "running";
  const partial = system === "partial";
  const stopped = !running && !partial;
  const unreachable = system === "unknown" && systemDetail !== "";
  const latest = backups[0];
  const lastRun = storage?.last_run;
  const backupFailed = lastRun?.state === "failed";
  const backupStale = storage?.stale ?? false;
  const backupTitle = backupFailed
    ? "Last backup failed"
    : backupStale
      ? "Backups have stopped"
      : latest
        ? "Up to date"
        : "No backups yet";
  const backupSub = backupFailed
    ? lastRun?.reason === "disk_full"
      ? "The backup drive is full."
      : lastRun?.message
        ? `Cause: ${lastRun.message}.`
        : "See the Backups tab."
    : latest
      ? `Last ${shortDate(latest.label)}${latest.encrypted ? ", encrypted" : ""}`
      : "Run one now or wait for the daily backup";

  const copyAddress = () =>
    void navigator.clipboard.writeText(mdnsName).then(
      () => toast("Address copied"),
      () => toast(mdnsName),
    );

  return (
    <div className="flex flex-col gap-3">
      {restorePending ? (
        <Alert variant="danger">
          {RESTORE_PENDING_NOTICE} Recovery data is kept until CARE starts successfully.
        </Alert>
      ) : null}
      <Card className="flex items-center gap-4 p-5">
        <span
          className={cn(
            "flex size-10 flex-none items-center justify-center rounded-full text-base font-bold",
            busy ? "bg-warn-bg text-warn-ink" : unreachable ? "bg-danger-bg text-danger-ink" : view.tone,
          )}
        >
          {busy ? <Spinner /> : unreachable ? "!" : view.glyph}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[17px] font-bold text-ink">
            {busy
              ? `${busyLabel}…`
              : system === "unknown"
                ? unreachable
                  ? "Can't check the clinic"
                  : "checking…"
                : view.label}
          </div>
          <div className="mt-0.5 text-[13.5px] text-muted-foreground">
            {busy ? "Please wait a moment." : system === "unknown" ? systemDetail : view.sub}
          </div>
        </div>
        <div className="flex flex-none items-center gap-2">
          <Button
            variant={stopped && !busy ? "primary" : "default"}
            disabled={busy || running || partial}
            onClick={() => void runAction("start")}
          >
            Start
          </Button>
          <Button disabled={busy || stopped} onClick={() => void runAction("stop")}>
            Stop
          </Button>
          <Button
            disabled={busy || stopped || restorePending}
            onClick={() => void runAction("restart")}
          >
            Restart
          </Button>
          <label className="ml-1.5 flex cursor-pointer items-center gap-[9px] text-[13px] font-semibold text-ink2 select-none">
            <Switch
              checked={autostart}
              onCheckedChange={(on) => void setAutostart(on)}
              aria-label="Start at login"
            />
            <span>Start at login</span>
          </label>
        </div>
      </Card>

      <div className="grid grid-cols-[1.15fr_1fr] gap-3">
        <div className="flex flex-col rounded-xl bg-brand-deep p-5 text-brand-bg">
          <div className="text-[11.5px] font-bold tracking-[0.06em] text-brand-line uppercase">
            Clinic address
          </div>
          <div className="mt-1.5 font-mono text-[26px] font-bold text-white">{mdnsName}</div>
          <div className="mt-1.5 text-[13px] text-brand-pale">
            Open on any device on the clinic WiFi.
          </div>
          <div className="min-h-3.5 flex-1" />
          <div className="flex flex-wrap gap-2">
            <Button variant="white" onClick={() => void bridge.OpenURL(`https://${mdnsName}/`)}>
              Open
            </Button>
            <Button variant="glass" onClick={copyAddress}>
              Copy
            </Button>
            <Button
              variant="glass"
              onClick={() => void bridge.OpenURL(`https://${mdnsName}/seed-data`)}
            >
              Load data into clinic
            </Button>
            <Button
              variant="glass"
              onClick={() => void bridge.OpenURL("https://docs.ohc.network/")}
            >
              Open docs
            </Button>
            <Button
              variant="glass"
              disabled={busy || !running}
              onClick={() => void bridge.OpenURL(`http://${mdnsName}/setup`)}
            >
              Connect phone or tablet
            </Button>
          </div>
        </div>

        <Card className="flex flex-col p-5">
          <div className="text-[11.5px] font-bold tracking-[0.06em] text-muted-foreground uppercase">
            Backups
          </div>
          <div
            className={cn(
              "mt-[7px] text-base font-bold",
              backupFailed || backupStale ? "text-danger-ink" : "text-ink",
            )}
          >
            {backupTitle}
          </div>
          <div className="mt-1 text-[13px] text-muted-foreground">{backupSub}</div>
          <div className="min-h-3.5 flex-1" />
          <div className="flex gap-2">
            <Button onClick={() => setTab("backups")}>View backups</Button>
            <Button
              variant="soft"
              disabled={busy || restorePending}
              onClick={() => void runAction("backup-now")}
            >
              Back up now
            </Button>
          </div>
        </Card>
      </div>

      <StorageCard />
    </div>
  );
}

const SHARED_DRIVE_NOTE =
  "Same drive as the clinic's data. A USB drive keeps the backups safe if this computer's drive fails.";

function StorageCard() {
  const { storage, recheckStorage, setTab, runAction, busy, restorePending } = useCare();
  const [checking, setChecking] = useState(false);

  const recheck = async () => {
    setChecking(true);
    try {
      await recheckStorage();
    } finally {
      setChecking(false);
    }
  };

  const drives = storage?.drives ?? [];
  const backup = storage?.backup;
  const checkedAt = storage?.checked_at
    ? new Date(storage.checked_at * 1000).toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
      })
    : "";

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-3 px-5 pt-4 pb-3">
        <div className="min-w-0 flex-1">
          <div className="text-[11.5px] font-bold tracking-[0.06em] text-muted-foreground uppercase">
            Storage
          </div>
          <div className="mt-0.5 text-[12.5px] text-faint">
            {checkedAt ? `Checked at ${checkedAt} · every 5 minutes` : "Checking…"}
          </div>
        </div>
        <Button disabled={checking} onClick={() => void recheck()}>
          {checking ? <Spinner className="size-3.5" /> : null}
          {checking ? "Checking…" : "Check now"}
        </Button>
      </div>
      {drives.map((drive) => (
        <StorageRow
          key={drive.id}
          className="border-t border-hair"
          icon={drive.id === "vm" ? Server : HardDrive}
          label={drive.label}
          path={drive.id === "vm" ? undefined : drive.path}
          free={drive.free}
          total={drive.total}
          level={drive.level}
          message={drive.message}
          note={
            drive.id === "vm"
              ? "The virtual disk Rancher Desktop keeps the clinic's database and uploads in."
              : undefined
          }
          action={
            drive.cleanable ? (
              <Button
                size="sm"
                disabled={busy || restorePending}
                title="Removes old CARE images and Docker's build cache. Clinic data is not touched."
                onClick={() => void runAction("free-space")}
              >
                Free up space
              </Button>
            ) : undefined
          }
        />
      ))}
      {backup && backup.dir ? (
        <StorageRow
          className="border-t border-hair"
          icon={Database}
          label="Backups"
          path={backup.dir}
          free={backup.free}
          total={backup.total}
          level={backup.level}
          message={backup.message}
          note={backup.shares_docker_drive ? SHARED_DRIVE_NOTE : undefined}
          action={
            backup.level !== "ok" ? (
              <Button size="sm" onClick={() => setTab("backups")}>
                Change folder
              </Button>
            ) : undefined
          }
        />
      ) : null}
    </Card>
  );
}

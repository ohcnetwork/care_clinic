import { Archive, Copy, ExternalLink, Play, RefreshCw, RotateCcw, Smartphone, Square, TriangleAlert } from "lucide-react";
import { useState } from "react";

import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { Switch } from "@/components/ui/switch";
import { bridge } from "@/lib/bridge";
import { diskSize } from "@/lib/format";
import { RESTORE_PENDING_NOTICE, useCare } from "@/state/care-store";
import { PhoneDialog } from "./phone-dialog";
import { RequirementsCard, usePanelRequirements } from "./panel-requirements";
import { backupFailureDetail, panelStatus } from "./panel-status";
import { PanelBadge, PanelLogButton, PanelNotice, PanelPageHeader, usePanelTask } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

function backupTime(label: string) {
  const compact = /^(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})/.exec(label);
  const match = compact
    ? ["", `${compact[1]}-${compact[2]}-${compact[3]}`, `${compact[4]}:${compact[5]}`]
    : /^(\d{4}-\d{2}-\d{2})\s+(\d{2}:\d{2})/.exec(label);
  if (!match) return "Last backup saved";
  const date = new Date(`${match[1]}T${match[2]}:00`);
  if (Number.isNaN(date.getTime())) return "Last backup saved";
  return date.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function OverviewTab({ onDiagnose }: { onDiagnose: () => void }) {
  const care = useCare();
  const requirements = usePanelRequirements();
  const updateLock = usePanelUpdateLock();
  const [phoneOpen, setPhoneOpen] = useState(false);
  const action = usePanelTask();
  const address = usePanelTask();
  const autostart = usePanelTask();
  const status = panelStatus(care);
  const startupError = care.autostartError;
  const locked = care.busy || requirements.working || action.working;
  const mutationLocked = locked || updateLock.active;
  const run = (name: string) => {
    if (locked || updateLock.isActive()) return;
    void action.run(() => care.runAction(name),
      "That didn't start. Try again, or open the log file for support.");
  };
  const canStart = care.system === "stopped" && !care.trouble;
  const copy = async () => {
    if (await address.run(() => navigator.clipboard.writeText(care.mdnsName),
      "Couldn't copy the address. Select the address above and copy it instead.")) toast("Address copied");
  };

  return <div className="panel-page" aria-label="Overview">
    <PanelPageHeader title="Overview" subtitle="Your clinic at a glance." />
    {care.restorePending ? <PanelNotice title="An earlier restore needs to finish">
      {RESTORE_PENDING_NOTICE} Recovery data is kept until CARE starts successfully.
    </PanelNotice> : null}
    {care.pluginRecoveryPending ? <PanelNotice title="Plugin recovery needs to finish">
      The previous clinic configuration has not yet been confirmed healthy.
      <Button disabled={mutationLocked} onClick={() => run("start")}>Recover clinic</Button>
      <PanelLogButton />
    </PanelNotice> : null}
    <section className={`panel-card panel-hero ${status.tone === "danger" ? "panel-hero-danger" : ""}`} aria-label="Clinic status">
      <div className={`panel-orb panel-tone-${status.tone}`} aria-hidden="true">
        {status.working ? <Spinner /> : status.tone === "danger" ? <TriangleAlert />
          : <span className={`panel-orb-dot ${care.system === "stopped" ? "panel-orb-stop" : ""}`} />}
      </div>
      <div className="panel-hero-copy" role="status" aria-live="polite">
        <h2 className={`panel-state-${status.tone}`}>{status.label}</h2>
        <p>{status.detail}</p>
        {care.busy && status.available ? <p>{care.busyLabel}…</p> : null}
      </div>
      <div className="panel-hero-actions">
        {care.system === "unknown" ? <>
          <Button disabled={locked} onClick={() => void action.run(care.refresh,
            "Couldn't check the clinic. Try again, or open the log file for support.")}>
            <RefreshCw aria-hidden="true" />Check again
          </Button>
          <Button disabled={requirements.working} onClick={onDiagnose}>See what&apos;s wrong</Button>
        </> : care.trouble ? <>
          <Button variant="primary" disabled={mutationLocked} onClick={() => run(care.restorePending || care.pluginRecoveryPending ? "start" : "restart")}>
            <RotateCcw aria-hidden="true" />{care.restorePending || care.pluginRecoveryPending ? "Start clinic" : "Restart clinic"}
          </Button>
          <Button disabled={requirements.working} onClick={onDiagnose}>See what&apos;s wrong</Button>
        </> : <>
          {canStart ? <Button variant="primary" disabled={mutationLocked} onClick={() => run("start")}>
            <Play aria-hidden="true" />Start clinic
          </Button> : <>
            <Button disabled={mutationLocked} onClick={() => run("stop")}><Square aria-hidden="true" />Stop</Button>
            <Button disabled={mutationLocked || care.restorePending || care.pluginRecoveryPending} onClick={() => run("restart")}><RotateCcw aria-hidden="true" />Restart</Button>
          </>}
          <label className="panel-autostart">
            <Switch checked={care.autostart} disabled={mutationLocked || autostart.working || !care.autostartReady || care.autostartSaving}
              aria-label="Start when this computer starts"
              onCheckedChange={(on) => {
                if (locked || updateLock.isActive() || autostart.working || !care.autostartReady || care.autostartSaving) return;
                void autostart.run(() => care.setAutostart(on),
                  "Couldn't save the start preference. Try again, or open the log file for support.");
              }} />
            <span>Start when this computer starts</span>
          </label>
        </>}
      </div>
      {action.error || autostart.error || startupError ? <div className="panel-hero-error">
        <p className="panel-inline-error" role="alert">{action.error || autostart.error || startupError}</p>
        <div className="panel-actions">
          {startupError ? <Button disabled={locked || autostart.working} onClick={() => void autostart.run(care.recheckAutostart,
            "Couldn't check the start preference. Try again, or open the log file for support.")}>
            Check start preference
          </Button> : null}
          <PanelLogButton />
        </div>
      </div> : null}
    </section>
    <RequirementsCard />
    <div className="panel-overview-grid">
      <section className="panel-address-card" aria-label="Clinic address">
        <h2 className="panel-eyebrow">Clinic address</h2>
        <div className="panel-address">{care.mdnsName}</div>
        <p>{status.available ? "Open on any computer, phone or tablet on the clinic Wi-Fi."
          : care.system === "stopped" && !care.trouble ? "Works again as soon as the clinic is started."
            : "Not reachable right now."}</p>
        <div className="panel-card-spacer" />
        <div className="panel-actions">
          <Button variant="white" disabled={!status.available || locked || address.working}
            onClick={() => void address.run(() => bridge.OpenURL(`https://${care.mdnsName}/`),
              "Couldn't open CARE. Try again, or type the clinic address into your browser.")}>
            <ExternalLink aria-hidden="true" />Open CARE
          </Button>
          <Button variant="glass" disabled={locked || address.working} onClick={() => void copy()}>
            <Copy aria-hidden="true" />Copy
          </Button>
          <Button variant="glass" disabled={!status.available || locked} onClick={() => setPhoneOpen(true)}>
            <Smartphone aria-hidden="true" />Connect a phone or tablet
          </Button>
        </div>
        {address.error ? <p className="panel-inline-error" role="alert">{address.error}</p> : null}
      </section>
      <BackupSummary locked={locked} available={status.available} onBackup={() => run("backup-now")} />
    </div>
    {phoneOpen ? <PhoneDialog onClose={() => setPhoneOpen(false)} /> : null}
  </div>;
}

function BackupSummary({ locked, available, onBackup }: { locked: boolean; available: boolean; onBackup: () => void }) {
  const { backups, backupsError, storage, storageError, busy, busyLabel, restorePending, setTab } = useCare();
  const updateLock = usePanelUpdateLock();
  const latest = backups[0];
  const failed = !storageError && storage?.last_run.state === "failed";
  const stale = !storageError && storage?.stale;
  const automaticRunning = !storageError && storage?.last_run.state === "running" && storage.last_run.at > 0 &&
    Date.now() - storage.last_run.at * 1000 < 6 * 60 * 60 * 1000;
  const working = (busy && busyLabel === "Backing up") || automaticRunning;
  const needsAttention = failed || stale;
  const badge = backupsError ? "Couldn't check" : working ? "Working" : failed ? "Last backup failed"
    : stale ? "Needs attention" : latest ? storage?.newest_backup_at && !storageError ? "Up to date" : "Saved" : "No backups yet";
  const title = backupsError ? "Couldn't read the backups" : working ? "A backup is running"
    : failed ? "The last backup didn't finish" : stale ? "No recent backup"
      : latest ? backupTime(latest.label) : "Your first backup is still to come";
  const details = [
    latest?.size_bytes > 0 ? diskSize(latest.size_bytes) : "",
    latest?.encrypted ? "encrypted" : "",
  ].filter(Boolean).join(" · ");
  return <section className="panel-card panel-backup-summary" aria-label="Backup summary">
    <div className="panel-row panel-between">
      <h2 className="panel-eyebrow">Backups</h2>
      <PanelBadge tone={backupsError || needsAttention ? "danger" : latest ? "ok" : "neutral"} working={working}>{badge}</PanelBadge>
    </div>
    <h2>{title}</h2>
    <p className="panel-small">{backupsError ? "Check the backup folder, then try again from Backups."
      : failed ? backupFailureDetail(storage.last_run.reason)
        : stale ? "Leave CARE running and check that the backup folder is available."
          : latest ? details : working ? "Keep CARE running while it finishes."
            : "Backups run automatically while CARE is running. You can also start one now."}</p>
    <div className="panel-card-spacer" />
    <div className="panel-actions">
      <Button disabled={locked} onClick={() => setTab("backups")}>{needsAttention ? "Fix this" : "View backups"}</Button>
      <Button variant="soft" disabled={locked || updateLock.active || !available || restorePending || working}
        onClick={onBackup}><Archive aria-hidden="true" />Back up now</Button>
    </div>
  </section>;
}

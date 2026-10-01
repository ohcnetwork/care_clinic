import { Download, Monitor, RefreshCw, ScrollText, Stethoscope } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import type { AppUpdateController } from "@/hooks/use-app-update";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText, megabytes } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { CareCheck, ChannelStatus } from "@/types";
import { usePanelRequirements } from "./panel-requirements";
import { PanelBadge, PanelLogButton, PanelNotice, PanelPageHeader, usePanelTask } from "./panel-ui";
import { usePanelUpdateLock } from "./panel-update-lock";

function checkedTime(timestamp: number) {
  return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function UpdatePanel({ appUpdate }: { appUpdate: AppUpdateController }) {
  return <div className="panel-page" aria-label="Updates">
    <PanelPageHeader title="Updates" subtitle="Keep CARE and CARE Desktop up to date." />
    <CareSoftwareCard />
    <DesktopApplicationCard controller={appUpdate} />
    <div className="panel-update-foot">
      <p className="panel-small panel-grow">Updates need internet. Your clinic records are kept.</p>
      <PanelLogButton />
    </div>
  </div>;
}

function CareSoftwareCard() {
  const { log, busy, busyLabel, restorePending, careUpdate, applyCareUpdate, dismissCareUpdate } = useCare();
  const requirements = usePanelRequirements();
  const updateLock = usePanelUpdateLock();
  const [status, setStatus] = useState<ChannelStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);
  const [checkedAt, setCheckedAt] = useState(0);
  const [upToDate, setUpToDate] = useState(false);
  const [error, setError] = useState("");
  const request = useRef(0);
  const checkPending = useRef(false);
  const mounted = useRef(false);
  const action = usePanelTask();

  const reload = useCallback(async () => {
    const id = ++request.current;
    try {
      const next = await bridge.CareUpdateStatus();
      if (mounted.current && id === request.current) {
        setStatus(next);
        setLoading(false);
      }
    } catch (cause) {
      log(`CARE update status: ${errorText(cause)}`);
      if (mounted.current && id === request.current) {
        setError("Couldn't read the CARE update status. Try checking again, or open the log file for support.");
        setLoading(false);
      }
    }
  }, [log]);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; request.current++; };
  }, []);
  useEffect(() => { if (!busy) void reload(); }, [reload, careUpdate, busy]);
  useEffect(() => onCareEvent("care-check", (check: CareCheck) => {
    checkPending.current = check.running;
    setChecking(check.running);
    if (check.running) {
      setError("");
      setUpToDate(false);
      return;
    }
    if (check.error) {
      log(`CARE update check: ${check.error}`);
      setError("Updates couldn't be checked. Check the internet connection and try again. If it keeps failing, share the log file with support.");
      setUpToDate(false);
    } else {
      setError("");
      setCheckedAt(Date.now());
      setUpToDate(!check.found);
    }
    void reload();
  }), [reload, log]);

  const check = async () => {
    if (checkPending.current || busy || updateLock.isActive() || requirements.working || restorePending) return;
    checkPending.current = true;
    setChecking(true);
    setUpToDate(false);
    setError("");
    try {
      await bridge.CheckCareUpdate();
      // The native method accepts the check; care-check reports its result.
    } catch (cause) {
      log(`CARE update check: ${errorText(cause)}`);
      checkPending.current = false;
      setChecking(false);
      setError("Couldn't start the update check. Try again, or open the log file for support.");
    }
  };
  const later = async () => {
    if (locked || updateLock.isActive()) return;
    await action.run(dismissCareUpdate, "Couldn't save this choice. Try again, or open the log file for support.");
  };
  const apply = () => {
    if (locked || updateLock.isActive()) return;
    void action.run(applyCareUpdate, "Couldn't start the CARE update. Try again, or open the log file for support.");
  };
  const pending = !!(careUpdate?.backend || careUpdate?.frontend || status?.pending_backend || status?.pending_frontend);
  const backendUpdate = !!(careUpdate?.backend || status?.pending_backend);
  const applying = busy && busyLabel === "Updating CARE";
  const locked = busy || updateLock.active || requirements.working || restorePending || action.working;

  return <section className="panel-card panel-card-pad panel-update-card" aria-label="CARE software">
    <div className="panel-update-head">
      <span className="panel-tile"><Stethoscope aria-hidden="true" /></span>
      <div className="panel-grow">
        <h2 className="panel-title">CARE software</h2>
        <p className="panel-card-sub">{checking ? "Checking for updates…"
          : checkedAt ? `Checked at ${checkedTime(checkedAt)} · Updates are prepared in the background.`
            : "Updates are prepared in the background."}</p>
      </div>
      <PanelBadge tone={error ? "warning" : pending || upToDate ? "ok" : "neutral"} working={checking || applying || loading}>
        {applying ? "Installing" : checking ? "Checking…" : error ? "Couldn't check"
          : pending ? "Ready to install" : upToDate ? "Up to date" : loading ? "Checking status…" : "Not checked yet"}
      </PanelBadge>
      <Button aria-label="Check CARE software updates" disabled={checking || locked} onClick={() => void check()}>Check now</Button>
    </div>
    {applying ? <div className="panel-update-progress" role="status">
      <h3>Installing the CARE software update…</h3>
      <Progress aria-label="CARE software update" />
      <p className="panel-small">Keep this computer on and CARE Desktop open. Staff may be signed out briefly.</p>
    </div> : pending ? <PanelNotice tone="ok" title="An update is ready">
      {backendUpdate ? "Staff will be signed out briefly while it installs. " : "CARE will reload when the update is applied. "}
      If you choose Later, it will be applied the next time the clinic starts.
      <div className="panel-actions">
        <Button disabled={locked || checking} onClick={() => void later()}>Later</Button>
        <Button variant="primary" disabled={locked || checking} onClick={apply}>
          <Download aria-hidden="true" />Install now
        </Button>
      </div>
    </PanelNotice> : checking ? <p className="panel-small" role="status">
      Anything found is prepared in the background. You can keep using the clinic while this runs.
    </p> : null}
    {error || action.error ? <PanelNotice title={error ? "Couldn't check for updates" : "Couldn't finish that"} tone="warning"
      actions={<><Button disabled={checking || locked} onClick={() => void check()}><RefreshCw aria-hidden="true" />Try again</Button><PanelLogButton /></>}>
      {error || action.error}
    </PanelNotice> : null}
  </section>;
}

function DesktopApplicationCard({ controller }: { controller: AppUpdateController }) {
  const { system } = useCare();
  const { update, version, checking, problem, progress, active, disabled, check, install } = controller;
  const action = usePanelTask();
  const [checkedAt, setCheckedAt] = useState(0);
  useEffect(() => { if (update) setCheckedAt(Date.now()); }, [update]);
  const phase = progress?.phase;
  const downloading = phase === "downloading" ? progress : null;
  const percent = downloading && downloading.total > 0
    ? Math.max(0, Math.min(100, Math.round(downloading.done / downloading.total * 100))) : undefined;
  const target = update?.version ? ` ${update.version}` : "";
  const phaseTitle = phase === "downloading" ? `Downloading CARE Desktop${target}…`
    : phase === "verifying" ? "Checking the downloaded update…"
      : phase === "installing" ? `Installing CARE Desktop${target}…`
        : phase === "restarting" ? "Installed — restarting CARE Desktop…"
          : phase === "installer" ? "The installer has opened"
            : "Preparing the CARE Desktop update…";
  const problemTitle = problem?.kind === "download" ? "The download didn't come through properly"
    : problem?.kind === "install" ? "CARE Desktop couldn't finish updating"
      : problem?.kind === "unavailable" ? "New version, but not for this computer yet"
        : "Couldn't check for updates";
  const problemDetail = problem?.kind === "offline" ? "Couldn't connect to the update service. Check the internet connection and try again."
    : problem?.kind === "download" ? "Nothing was installed. Try again on a steadier connection."
      : problem?.kind === "install" ? "Try again. If it keeps failing, share the log file with your support contact."
        : problem?.kind === "unavailable" ? `${problem.version} is available for other systems. Check again another day.`
          : "Updates couldn't be checked right now. Try again. If it keeps failing, share the log file with support.";

  return <section className="panel-card panel-card-pad panel-update-card" aria-label="CARE Desktop application">
    <div className="panel-update-head">
      <span className="panel-tile"><Monitor aria-hidden="true" /></span>
      <div className="panel-grow">
        <h2 className="panel-title">CARE Desktop application</h2>
        <p className="panel-card-sub">{version ? `Version ${version}` : "Checking the application version…"}
          {checkedAt ? ` · Checked at ${checkedTime(checkedAt)}` : ""}</p>
      </div>
      <PanelBadge tone={problem ? "warning" : update ? "ok" : "neutral"} working={checking || active}>
        {active ? "Updating" : checking ? "Checking…" : problem ? "Needs attention"
          : update?.available ? `${update.version} available` : update ? "Up to date" : "Not checked yet"}
      </PanelBadge>
      {!active ? <Button aria-label="Check CARE Desktop updates" disabled={checking || disabled} onClick={() => void check()}>Check now</Button> : null}
    </div>
    {active ? <div className="panel-update-progress" role="status" aria-live="polite">
      <h3>{phaseTitle}</h3>
      {phase !== "installer" && phase !== "restarting" ? <Progress value={percent} aria-label="CARE Desktop update download" /> : null}
      {downloading ? <p className="panel-small">
        {downloading.total > 0 ? `${megabytes(downloading.done) || "0 MB"} of ${megabytes(downloading.total)}`
          : downloading.done > 0 ? `${megabytes(downloading.done)} downloaded` : "Starting download…"}
        {percent !== undefined ? ` · ${percent}%` : ""}
      </p> : null}
      <p className="panel-small">{phase === "installer" ? "Follow the installer to finish, then reopen CARE Desktop."
        : phase === "restarting" ? "CARE Desktop is reopening."
          : "Keep CARE Desktop open. Your computer may ask for permission to replace the app."}</p>
      {system === "running" ? <p className="panel-small">The clinic keeps running while CARE Desktop updates.</p> : null}
      {phase === "installer" ? <Button disabled={controller.updating} onClick={controller.dismiss}>Done</Button> : null}
    </div> : problem ? <PanelNotice title={problemTitle} tone={problem.kind === "download" || problem.kind === "install" ? "danger" : "warning"}
      actions={<>
        {problem.kind !== "unavailable" ? <Button disabled={disabled || checking}
          onClick={() => void (problem.kind === "download" || problem.kind === "install" ? install() : check())}>Try again</Button> : null}
        <PanelLogButton />
      </>}>
      {problemDetail}
    </PanelNotice> : update?.available ? <PanelNotice tone="ok" title={`CARE Desktop ${update.version} is available`}>
      Downloads and installs the new version. Your computer may ask for permission.
      {system === "running" ? " Your clinic keeps running." : ""}
      <div className="panel-actions">
        {update.notes_url ? <Button disabled={action.working} onClick={() => void action.run(() => bridge.OpenURL(update.notes_url),
          "Couldn't open what's new. Try again when this computer is connected to the internet.")}>
          <ScrollText aria-hidden="true" />What&apos;s new
        </Button> : null}
        <Button variant="primary" disabled={disabled || checking} onClick={() => void install()}>
          <Download aria-hidden="true" />Update CARE Desktop
        </Button>
      </div>
    </PanelNotice> : null}
    {action.error ? <p className="panel-inline-error" role="alert">{action.error}</p> : null}
  </section>;
}

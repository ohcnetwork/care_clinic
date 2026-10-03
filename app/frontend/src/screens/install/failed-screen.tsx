import { AlertTriangle, ArrowLeft, Download, RotateCcw } from "lucide-react";
import { useRef, useState } from "react";

import { Callout, LogButton } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { useAppUpdate, type AppUpdateController } from "@/hooks/use-app-update";
import { errorText, megabytes } from "@/lib/format";
import { useCare } from "@/state/care-store";

import { InstallLayout } from "./install-layout";

function FailedInstallUpdate({ controller }: { controller: AppUpdateController }) {
  const { update, checking, problem, progress, active, disabled, check, install } = controller;

  if (checking) {
    return <p className="install-update-checking" role="status"><span aria-hidden="true"><Spinner /></span>Checking for a CARE Desktop update…</p>;
  }
  if (active) {
    const phase = progress?.phase;
    const title = phase === "downloading" ? `Downloading CARE Desktop ${update?.version || ""}…`
      : phase === "verifying" ? "Checking the CARE Desktop download…"
      : phase === "installing" ? "Updating the CARE Desktop application…"
      : phase === "restarting" ? "CARE Desktop is restarting…"
      : phase === "installer" ? "The CARE Desktop installer has opened"
      : "Preparing the CARE Desktop update…";
    const detail = phase === "installer" ? "Finish updating in the installer, then reopen CARE Desktop before trying setup again."
      : phase === "installing" ? "Your computer may ask for permission to replace the app."
      : phase === "restarting" ? "Reopen CARE Desktop after the update to try setup again."
      : "Keep this window open until the update finishes.";
    return (
      <section className="on-card install-update" aria-label="CARE Desktop update">
        <div role="status" aria-atomic="true"><h2>{title}</h2><p>{detail}</p></div>
        {phase !== "restarting" && phase !== "installer" ? <Progress className="install-progress" aria-label="CARE Desktop update progress" /> : null}
        {progress?.phase === "downloading" && progress.done > 0 ? <div className="install-update-meta">
          {megabytes(progress.done)}{progress.total > 0 ? ` of ${megabytes(progress.total)}` : " downloaded"}
        </div> : null}
      </section>
    );
  }
  if (problem) {
    const updateFailed = problem.kind === "download" || problem.kind === "install";
    const canRetryUpdate = updateFailed && update?.available;
    const title = updateFailed ? "The CARE Desktop update didn't finish"
      : problem.kind === "unavailable" ? `CARE Desktop ${problem.version} isn't available for this computer yet`
      : "Couldn't check for a CARE Desktop update";
    return (
      <section className="on-card install-update" aria-label="CARE Desktop update">
        <div role="alert">
          <h2>{title}</h2>
          <p>{updateFailed ? "Try updating again, or retry setup with your current version. The log file has the details."
            : "You can try setup again with your current version, or check for an update later."}</p>
        </div>
        <div className="on-actions">
          <Button disabled={disabled} onClick={() => void (canRetryUpdate ? install() : check())}>
            {canRetryUpdate ? "Retry update" : "Check again"}
          </Button>
        </div>
      </section>
    );
  }
  if (!update?.available) return null;
  return (
    <section className="on-card install-update" aria-label="CARE Desktop update">
      <div className="install-update-row">
        <span className="on-tile" aria-hidden="true"><Download /></span>
        <div className="on-grow">
          <h2>CARE Desktop {update.version} is available</h2>
          <p>Worth installing before you try again — it may be the fix.</p>
        </div>
        <Button className="install-update-primary" disabled={disabled} onClick={() => void install()}>Update now</Button>
      </div>
    </section>
  );
}

export function FailedScreen() {
  const { run, resumeInstall, retryInstall, restartSetup, busy } = useCare();
  const pending = useRef(false);
  const [retrying, setRetrying] = useState(false);
  const [retryError, setRetryError] = useState("");
  const canResume = run.failure?.can_retry === true;
  const interrupted = run.failure?.download_interrupted === true;
  const update = useAppUpdate(retrying, !interrupted, () => pending.current);
  const disabled = retrying || busy || update.active;

  const retry = async () => {
    if (pending.current || busy || update.isActive()) return;
    pending.current = true;
    setRetrying(true);
    setRetryError("");
    try {
      await (canResume ? resumeInstall() : retryInstall());
    } catch (error) {
      setRetryError(canResume
        ? "Installation couldn't be retried. Your saved setup has been kept. Check that the backup and recovery files are available, then try again or open the log file for support."
        : /\b(cancelled|canceled|declined)\b/i.test(errorText(error))
        ? "Cleanup was cancelled. Setup hasn't restarted and some unfinished installation may remain. Try again when you're ready."
        : "The unfinished installation couldn't be cleared. Setup hasn't restarted. Try again, or share the log file with your support contact.");
    } finally {
      pending.current = false;
      setRetrying(false);
    }
  };

  return (
    <InstallLayout
      failed
      title={interrupted ? "The download was interrupted" : "Something went wrong during installation"}
      subtitle="Installation stopped. Your clinic isn't ready to use yet."
      footer={<>
        <Button className="on-back" variant="ghost" disabled={disabled} onClick={() => {
          if (!pending.current && !busy && !update.isActive()) restartSetup();
        }}><ArrowLeft aria-hidden="true" />Back to setup</Button>
        <p className="on-foot-note" role="status">{retrying ? canResume ? "Retrying installation…" : "Clearing the unfinished installation…"
          : update.active ? "Finish the CARE Desktop update before trying again."
          : canResume ? "Trying again keeps your saved setup."
          : "Trying again clears the unfinished install first."}</p>
        <Button variant={canResume ? "primary" : "destructive"} className={canResume ? "install-retry-action install-resume-action" : "install-retry-action"} disabled={disabled}
          aria-describedby="install-retry-consequences" onClick={() => void retry()}>
          {retrying ? <span aria-hidden="true"><Spinner /></span> : <RotateCcw aria-hidden="true" />}
          {retrying ? canResume ? "Retrying installation…" : "Clearing installation…" : "Try again"}
        </Button>
      </>}
    >
      <section className="on-card install-failure-card" aria-labelledby="install-failure-heading">
        <div className="on-card-head">
          <span className="on-tile on-large on-bad" aria-hidden="true"><AlertTriangle /></span>
          <div className="on-grow"><h2 id="install-failure-heading">{interrupted ? "Check the internet connection" : "CARE couldn't finish setting up the clinic"}</h2>
            <p>{interrupted ? "A required download didn't finish. Keep CARE Desktop open, reconnect to the internet, then try again."
              : "It stopped partway through. The details are in the log file."}</p></div>
        </div>
        <LogButton />
      </section>
      {!interrupted || update.active ? <FailedInstallUpdate controller={update} /> : null}
      <div className={canResume ? "install-retry-consequences install-resume-consequences" : "install-retry-consequences"} id="install-retry-consequences">
        {canResume ? <Callout tone="info" title="Your setup choices are kept">
          <p>Trying again keeps your clinic address, backup folder, admin password and saved recovery files. Completed downloads and images can be reused.</p>
          <p>Keep CARE Desktop open to continue this installation without starting setup over.</p>
        </Callout> : <Callout tone="danger" title="Trying again starts the backup and password steps over">
          <p>The unfinished installation is cleared. Your clinic address and existing backups are kept. Choose the backup folder and admin password again, and save fresh recovery materials.</p>
          <p>The old Desktop admin codes stop working. Keep your old backup recovery files — you may still need them to open earlier backups.</p>
        </Callout>}
      </div>
      {retryError ? <Callout tone="danger" title={canResume ? "Installation hasn't restarted" : "Setup hasn't restarted"}>{retryError}</Callout> : null}
    </InstallLayout>
  );
}

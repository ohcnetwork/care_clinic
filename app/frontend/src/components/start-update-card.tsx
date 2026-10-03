import { Check, Download, FileText } from "lucide-react";
import { useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import type { AppUpdateController } from "@/hooks/use-app-update";
import { bridge } from "@/lib/bridge";
import { errorText, megabytes } from "@/lib/format";
import { useCare } from "@/state/care-store";

export function StartUpdateCard({ controller, context = "start" }: {
  controller: AppUpdateController;
  context?: "start" | "client" | "setup";
}) {
  const { update, version, checking, problem, progress, active, updating, disabled, check, install, dismiss } = controller;
  const { log } = useCare();
  const [actionError, setActionError] = useState("");
  const [opening, setOpening] = useState(false);
  const openingRef = useRef(false);
  const releaseVersion = update?.version || "";
  const phase = progress?.phase;
  const downloading = progress?.phase === "downloading" ? progress : null;
  const percent = downloading && downloading.total > 0
    ? Math.max(0, Math.min(100, Math.round(downloading.done / downloading.total * 100)))
    : undefined;

  const open = async (kind: "notes" | "log") => {
    if (openingRef.current) return;
    openingRef.current = true;
    setOpening(true);
    setActionError("");
    try {
      if (kind === "notes") {
        if (!update?.notes_url) throw new Error("The release has no notes URL.");
        await bridge.OpenURL(update.notes_url);
      }
      else await bridge.OpenLogFolder();
    } catch (error) {
      log(`open ${kind}: ${errorText(error)}`);
      setActionError(kind === "notes"
        ? "Couldn't open what's new. Try again."
        : "Couldn't open the log file. Try again, or ask the person who looks after this computer.");
    } finally {
      openingRef.current = false;
      setOpening(false);
    }
  };

  let eyebrow = "";
  let title = "";
  let detail = "";
  let tone = "";
  let retry: "check" | "install" | null = null;
  let showLog = false;

  if (active) {
    eyebrow = "Updating";
    switch (phase) {
      case "downloading":
        title = `Downloading CARE Desktop ${releaseVersion}\u2026`;
        detail = "Keep this window open. You can continue once the update has finished.";
        break;
      case "verifying":
        title = `Checking CARE Desktop ${releaseVersion}\u2026`;
        detail = "Keep this window open. You can continue once the update has finished.";
        break;
      case "installing":
        title = `Installing CARE Desktop ${releaseVersion}\u2026`;
        detail = "Your computer may ask for permission to replace the app.";
        break;
      case "restarting":
        title = "Restarting CARE Desktop to finish updating\u2026";
        detail = "You'll be back here in a moment.";
        break;
      case "installer":
        title = "The installer has opened";
        detail = "Follow it to finish updating, then reopen CARE Desktop.";
        break;
      default:
        title = "Preparing the update\u2026";
        detail = "Keep this window open. You can continue once the update has finished.";
    }
  } else if (problem) {
    const failed = problem.kind === "install" || problem.kind === "download";
    eyebrow = failed ? "Update didn't finish" : "Updates";
    tone = failed ? "danger" : "warn";
    switch (problem.kind) {
      case "install":
        title = "Your current version was kept";
        detail = "Nothing about your clinic has changed. Try again \u2014 if it keeps failing, share the log file.";
        retry = "install";
        showLog = true;
        break;
      case "location":
        title = "Open an installed copy to update";
        detail = "Install CARE Desktop in a permanent folder first. On macOS, open the copy outside the disk image; on Windows, use the installed Start menu shortcut. Development copies cannot update themselves.";
        showLog = true;
        break;
      case "download":
        title = "The download didn't come through properly";
        detail = "Nothing was installed. Try again on a steadier connection.";
        retry = "install";
        break;
      case "offline":
        title = "Couldn't check for updates";
        detail = "This computer seems to be offline. Connect to the internet and try again.";
        retry = "check";
        break;
      case "check":
        title = "Couldn't check for updates";
        detail = "Updates couldn't be checked right now. Try again \u2014 if it keeps failing, share the log file.";
        retry = "check";
        showLog = true;
        break;
      case "unavailable":
        title = "New version, but not for this computer yet";
        detail = `${problem.version} is out for other systems. Check again another day.`;
    }
  } else if (update?.available) {
    eyebrow = "Update available";
    title = `CARE Desktop ${releaseVersion}`;
    detail = context === "setup" ? "Best done before installing. CARE Desktop will close briefly and reopen."
      : "Updates this installed copy and reopens CARE Desktop. Your computer may ask for permission. Clinic data is kept and a running clinic is not stopped.";
  }

  return (
    <div className="start-updates" aria-label="CARE Desktop updates">
      {title ? (
        <div className={`start-update-card ${tone ? `start-update-${tone}` : ""}`}>
          <div role={problem ? "alert" : "status"} aria-live={problem ? "assertive" : "polite"} aria-atomic="true">
            <div className="start-update-eyebrow">{eyebrow}</div>
            <div className="start-update-title">{title}</div>
            <p className="start-update-detail">{detail}</p>
          </div>
          {active && phase !== "restarting" && phase !== "installer" ? (
            <>
              <Progress className="start-update-progress" value={percent} aria-label={title} />
              {downloading ? (
                <div className="start-update-meta">
                  <span>
                    {downloading.total > 0
                      ? `${megabytes(downloading.done) || "0 MB"} of ${megabytes(downloading.total)}`
                      : downloading.done > 0 ? `${megabytes(downloading.done)} downloaded` : "Starting download\u2026"}
                  </span>
                  {percent !== undefined ? <span>{percent}%</span> : null}
                </div>
              ) : null}
            </>
          ) : null}
          {!active && update?.available && !problem ? (
            <div className="start-update-actions">
              <Button variant="white" size="sm" disabled={disabled || checking} onClick={() => void install()}>
                <Download aria-hidden="true" />Update now
              </Button>
              {context === "setup" ? (
                <Button variant="glass" size="sm" disabled={disabled} onClick={dismiss}>Later</Button>
              ) : update.notes_url ? (
                <Button variant="glass" size="sm" disabled={disabled || opening} onClick={() => void open("notes")}>
                  What's new
                </Button>
              ) : null}
            </div>
          ) : null}
          {retry ? (
            <div className="start-update-actions">
              <Button
                variant={tone === "danger" ? "white" : "glass"}
                size="sm"
                disabled={disabled || checking}
                onClick={() => void (retry === "install" ? install() : check())}
              >
                Try again
              </Button>
              {showLog ? (
                <Button variant="glass" size="sm" disabled={opening} onClick={() => void open("log")}>
                  <FileText aria-hidden="true" />Open log file
                </Button>
              ) : null}
            </div>
          ) : null}
          {(active && phase === "installer") || (!active && problem?.kind === "unavailable") ? (
            <div className="start-update-actions">
              <Button variant="glass" size="sm" disabled={phase === "installer" ? updating : disabled} onClick={dismiss}>
                {phase === "installer" ? "OK" : "Dismiss"}
              </Button>
            </div>
          ) : null}
          {actionError ? <p className="start-update-action-error" role="alert">{actionError}</p> : null}
        </div>
      ) : null}
      <div className="start-version-line">
        <span className="start-version">Version {version || "\u2026"}</span>
        {!title && checking ? (
          <span className="start-update-checking" role="status">
            <span aria-hidden="true"><Spinner className="size-[13px]" /></span>
            {"Checking for updates\u2026"}
          </span>
        ) : !title ? (
          <>
            {update && !update.available ? (
              <span className="start-version-ok" role="status"><Check aria-hidden="true" />Up to date</span>
            ) : null}
            <button type="button" className="start-check-link" disabled={disabled} onClick={() => void check()}>
              Check for updates
            </button>
          </>
        ) : null}
      </div>
    </div>
  );
}

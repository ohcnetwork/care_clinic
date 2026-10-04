import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { useAppUpdate } from "@/hooks/use-app-update";
import { bridge } from "@/lib/bridge";
import { megabytes } from "@/lib/format";
import type { AppUpdateProgress } from "@/types";

const phaseText = (phase: AppUpdateProgress["phase"], version: string) =>
  ({
    downloading: `Downloading CARE Clinic ${version}...`,
    verifying: `Checking CARE Clinic ${version}...`,
    installing: `Installing CARE Clinic ${version}...`,
    restarting: "Restarting CARE Clinic to finish updating...",
    installer: "The installer is open. Follow it to finish updating.",
  })[phase];

export function AppUpdateCard({ disabled = false }: { disabled?: boolean }) {
  const { update, version, checking, problem, progress, active, check, install, disabled: blocked } =
    useAppUpdate(disabled);
  const error = !problem ? "" : problem.kind === "install" || problem.kind === "download"
    ? "CARE Clinic could not finish updating. Your current version was kept. Try again."
    : problem.kind === "location"
      ? "Install CARE Clinic in a permanent folder first, then open that copy and try again. On macOS, leave the disk image; on Windows, use the installed Start menu shortcut. Development copies cannot update themselves."
    : problem.kind === "unavailable"
      ? "There is no installer for this computer yet. Check again another day."
      : "Couldn't check for updates. Check your internet connection and try again.";

  return (
    <div className="rounded-xl border border-line bg-card px-[18px] py-4 shadow-card">
      <div className="flex items-center gap-3.5">
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-bold text-ink">CARE Clinic updates</div>
          <div className="mt-[3px] text-[13px] text-muted-foreground">
            Version <span className="font-mono text-ink2">{update?.current || version || "..."}</span>
            {update?.available ? (
              <>
                {" - "}
                <span className="font-semibold text-brand-ink">{update.version} is available</span>
              </>
            ) : update?.version ? (
              " - up to date"
            ) : null}
          </div>
          <p className="mt-1 text-[13px] text-muted-foreground">
            Update this application without setting up a server or connecting to a clinic.
          </p>
        </div>
        <Button disabled={blocked || checking || active} onClick={() => void check()}>
          {checking ? "Checking..." : "Check now"}
        </Button>
      </div>

      {update?.available && active ? (
        <UpdateProgress progress={progress} version={update.version} />
      ) : null}

      {update?.available && !active ? (
        <div className="mt-3.5 flex flex-wrap items-center gap-3 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
          <span className="min-w-[180px] flex-1">
            Downloads {update.asset}, verifies its checksum, and installs the update.
            CARE Clinic closes briefly, replaces this installed copy, and reopens.
            Your computer may ask for permission. Clinic data and settings are kept;
            a running clinic is not stopped.
          </span>
          {update.notes_url ? (
            <Button onClick={() => void bridge.OpenURL(update.notes_url)}>Release notes</Button>
          ) : null}
          <Button variant="primary" disabled={blocked || checking || active} onClick={() => void install()}>
            Update CARE Clinic
          </Button>
        </div>
      ) : null}

      {error ? <div role="alert" className="mt-2.5 text-[12.5px] text-danger-ink">{error}</div> : null}
    </div>
  );
}

function UpdateProgress({
  progress,
  version,
}: {
  progress: AppUpdateProgress | null;
  version: string;
}) {
  const phase = progress?.phase ?? "downloading";
  const downloading = phase === "downloading";
  const pct =
    downloading && progress && progress.total > 0
      ? Math.min(100, Math.round((progress.done / progress.total) * 100))
      : undefined;
  const detail =
    downloading && progress && progress.total > 0
      ? `${megabytes(progress.done)} of ${megabytes(progress.total)}`
      : "";

  return (
    <div className="mt-3.5 rounded-lg border border-line bg-brand-bg px-4 py-[13px] text-[12.5px] text-brand-ink">
      <div className="flex items-baseline gap-3">
        <span className="flex-1 font-semibold">{phaseText(phase, version)}</span>
        {detail ? <span className="font-mono text-ink2">{detail}</span> : null}
      </div>
      <Progress className="mt-2.5" value={pct} />
      {phase === "installing" ? (
        <div className="mt-2 text-muted-foreground">
          macOS may ask for an administrator password to replace the app.
        </div>
      ) : null}
    </div>
  );
}

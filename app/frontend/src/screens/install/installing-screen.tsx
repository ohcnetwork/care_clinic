import { Check, Coffee } from "lucide-react";
import { useEffect, useRef } from "react";

import { Callout, LogButton } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { Progress } from "@/components/ui/progress";
import { useElapsed } from "@/hooks/use-elapsed";
import { onCareEvent } from "@/lib/bridge";
import { mmss } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCare } from "@/state/care-store";

import { InstallLayout } from "./install-layout";

const QUIET_WARNING_MS = 15 * 60 * 1000;

export function InstallingScreen() {
  const { run } = useCare();
  const elapsed = useElapsed(run.startedAt, true);
  const elapsedTime = mmss(elapsed).padStart(5, "0");
  const lastLogAt = useRef(run.startedAt || Date.now());

  useEffect(() => {
    lastLogAt.current = run.startedAt || Date.now();
    // Only actual host log events reset silence, not UI logs or phase weights.
    return onCareEvent("care-log", (line: unknown) => {
      if (typeof line === "string") lastLogAt.current = Date.now();
    });
  }, [run.startedAt]);

  const quiet = Date.now() - lastLogAt.current >= QUIET_WARNING_MS;
  // The legacy weight only tells us whether a log milestone has arrived.
  // It is not measured progress and must never become a displayed percentage.
  const stageIndex = run.pct > 0 ? run.stepIdx : -1;
  const stage = run.finished ? "Finishing installation"
    : run.steps[stageIndex]?.label || "Preparing the installation";

  return (
    <InstallLayout
      title="Installing CARE"
      subtitle="This takes 5 to 20 minutes. You can leave this window open and do something else."
      headerAside={<time className="install-elapsed" role="timer" aria-live="off" dateTime={`PT${elapsed}S`} aria-label={`Elapsed installation time: ${elapsedTime}`}>{elapsedTime}</time>}
      footer={<p className="on-foot-note">Installing — please don't close this window.</p>}
    >
      <section className="on-card install-progress-card" aria-label="Installation progress">
        <div role="status" aria-live="polite" aria-atomic="true">
          {quiet ? <div className="on-eyebrow">Last reported stage</div> : null}
          <h2 className="install-current-label">{stage}</h2>
        </div>
        <Progress className="install-progress" aria-label="Installation progress" />
        <ol className="install-stages" aria-label="Installation stages">
          {run.steps.map((step, index) => {
            const state = index < stageIndex ? "past" : index === stageIndex ? "now" : "waiting";
            return (
              <li key={step.label} className={cn("install-stage", `install-stage-${state}`)}
                aria-current={state === "now" ? "step" : undefined}>
                <span className="install-stage-mark" aria-hidden="true">
                  {state === "past" ? <Check /> : state === "now" ? <Spinner /> : "·"}
                </span>
                <span className="install-stage-name">{step.label}</span>
                <span className="sr-only">: {state === "past" ? "Earlier stage" : state === "now" ? "Latest reported stage" : "Not reported yet"}</span>
              </li>
            );
          })}
        </ol>
      </section>
      {quiet ? (
        <div role="status">
          <Callout tone="warn" title="This is taking longer than usual">
            No new installation messages have arrived for at least 15 minutes. Open the log file and share it with your support contact before closing CARE Clinic.
          </Callout>
        </div>
      ) : (
        <div className="install-reassurance">
          <Coffee aria-hidden="true" />
          <div><strong>A good time for a break</strong>Keep the computer on, awake and connected to the internet. The control panel will open when your clinic is ready.</div>
        </div>
      )}
      <div className="install-support">
        <LogButton />
        <p className="on-small">For technical support only — you don't need to read it.</p>
      </div>
    </InstallLayout>
  );
}

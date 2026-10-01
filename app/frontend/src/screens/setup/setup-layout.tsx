import { ArrowLeft, ArrowRight, Check, Download, X } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";

import { Callout, OnboardingBrand, OnboardingUpdates } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import type { AppUpdateController } from "@/hooks/use-app-update";
import { cn } from "@/lib/utils";
import type { SetupPage } from "@/types";
import { SETUP_LABELS } from "./setup-model";

export function SetupLayout({ steps, page, done, blocked = [], working, title, subtitle, note, back, next, nextDisabled, editing, update, children }: {
  steps: SetupPage[];
  page: SetupPage;
  done: Partial<Record<SetupPage, boolean>>;
  blocked?: SetupPage[];
  working: boolean;
  title: string;
  subtitle: string;
  note: string;
  back?: () => void;
  next?: () => void;
  nextDisabled?: boolean;
  editing?: boolean;
  update: AppUpdateController;
  children: ReactNode;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const focusPending = useRef(true);
  const active = editing ? "review" : page;
  const index = Math.max(0, steps.indexOf(active));
  useEffect(() => {
    focusPending.current = true;
    body.current?.scrollTo({ top: 0 });
  }, [page]);
  useEffect(() => {
    if (!focusPending.current) return;
    const field = body.current?.querySelector<HTMLInputElement>("#mdnsname, #adminpw");
    if (field?.disabled) {
      heading.current?.focus({ preventScroll: true });
      return;
    }
    (field ?? heading.current)?.focus({ preventScroll: true });
    focusPending.current = false;
  }, [page, working, update.active]);
  return (
    <div className="onboarding onboarding-flow on-setup">
      <aside className="on-rail" aria-label="Setup progress">
        <OnboardingBrand wizard />
        <div className="on-rail-sep" />
        <div className="on-rail-progress">
          <span>Step {index + 1} of {steps.length}</span>
          <Progress value={index / steps.length * 100} aria-label="Setup progress" />
        </div>
        <ol className="on-rail-steps">{steps.map((step) => {
          const current = step === active;
          const bad = blocked.includes(step);
          const finished = done[step] && !bad;
          const state = current ? working ? "In progress" : "Current step" : bad ? "Needs attention" : finished ? "Done" : "Not started";
          return <li key={step} aria-current={current ? "step" : undefined} className={cn("on-rail-step", finished && "on-done", current && "on-current", bad && "on-blocked")}>
            <span className="on-step-dot" aria-hidden="true">{bad ? <X /> : current && working ? <Spinner /> : finished ? <Check /> : null}</span>
            <span>{SETUP_LABELS[step]}</span><span className="sr-only">: {state}</span>
          </li>;
        })}</ol>
        <div className="on-rail-spacer" />
        <OnboardingUpdates controller={update} context="setup" />
      </aside>
      <main className="on-main" aria-labelledby="setup-title">
        <header className="on-head"><h1 className="on-title" id="setup-title" tabIndex={-1} ref={heading}>{title}</h1><p className="on-subtitle">{subtitle}</p></header>
        <div className="on-body" ref={body}>
          <div className="on-stack">
            {editing ? <Callout tone="info" title="Fixing this for the review step">Your choices are kept. When this is sorted you go straight back to Review, without walking through the other steps again.</Callout> : null}
            {children}
          </div>
        </div>
        <footer className="on-foot">
          {back ? <Button className="on-back" variant="ghost" disabled={working || update.active} onClick={back}><ArrowLeft aria-hidden="true" />{editing ? "Back to review" : "Back"}</Button> : null}
          <p className="on-foot-note" role="status">{update.active ? "Wait for the CARE Desktop update to finish." : note}</p>
          {next ? <Button variant="primary" className="on-primary" disabled={working || update.active || nextDisabled} onClick={next}>
            {page === "review" ? <Download aria-hidden="true" /> : null}
            {page === "review" ? "Install" : editing ? "Back to review" : "Continue"}
            {page !== "review" ? <ArrowRight aria-hidden="true" /> : null}
          </Button> : null}
        </footer>
      </main>
    </div>
  );
}

import { Check, X } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";

import { OnboardingBrand } from "@/components/onboarding";
import { Spinner } from "@/components/spinner";
import { cn } from "@/lib/utils";
import { SETUP_LABELS } from "@/screens/setup/setup-model";
import { useCare } from "@/state/care-store";

import "./install.css";

export function InstallLayout({ title, subtitle, failed = false, headerAside, footer, children }: {
  title: string;
  subtitle: string;
  failed?: boolean;
  headerAside?: ReactNode;
  footer: ReactNode;
  children: ReactNode;
}) {
  const { run, version } = useCare();
  const heading = useRef<HTMLHeadingElement>(null);
  const index = Math.max(0, run.pages.indexOf("install"));

  useEffect(() => {
    heading.current?.focus({ preventScroll: true });
  }, [title]);

  return (
    <div className={cn("onboarding onboarding-flow install-screen", failed && "install-failed")}>
      <aside className="on-rail" aria-label="Setup progress">
        <OnboardingBrand wizard />
        <div className="on-rail-sep" />
        <div className="on-rail-progress">
          <span>Step {index + 1} of {run.pages.length}</span>
          <div className="install-rail-track" aria-hidden="true">
            <span style={{ width: `${index / Math.max(1, run.pages.length) * 100}%` }} />
          </div>
        </div>
        <ol className="on-rail-steps">
          {run.pages.map((page, position) => {
            const current = page === "install";
            const done = position < index;
            return (
              <li key={page} aria-current={current ? "step" : undefined}
                className={cn("on-rail-step", done && "on-done", current && "on-current", current && failed && "on-blocked")}>
                <span className="on-step-dot" aria-hidden="true">
                  {current ? failed ? <X /> : <Spinner /> : done ? <Check /> : null}
                </span>
                <span>{SETUP_LABELS[page]}</span>
                <span className="sr-only">: {current ? failed ? "Installation stopped" : "In progress" : done ? "Done" : "Not started"}</span>
              </li>
            );
          })}
        </ol>
        <div className="on-rail-spacer" />
        <div className="install-version">Version {version || "…"}</div>
      </aside>
      <main className="on-main" aria-labelledby="install-title">
        <header className="on-head install-head">
          <div className="on-grow">
            <h1 className="on-title" id="install-title" tabIndex={-1} ref={heading}>{title}</h1>
            <p className="on-subtitle">{subtitle}</p>
          </div>
          {headerAside}
        </header>
        <div className="on-body">
          <div className="install-content">{children}</div>
        </div>
        <footer className="on-foot install-foot">{footer}</footer>
      </main>
    </div>
  );
}

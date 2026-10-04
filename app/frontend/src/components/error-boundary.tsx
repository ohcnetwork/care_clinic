import { Component, type ErrorInfo, type ReactNode } from "react";

// A clinic operator can do nothing with a blank window. Anything that escapes a
// render lands here instead, so there is always a message and a way back.
type Props = { children: ReactNode };
type State = { error: Error | null };

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("CARE Clinic crashed:", error, info.componentStack);
  }

  render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;

    return (
      <div className="flex h-full items-center justify-center bg-background p-10">
        <div className="max-w-[560px] rounded-2xl border border-line bg-card p-[26px] shadow-card">
          <div className="text-[17px] font-bold text-ink">CARE Clinic hit a problem</div>
          <p className="mt-1.5 text-[13px] text-muted-foreground">
            CARE Clinic could not continue. Read the message below before retrying.
          </p>
          <pre className="mt-4 max-h-[220px] overflow-auto rounded-lg bg-[#0b1f17] px-4 py-3.5 font-mono text-[12.5px] leading-[1.6] break-words whitespace-pre-wrap text-[#d7f7e6]">
            {error.stack?.includes(error.message)
              ? error.stack
              : [`${error.name}: ${error.message}`, error.stack].filter(Boolean).join("\n")}
          </pre>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="mt-3.5 cursor-pointer rounded-md border border-brand bg-brand px-3.5 py-[9px] text-[13px] font-semibold text-white hover:border-brand-dark hover:bg-brand-dark"
          >
            Reload
          </button>
        </div>
      </div>
    );
  }
}

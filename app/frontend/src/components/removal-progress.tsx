import { Spinner } from "@/components/spinner";
import { useCare } from "@/state/care-store";

export function RemovalProgress() {
  const { busy, busyLabel, operationError } = useCare();
  if (!busy || (busyLabel !== "Uninstalling" && busyLabel !== "Removing CARE Clinic")) return null;

  // Native permission and quit dialogs stay above this non-modal status layer.
  return <div className="fixed inset-0 z-40 flex items-center justify-center bg-brand-deep/40 p-5 backdrop-blur-[2px]">
    <section className="w-full max-w-[460px] max-h-[calc(100dvh-40px)] overflow-auto rounded-2xl border border-line bg-card p-7 text-center shadow-pop"
      role="status" aria-live="polite" aria-labelledby="removal-progress-title">
      <div className="mb-4 flex justify-center"><Spinner className="size-9" /></div>
      <h2 id="removal-progress-title" className="text-lg font-bold">
        {busyLabel === "Removing CARE Clinic" ? "Removing CARE Clinic" : "Removing CARE from this computer"}
      </h2>
      <p className="mt-3 text-sm text-muted-foreground">Please wait and keep this window open until removal finishes.</p>
      <p className="mt-2 text-sm text-muted-foreground">If your computer asks for permission, approve the system prompt to continue.</p>
      {operationError ? <div className="mt-4 text-sm text-danger-ink" role="alert">
        <strong>{operationError.title}</strong><p>{operationError.message}</p>
      </div> : null}
    </section>
  </div>;
}

import { errorText } from "@/lib/format";

export type OperationError = { action: string; title: string; message: string };

const TITLES: Record<string, string> = {
  start: "CARE couldn't start",
  stop: "CARE couldn't stop",
  restart: "CARE couldn't restart",
  restore: "Restore didn't finish",
  "backup-now": "Backup didn't finish",
  update: "The CARE update didn't finish",
  "free-space": "Cleanup didn't finish",
  autostart: "The startup setting couldn't be saved",
  "dismiss-update": "The update couldn't be deferred",
  uninstall: "Removal didn't finish",
};

export function operationError(action: string, cause: unknown): OperationError {
  const detail = errorText(cause);
  const message = /Desktop admin password does not match/.test(detail)
    ? "The CARE Desktop admin password wasn't accepted. Enter it again."
    : /restore is unfinished/.test(detail)
      ? "An earlier restore needs to finish. Start CARE to recover it before making other changes."
      : /something else is still running|CARE Desktop is closing/.test(detail)
        ? "Another operation is still running. Wait for it to finish before trying again."
        : action === "restore"
          ? "Keep your backup and recovery files safe. Check the restore status and open the log file for support before trying again."
          : "The operation didn't finish. Try again, or open the log file for support.";
  return { action, title: TITLES[action] ?? "CARE couldn't finish that", message };
}

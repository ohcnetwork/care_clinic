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
  "apply-plugins": "The plugins couldn't be applied",
};

export function operationError(action: string, cause: unknown): OperationError {
  const detail = errorText(cause);
  if (detail.includes("plugin rollback is unfinished") || detail.includes("Plugin rollback is unfinished")) {
    return { action, title: TITLES[action] ?? "CARE couldn't finish that",
      message: "Plugin recovery is unfinished. CARE has not been confirmed online. Start clinic from Overview to retry recovery; keep the log file for support." };
  }
  if (action === "apply-plugins") {
    if (detail.startsWith("plugin loading failed; previous settings were restored and CARE is back online:")) {
      return { action, title: TITLES[action],
        message: "There was a problem loading this plugin. The previous plugin settings were restored and CARE is back online. Correct the plugin settings before trying again." };
    }
    if (detail.includes("start CARE successfully before changing plugins")) {
      return { action, title: TITLES[action],
        message: "Start clinic from Overview and wait until it is healthy before changing plugins. The current settings were not changed." };
    }
  }
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

import { useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { bridge } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import type { RestartPlan } from "@/types";
import { useCare } from "@/state/care-store";

/**
 * Offered only when the host says a restart is genuinely outstanding, and only
 * straight after an install. "Later" is a real choice: the operator keeps a
 * working wizard and can finish when the clinic is closed.
 */
export function RestartDialog({
  plan,
  onDismiss,
  disabled = false,
  isBlocked,
}: {
  plan: RestartPlan | null;
  onDismiss: () => void;
  disabled?: boolean;
  isBlocked?: () => boolean;
}) {
  const [restarting, setRestarting] = useState(false);
  const restartingRef = useRef(false);
  const [failure, setFailure] = useState("");
  const { log } = useCare();
  const blocked = disabled || isBlocked?.() === true;

  const restart = async () => {
    if (restartingRef.current || disabled || isBlocked?.()) return;
    restartingRef.current = true;
    setRestarting(true);
    setFailure("");
    try {
      await bridge.RestartNow();
    } catch (err) {
      log(`restart computer: ${errorText(err)}`);
      setFailure("CARE couldn't restart this computer.");
      restartingRef.current = false;
      setRestarting(false);
    }
  };

  return (
    <AlertDialog open={plan !== null}>
      <AlertDialogContent className="onboarding onboarding-dialog">
        <AlertDialogTitle>{plan?.title}</AlertDialogTitle>
        <AlertDialogDescription>{plan?.detail}</AlertDialogDescription>
        {failure ? (
          <div className="mt-3 text-[12.5px] leading-[1.5] text-danger-ink">
            {failure} You can restart the computer yourself instead.
          </div>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={restarting} onClick={onDismiss}>
            I'll restart later
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={restarting || blocked}
            onClick={(e) => {
              // Keep the dialog up: the machine is about to go down, and a
              // closing dialog would look like the restart was cancelled.
              e.preventDefault();
              void restart();
            }}
          >
            {restarting ? <span aria-hidden="true"><Spinner className="size-3.5" /></span> : null}
            {restarting ? "Restarting…" : (plan?.label ?? "Restart now")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

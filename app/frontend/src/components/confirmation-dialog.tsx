import { ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { ConfirmationRequest } from "@/types";

export function ConfirmationDialog() {
  const { log } = useCare();
  const [request, setRequest] = useState<ConfirmationRequest | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const current = useRef<ConfirmationRequest | null>(null);
  const revision = useRef(0);
  const closedThrough = useRef(0);
  const cancelButton = useRef<HTMLButtonElement>(null);

  const receive = useCallback((next: ConfirmationRequest | null) => {
    if (next && next.id <= closedThrough.current) return;
    revision.current++;
    current.current = next;
    setRequest(next);
    setError("");
  }, []);

  const close = useCallback((id: number) => {
    closedThrough.current = Math.max(closedThrough.current, id);
    if (current.current && current.current.id <= id) receive(null);
  }, [receive]);

  const refresh = useCallback(async () => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    const before = revision.current;
    try {
      const next = await bridge.SetConfirmationDialogReady(true);
      if (revision.current === before) {
        if (!next && current.current) close(current.current.id);
        else receive(next);
      }
    } catch (cause) {
      log(`read permission request: ${errorText(cause)}`);
      const message = "The permission request couldn't be checked. Try again.";
      if (current.current) setError(message);
      else toast.error(message);
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }, [close, log, receive]);

  useEffect(() => {
    let active = true;
    const before = revision.current;
    const offRequest = onCareEvent("confirmation-requested", receive);
    const offCancel = onCareEvent("confirmation-cancelled", close);
    void bridge.SetConfirmationDialogReady(true).then((next) => {
      if (active && revision.current === before) receive(next);
    }).catch((cause) => {
      log(`register permission dialog: ${errorText(cause)}`);
      if (active) toast.error("Permission requests couldn't be checked.", {
        action: { label: "Try again", onClick: () => void refresh() },
      });
    });
    return () => {
      active = false;
      offRequest();
      offCancel();
      void bridge.SetConfirmationDialogReady(false).catch((cause) => log(`release permission dialog: ${errorText(cause)}`));
    };
  }, [close, log, receive, refresh]);

  const answer = async (approved: boolean) => {
    if (!request || pending.current) return;
    const id = request.id;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      await bridge.RespondToConfirmation(id, approved);
      close(id);
    } catch (cause) {
      log(`answer permission request: ${errorText(cause)}`);
      if (current.current?.id === id) {
        setError("Your answer couldn't be sent. Try again, or check whether this request is still needed.");
      }
    } finally {
      pending.current = false;
      setBusy(false);
    }
  };

  return <AlertDialog open={request !== null} onOpenChange={(open) => { if (!open) void answer(false); }}>
    <AlertDialogContent className="onboarding onboarding-dialog max-h-[calc(100vh-40px)] overflow-auto"
      onOpenAutoFocus={(event) => { event.preventDefault(); cancelButton.current?.focus(); }}>
      <span className="on-tile on-large mb-4"><ShieldCheck aria-hidden="true" /></span>
      <AlertDialogTitle className="break-words">{request?.title}</AlertDialogTitle>
      <AlertDialogDescription className="whitespace-pre-line break-words">{request?.message}</AlertDialogDescription>
      {error ? <div className="mt-4" role="alert">
        <p className="text-sm text-danger-ink">{error}</p>
        <Button disabled={busy} onClick={() => void refresh()}>Check again</Button>
      </div> : null}
      <AlertDialogFooter>
        <Button ref={cancelButton} disabled={busy} onClick={() => void answer(false)}>Cancel</Button>
        <Button variant="primary" disabled={busy} onClick={() => void answer(true)}>
          {busy ? <Spinner /> : null}Continue
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

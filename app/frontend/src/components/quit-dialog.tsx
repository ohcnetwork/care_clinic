import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  AlertDialog, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { toast } from "@/components/ui/sonner";
import { bridge, onCareEvent } from "@/lib/bridge";
import { errorText } from "@/lib/format";
import { useCare } from "@/state/care-store";
import type { QuitRequest } from "@/types";

export function QuitDialog() {
  const { log } = useCare();
  const [request, setRequest] = useState<QuitRequest | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const currentRequest = useRef<QuitRequest | null>(null);
  const revision = useRef(0);
  const keepWaiting = useRef<HTMLButtonElement>(null);

  const receive = useCallback((next: QuitRequest | null) => {
    revision.current++;
    currentRequest.current = next;
    setRequest(next);
    setError("");
  }, []);

  useEffect(() => {
    let active = true;
    const initialRevision = revision.current;
    const readRequest = async () => {
      try {
        const current = await bridge.SetQuitDialogReady(true);
        if (active && revision.current === initialRevision) receive(current);
      } catch (e) {
        log(`quit dialog: ${errorText(e)}`);
        if (active) toast.error("The close request couldn't be checked. Keep this window open and try again.");
      }
    };
    const off = onCareEvent("quit-requested", receive);
    void readRequest();
    return () => {
      active = false;
      off();
      void bridge.SetQuitDialogReady(false).catch((e) => log(`release quit dialog: ${errorText(e)}`));
    };
  }, [log, receive]);

  const answer = async (quit: boolean) => {
    if (!request || pending.current) return;
    const id = request.id;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      await bridge.RespondToQuit(id, quit);
      if (currentRequest.current?.id === id) receive(null);
    } catch (e) {
      log(`respond to quit: ${errorText(e)}`);
      if (currentRequest.current?.id === id) {
        setError("The close request couldn't be completed. Check it again before closing this window.");
      }
    } finally {
      pending.current = false;
      setBusy(false);
    }
  };

  const refresh = async () => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    const initialRevision = revision.current;
    try {
      const current = await bridge.SetQuitDialogReady(true);
      if (revision.current === initialRevision) receive(current);
    } catch (e) {
      log(`refresh quit request: ${errorText(e)}`);
      if (revision.current === initialRevision) {
        setError("The close request couldn't be checked. Keep this window open and try again.");
      }
    } finally {
      pending.current = false;
      setBusy(false);
    }
  };

  return (
    <AlertDialog open={request !== null} onOpenChange={(open) => { if (!open) void answer(false); }}>
      <AlertDialogContent className="onboarding onboarding-dialog max-h-[calc(100vh-40px)] overflow-auto"
        onOpenAutoFocus={(event) => { event.preventDefault(); keepWaiting.current?.focus(); }}>
        <div>
          <AlertDialogTitle>CARE is still working</AlertDialogTitle>
          <AlertDialogDescription className="whitespace-pre-line">{request?.message}</AlertDialogDescription>
        </div>
        {error ? <div role="alert"><p className="text-sm text-danger-ink">{error}</p><Button disabled={busy} onClick={() => void refresh()}>Check again</Button></div> : null}
        <AlertDialogFooter>
          <Button disabled={busy} onClick={() => void answer(true)}>Quit anyway</Button>
          <Button ref={keepWaiting} variant="primary" disabled={busy} onClick={() => void answer(false)}>Keep waiting</Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

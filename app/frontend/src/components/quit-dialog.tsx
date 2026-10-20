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
import { Spinner } from "@/components/spinner";
import type { CloseChoice, QuitRequest } from "@/types";

export function QuitDialog() {
  const { log } = useCare();
  const [request, setRequest] = useState<QuitRequest | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const currentRequest = useRef<QuitRequest | null>(null);
  const revision = useRef(0);
  const keepWaiting = useRef<HTMLButtonElement>(null);
  const [quitting, setQuitting] = useState(false);

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

  const respond = async (send: (id: number) => Promise<void>, failure: string) => {
    if (!request || pending.current) return;
    const id = request.id;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      await send(id);
      if (currentRequest.current?.id === id) receive(null);
    } catch (e) {
      log(`respond to quit: ${errorText(e)}`);
      if (currentRequest.current?.id === id) setError(failure);
    } finally {
      pending.current = false;
      setBusy(false);
      setQuitting(false);
    }
  };

  const answer = (quit: boolean) => {
    if (pending.current) return Promise.resolve();
    setQuitting(quit);
    return respond((id) => bridge.RespondToQuit(id, quit),
      quit
        ? "The clinic couldn't be stopped, so CARE Clinic is still open. Try again, or keep waiting."
        : "The close request couldn't be completed. Check it again before closing this window.");
  };

  const choose = (choice: CloseChoice) => {
    if (pending.current) return Promise.resolve();
    setQuitting(choice === "quit");
    return respond((id) => bridge.RespondToClose(id, choice),
      choice === "quit"
        ? "The clinic couldn't be stopped, so CARE Clinic is still open. Try again, or keep it running in the background."
        : "The close request couldn't be completed. Check it again before closing this window.");
  };

  const dismiss = () => request?.kind === "close" ? choose("cancel") : answer(false);

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

  const closing = request?.kind === "close";

  return (
    <AlertDialog open={request !== null} onOpenChange={(open) => { if (!open) void dismiss(); }}>
      <AlertDialogContent className="onboarding onboarding-dialog max-h-[calc(100vh-40px)] overflow-auto"
        onOpenAutoFocus={(event) => { event.preventDefault(); keepWaiting.current?.focus(); }}>
        <div>
          <AlertDialogTitle>{closing ? request?.title : "CARE is still working"}</AlertDialogTitle>
          <AlertDialogDescription className="whitespace-pre-line">{request?.message}</AlertDialogDescription>
        </div>
        {quitting ? <p role="status" className="text-sm">Stopping the clinic and Rancher Desktop. This can take a minute or two.</p> : null}
        {error ? <div role="alert"><p className="text-sm text-danger-ink">{error}</p><Button disabled={busy} onClick={() => void refresh()}>Check again</Button></div> : null}
        {closing ? (
          <AlertDialogFooter>
            <Button disabled={busy} onClick={() => void choose("cancel")}>Cancel</Button>
            <Button disabled={busy} onClick={() => void choose("quit")}>
              {quitting ? <Spinner /> : null}Stop clinic and quit
            </Button>
            <Button ref={keepWaiting} variant="primary" disabled={busy} onClick={() => void choose("background")}>
              Keep running in background
            </Button>
          </AlertDialogFooter>
        ) : (
          <AlertDialogFooter>
            <Button disabled={busy} onClick={() => void answer(true)}>{quitting ? <Spinner /> : null}Quit anyway</Button>
            <Button ref={keepWaiting} variant="primary" disabled={busy} onClick={() => void answer(false)}>Keep waiting</Button>
          </AlertDialogFooter>
        )}
      </AlertDialogContent>
    </AlertDialog>
  );
}

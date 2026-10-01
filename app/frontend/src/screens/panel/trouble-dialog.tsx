import { RefreshCw } from "lucide-react";
import { useEffect, useRef } from "react";

import { Spinner } from "@/components/spinner";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { useCare } from "@/state/care-store";
import { RequirementChecklist, usePanelRequirements } from "./panel-requirements";
import { PanelLogButton } from "./panel-ui";

export function TroubleDialog({ onClose }: { onClose: () => void }) {
  const { busy, refresh, tab, log } = useCare();
  const requirements = usePanelRequirements();
  const initialTab = useRef(tab);
  const opener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  const locked = requirements.working || busy;
  useEffect(() => { void requirements.recheck(); }, [requirements.recheck]);
  useEffect(() => {
    if (tab !== initialTab.current && !locked) onClose();
  }, [tab, locked, onClose]);

  return <AlertDialog open onOpenChange={(open) => { if (!open && !locked) onClose(); }}>
    <AlertDialogContent className="panel-dialog panel-dialog-wide"
      onEscapeKeyDown={(event) => { if (locked) event.preventDefault(); }}
      onCloseAutoFocus={(event) => {
        event.preventDefault();
        if (opener.current?.isConnected && !opener.current.closest("[hidden]")) opener.current.focus();
      }}>
      <AlertDialogTitle>Let&apos;s get the clinic back</AlertDialogTitle>
      <AlertDialogDescription className="panel-dialog-description">
        Check what the clinic needs on this computer. Fix anything marked Not ready, then check again.
      </AlertDialogDescription>
      <div className="panel-dialog-scroll"><RequirementChecklist /></div>
      <AlertDialogFooter className="panel-dialog-foot">
        <PanelLogButton />
        <AlertDialogCancel disabled={locked} onClick={onClose}>Close</AlertDialogCancel>
        <AlertDialogAction disabled={locked || requirements.checking}
          onClick={(event) => {
            event.preventDefault();
            void requirements.recheck().then(() => refresh()).catch((error) => log(`diagnostic refresh: ${String(error)}`));
          }}>
          {requirements.checking ? <Spinner /> : <RefreshCw aria-hidden="true" />}
          {requirements.checking ? "Checking…" : "Check again"}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

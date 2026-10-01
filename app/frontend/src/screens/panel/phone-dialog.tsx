import { Copy, Wifi } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { useRef } from "react";

import {
  AlertDialog, AlertDialogAction, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { useCare } from "@/state/care-store";
import { usePanelTask } from "./panel-ui";

export function PhoneDialog({ onClose }: { onClose: () => void }) {
  const { mdnsName } = useCare();
  const task = usePanelTask();
  const opener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  const address = `http://${mdnsName}/setup`;
  const copy = async () => {
    if (await task.run(() => navigator.clipboard.writeText(address),
      "Couldn't copy the address. Select and copy the address above, or type it on the device.")) toast("Address copied");
  };
  return <AlertDialog open onOpenChange={(open) => { if (!open) onClose(); }}>
    <AlertDialogContent className="panel-dialog" onCloseAutoFocus={(event) => {
      event.preventDefault();
      opener.current?.focus();
    }}>
      <AlertDialogTitle>Connect a phone or tablet</AlertDialogTitle>
      <AlertDialogDescription className="panel-dialog-description">
        Point the camera at the code, or type the address by hand. Then follow the steps on the page.
      </AlertDialogDescription>
      <div className="panel-qr-row">
        <div className="panel-qr">
          <QRCodeSVG value={address} level="M" size={148} marginSize={4}
            role="img" aria-label={`QR code for ${address}`} />
        </div>
        <div className="panel-grow">
          <div className="panel-eyebrow">Or type</div>
          <span className="panel-qr-address">{address}</span>
          <ol className="panel-qr-steps">
            <li>Open the browser on the phone or tablet.</li>
            <li>Type the address above.</li>
            <li>Follow the steps shown on the page.</li>
          </ol>
          <p className="panel-qr-hint"><Wifi aria-hidden="true" />The device must be on the clinic Wi-Fi.</p>
        </div>
      </div>
      {task.error ? <p className="panel-inline-error" role="alert">{task.error}</p> : null}
      <AlertDialogFooter className="panel-dialog-foot">
        <Button disabled={task.working} onClick={() => void copy()}><Copy aria-hidden="true" />Copy address</Button>
        <span className="panel-grow" />
        <AlertDialogAction onClick={onClose}>Done</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

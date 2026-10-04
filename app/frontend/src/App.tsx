import { useCallback, useEffect, useRef, useState } from "react";

import { QuitDialog } from "@/components/quit-dialog";
import { ConfirmationDialog } from "@/components/confirmation-dialog";
import { RemovalProgress } from "@/components/removal-progress";
import { FailedScreen } from "@/screens/install/failed-screen";
import { InstallingScreen } from "@/screens/install/installing-screen";
import { PanelScreen } from "@/screens/panel/panel-screen";
import { SetupScreen } from "@/screens/setup/setup-screen";
import { RoleScreen } from "@/screens/role-screen";
import { ClientScreen } from "@/screens/client-screen";
import { RemoveScreen } from "@/screens/remove-screen";
import { useCare } from "@/state/care-store";
import { EMPTY_SETUP_FORM, type SetupForm } from "@/state/forms";

export function App() {
  const care = useCare();
  const removing = care.busy && (care.busyLabel === "Uninstalling" || care.busyLabel === "Removing CARE Desktop");
  // Keep choices across step navigation, but discard them when retry cleanup
  // invalidates the saved recovery material.
  const [setupForm, setSetupForm] = useState<SetupForm>(EMPTY_SETUP_FORM);
  const seeded = useRef(false);
  const lastReset = useRef(0);

  // The host already has a name for this clinic; adopt it as the field's value.
  useEffect(() => {
    if (!care.ready || seeded.current) return;
    seeded.current = true;
    setSetupForm((form) => ({
      ...form,
      hostInput: care.mdnsName.replace(/\.local$/i, ""),
    }));
  }, [care.ready, care.mdnsName]);

  useEffect(() => {
    if (care.setupReset === lastReset.current) return;
    lastReset.current = care.setupReset;
    setSetupForm({ ...EMPTY_SETUP_FORM, hostInput: care.mdnsName.replace(/\.local$/i, "") });
  }, [care.setupReset, care.mdnsName]);

  const patchSetup = useCallback(
    (values: Partial<SetupForm>) => setSetupForm((form) => ({ ...form, ...values })),
    [],
  );

  return (
    <div className="flex h-full">
      <ConfirmationDialog />
      <QuitDialog />
      <RemovalProgress />
      <div className="flex min-w-0 flex-1" inert={removing}>
      {care.ready ? (
        care.flow === "remove" ? (
          <RemoveScreen />
        ) : care.flow === "role" ? (
          <RoleScreen />
        ) : care.flow === "client" ? (
          <ClientScreen />
        ) : care.flow === "setup" ? (
          <SetupScreen form={setupForm} patch={patchSetup} />
        ) : care.flow === "installing" ? (
          <InstallingScreen />
        ) : care.flow === "failed" ? (
          <FailedScreen />
        ) : (
          <PanelScreen />
        )
      ) : (
        <div className="min-w-0 flex-1 bg-background" />
      )}
      </div>
    </div>
  );
}

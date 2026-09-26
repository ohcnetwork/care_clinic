import { useCallback, useEffect, useRef, useState } from "react";

import { Rail } from "@/components/rail";
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
  // The form lives here rather than in its screen, so bouncing off a failed
  // install keeps everything the operator already typed.
  const [setupForm, setSetupForm] = useState<SetupForm>(EMPTY_SETUP_FORM);
  const seeded = useRef(false);

  // The host already has a name for this clinic; adopt it as the field's value.
  useEffect(() => {
    if (!care.ready || seeded.current) return;
    seeded.current = true;
    setSetupForm((form) => ({
      ...form,
      hostInput: care.mdnsName.replace(/\.local$/i, ""),
    }));
  }, [care.ready, care.mdnsName]);

  const patchSetup = useCallback(
    (values: Partial<SetupForm>) => setSetupForm((form) => ({ ...form, ...values })),
    [],
  );

  return (
    <div className="flex h-full">
      {care.ready && care.flow !== "role" && care.flow !== "client" && care.flow !== "remove" ? (
        <Rail />
      ) : null}
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
  );
}

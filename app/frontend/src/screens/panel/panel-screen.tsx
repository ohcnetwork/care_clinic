import { ArrowDownToLine, ArrowUpRight, HardDrive, TriangleAlert, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Rail } from "@/components/rail";
import { Screen, ScreenBody } from "@/components/screen";
import { Button } from "@/components/ui/button";
import { useAppUpdate, type AppUpdateController } from "@/hooks/use-app-update";
import { bridge } from "@/lib/bridge";
import { useCare } from "@/state/care-store";
import { AdvancedTab } from "./advanced-tab";
import { BackupsTab } from "./backups-tab";
import { OverviewTab } from "./overview-tab";
import { RecoveryCodesBanner } from "./recovery-codes-banner";
import { PanelRequirementsProvider, usePanelRequirements } from "./panel-requirements";
import { storageProblem } from "./panel-status";
import { PanelLogButton, usePanelTask } from "./panel-ui";
import { PanelUpdateLockProvider } from "./panel-update-lock";
import { PluginTable } from "./plugin-table";
import { StorageTab } from "./storage-tab";
import { TroubleDialog } from "./trouble-dialog";
import { UpdatePanel } from "./update-panel";

import "./panel.css";

export function PanelScreen() {
  const care = useCare();
  const [requirementsWorking, setRequirementsWorking] = useState(false);
  const appUpdate = useAppUpdate(care.restorePending || requirementsWorking);
  return <PanelUpdateLockProvider active={appUpdate.active} isActive={appUpdate.isActive}>
    <PanelRequirementsProvider onWorkingChange={setRequirementsWorking}>
      <PanelContent appUpdate={appUpdate} />
    </PanelRequirementsProvider>
  </PanelUpdateLockProvider>;
}

function PanelContent({ appUpdate }: { appUpdate: AppUpdateController }) {
  const care = useCare();
  const requirements = usePanelRequirements();
  const task = usePanelTask();
  const [diagnosing, setDiagnosing] = useState(false);
  const panel = useRef<HTMLDivElement>(null);
  const pluginLayout = care.tab === "plugins";
  const trouble = care.trouble && !care.busy;
  const problem = storageProblem(care.storage);
  const locked = care.busy || requirements.working || appUpdate.active || task.working;
  const mutate = (run: () => Promise<unknown>, message: string) => {
    if (locked || appUpdate.isActive()) return;
    void task.run(run, message);
  };
  const updateCount = (care.careUpdate ? 1 : 0) + (appUpdate.update?.available && !appUpdate.active ? 1 : 0);

  useEffect(() => {
    if (care.tab === "backups") void care.reloadBackups();
  }, [care.tab, care.reloadBackups]);
  useLayoutEffect(() => {
    panel.current?.querySelector(".panel-tab-body")?.scrollTo({ top: 0 });
  }, [care.tab]);

  return <div className="care-panel" ref={panel}>
    <Rail variant="panel" locked={requirements.working} updateCount={updateCount} />
    <Screen className={pluginLayout ? undefined : "care-panel-main"}>
      {care.operationError ? <div className="panel-banner panel-tone-danger" role="alert">
        <TriangleAlert aria-hidden="true" />
        <div className="panel-grow"><strong>{care.operationError.title}. </strong>{care.operationError.message}</div>
        <PanelLogButton />
        <Button variant="ghost" size="icon" aria-label="Dismiss operation message" onClick={care.clearOperationError}><X aria-hidden="true" /></Button>
      </div> : appUpdate.active && care.tab !== "updates" ? <div className="panel-banner" role="status">
        <ArrowDownToLine aria-hidden="true" />
        <div className="panel-grow"><strong>CARE Clinic is updating. </strong>
          {appUpdate.progress?.phase === "installer" ? "Finish the installer, then acknowledge it in Updates."
            : "Wait until it finishes before making other changes."}</div>
        <Button size="sm" onClick={() => care.setTab("updates")}>View update</Button>
      </div> : trouble ? <div className="panel-banner panel-tone-danger" role="alert">
        <TriangleAlert aria-hidden="true" />
        <div className="panel-grow"><strong>Staff can&apos;t reach the clinic right now.</strong> Check what the clinic needs on this computer.</div>
        <Button variant="primary" size="sm" onClick={() => setDiagnosing(true)}>See what&apos;s wrong</Button>
      </div> : problem ? <div className={`panel-banner panel-tone-${problem.tone}`} role="status">
        <HardDrive aria-hidden="true" />
        <div className="panel-grow"><strong>{problem.title} </strong>{care.storageError
          ? "This is from the last successful check. Check Storage again for current information."
          : problem.detail}</div>
        {care.tab !== problem.tab ? <Button size="sm" onClick={() => care.setTab(problem.tab)}>See details</Button> : null}
      </div> : care.careUpdate && care.tab !== "updates" ? <div className="panel-banner" role="status">
        <ArrowDownToLine aria-hidden="true" />
        <div className="panel-grow"><strong>A CARE software update is ready to install. </strong>
          {care.careUpdate.backend ? "Staff will be signed out briefly." : "CARE will reload when it is applied."}</div>
        <div className="panel-actions">
          <Button size="sm" disabled={locked || care.restorePending} onClick={() => mutate(care.dismissCareUpdate,
            "Couldn't save this choice. Try again, or open the log file for support.")}>Later</Button>
          <Button size="sm" variant="primary" disabled={locked || care.restorePending} onClick={() => mutate(care.applyCareUpdate,
            "Couldn't start the CARE update. Try again, or open the log file for support.")}>Install now</Button>
        </div>
      </div> : null}
      {task.error && !care.operationError ? <div className="panel-banner panel-tone-danger" role="alert">
        <span className="panel-grow">{task.error}</span><PanelLogButton />
      </div> : null}
      <RecoveryCodesBanner disabled={locked || care.restorePending} />
      {pluginLayout ? <div className="flex items-center gap-4 px-[34px] pt-[26px] pb-[18px]">
        <div className="min-w-0 flex-1">
          <h1 className="text-[23px] font-bold tracking-[-0.015em] text-ink">Plugins</h1>
          <p className="mt-[5px] text-[13.5px] text-muted-foreground">Manage extra features for your clinic.</p>
        </div>
        <Button variant="soft" onClick={() => void task.run(() => bridge.OpenURL(`https://${care.mdnsName}/`),
          "Couldn't open CARE in your browser. Try again.")}>
          <span className="font-mono">{care.mdnsName}</span>
          <ArrowUpRight className="size-3.5" strokeWidth={2.2} />
        </Button>
      </div> : null}
      <ScreenBody className={pluginLayout ? "panel-tab-body" : "panel-tab-body care-panel-content"}>
        {/* Keep component instances stable; each tab controls its own deactivation. */}
        <div hidden={care.tab !== "overview"}><OverviewTab onDiagnose={() => setDiagnosing(true)} /></div>
        <div hidden={care.tab !== "backups"}><BackupsTab /></div>
        <div hidden={care.tab !== "storage"}><StorageTab /></div>
        <div data-panel-tab="plugins" hidden={care.tab !== "plugins"}><PluginTable disabled={locked} /></div>
        <div hidden={care.tab !== "updates"}><UpdatePanel appUpdate={appUpdate} /></div>
        <div data-panel-tab="advanced" hidden={care.tab !== "advanced"}><AdvancedTab disabled={locked} /></div>
      </ScreenBody>
    </Screen>
    {diagnosing ? <TroubleDialog onClose={() => setDiagnosing(false)} /> : null}
  </div>;
}

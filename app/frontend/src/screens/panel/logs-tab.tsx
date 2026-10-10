import { FolderOpen, ScrollText } from "lucide-react";
import { useEffect, useState } from "react";

import { bridge } from "@/lib/bridge";
import { useCare } from "@/state/care-store";
import { PanelLogButton, PanelNotice, PanelPageHeader, usePanelTask } from "./panel-ui";

const files = [
  ["care-log.log", "Installation, updates, backups and app errors"],
  ["app-update-helper.log", "Desktop update installation and relaunch"],
  ["engine-host.log", "VM startup, shutdown and connection failures"],
  ["engine-console.log", "VM boot output and kernel messages"],
  ["guest-system.log", "Linux services and Docker daemon"],
  ["containers/*.log", "CARE service logs, separated by container name"],
  ["collector.log", "Collection gaps, reconnects and retention errors"],
];

export function LogsTab() {
  const care = useCare();
  const task = usePanelTask();
  const [path, setPath] = useState("");
  useEffect(() => {
    if (care.tab !== "logs") return;
    let active = true;
    void task.run(async () => {
      const file = await bridge.LogPath();
      if (active) setPath(file.replace(/[\\/][^\\/]+$/, ""));
    }, "Couldn't locate the logs folder. Try opening it below.");
    return () => { active = false; };
  }, [care.tab]);
  return <div className="panel-page" aria-label="Logs">
    <PanelPageHeader title="Logs" subtitle="Local diagnostics for installation, updates and running CARE." />
    <section className="panel-card">
      <h2 className="panel-card-title"><FolderOpen aria-hidden="true" />All local logs in one folder</h2>
      {path ? <p className="panel-small break-all font-mono">{path}</p> : null}
      <p className="panel-small">Engine and container logs are collected while the CARE engine is running, even when this window is closed.</p>
      <PanelLogButton />
      {task.error ? <p className="panel-inline-error" role="alert">{task.error}</p> : null}
    </section>
    <section className="panel-card">
      <h2 className="panel-card-title"><ScrollText aria-hidden="true" />What is recorded</h2>
      <dl>
        {files.map(([name, detail]) => <div key={name} className="mb-3">
          <dt className="font-mono text-sm">{name}</dt>
          <dd className="panel-small">{detail}</dd>
        </div>)}
      </dl>
      <p className="panel-small">Logs rotate at 10 MB. Retention removes logs older than seven days and limits the folder to approximately 500 MB. Files appear as their services produce logs.</p>
    </section>
    <PanelNotice title="Review diagnostics before sharing" tone="warning">
      Logs may contain patient information, URLs or secrets printed by services. Export saves a ZIP locally; nothing is uploaded automatically.
      Hard crashes or disk problems can lose the final messages. Check collector.log for detected gaps.
    </PanelNotice>
  </div>;
}

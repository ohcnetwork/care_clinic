// The shapes the Go bridge hands back. Mirrors app/internal/care — keep in step
// with wailsjs/go/models.ts, which Wails regenerates from the Go structs.

export type DockerStatus = { ok: boolean; message: string };
export type NameStatus = { ok: boolean; message: string };
export type NetworkStatus = {
  applicable: boolean;
  ok: boolean;
  message: string;
  how: string;
  fixable: boolean;
};
export type WSLStatus = NetworkStatus;
export type Health = { active: boolean; code: number; detail: string };

/** Whether this machine must restart before the prerequisites will work. */
export type RestartPlan = {
  needed: boolean;
  title: string;
  detail: string;
  label: string;
};

/** What the app can do about a prerequisite that isn't ready on this machine. */
export type ToolAction = "" | "install" | "open" | "manual";
export type ToolPlan = {
  action: ToolAction;
  label: string;
  detail: string;
  url: string;
};
export type AppState = {
  role: "" | "server" | "client";
  client_url: string;
  version: string;
  setup_done: boolean;
  mdns_name: string;
  docker: DockerStatus;
  restore_pending: boolean;
};

/** One thing an earlier CARE Desktop left on this computer. */
export type ResidueTrace = { id: string; label: string; detail: string };

/** What ScanResidue found. `clean` is what the wizard gates on. */
export type ResidueReport = { clean: boolean; traces: ResidueTrace[] };

export type Backup = {
  db_dump: string;
  files_archive: string;
  label: string;
  manual: boolean;
  encrypted: boolean;
  size_bytes: number;
};

/** A backup file the operator picked from outside the clinic's backup folder. */
export type ImportedBackup = {
  path: string;
  dir: string;
  db_dump: string;
  files_archive: string;
  label: string;
  encrypted: boolean;
  has_key: boolean;
};

export type PluginBackend = {
  name: string;
  package_name: string;
  version?: string;
  configs?: Record<string, unknown>;
};

export type PluginFrontend = {
  slug: string;
  url: string;
  meta?: Record<string, unknown>;
};

export type CarePlugin = {
  id: string;
  label?: string;
  catalog?: boolean;
  backend?: PluginBackend;
  frontend?: PluginFrontend;
};

export type PluginCatalogEntry = {
  plugin: CarePlugin;
  description?: string;
};

/** Which of the two .env files an editor is pointed at. */
export type Section = "backend" | "frontend";

export type ChannelStatus = {
  backend_branch: string;
  frontend_branch: string;
  backend: string;
  frontend: string;
  pending_backend: string;
  pending_frontend: string;
};

export type CareUpdate = { backend: string; frontend: string };

export type CareCheck = { running: boolean; found: boolean };

export type StorageLevel = "ok" | "low" | "critical" | "unknown";

export type DiskStatus = {
  ok: boolean;
  message: string;
  how: string;
  free: number;
  need: number;
};

export type StorageDrive = {
  id: "docker" | "vm";
  label: string;
  path: string;
  free: number;
  total: number;
  level: StorageLevel;
  message: string;
  cleanable: boolean;
};

export type BackupSpace = {
  dir: string;
  free: number;
  total: number;
  need: number;
  set_bytes: number;
  days_left: number;
  shares_docker_drive: boolean;
  level: StorageLevel;
  message: string;
};

export type BackupRun = {
  state: "" | "ok" | "failed" | "running";
  reason: "" | "disk_full" | "error";
  at: number;
  need_bytes: number;
  free_bytes: number;
  message: string;
};

export type StorageReport = {
  checked_at: number;
  level: StorageLevel;
  headline: string;
  drives: StorageDrive[] | null;
  backup: BackupSpace;
  last_run: BackupRun;
  newest_backup_at: number;
  stale: boolean;
};

export type AppUpdate = {
  current: string;
  version: string;
  available: boolean;
  notes_url: string;
  asset: string;
  size: number;
};

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
  download_preview: boolean;
};

export type DownloadInfo = { name: string; size: number };
export type PrereqDownloadProgress = {
  name: string;
  phase: "connecting" | "downloading" | "verifying" | "complete" | "failed";
  done: number;
  total: number;
};
export type AppState = {
  role: "" | "server" | "client";
  client_url: string;
  version: string;
  platform: string;
  setup_done: boolean;
  mdns_name: string;
  docker: DockerStatus;
  restore_pending: boolean;
};

export type ClientConnectPhase = "finding" | "connecting" | "checking" | "opening";

export type SetupPage =
  | "space" | "windows" | "software" | "cleanup" | "network"
  | "address" | "backup" | "admin" | "review" | "install";
export type SetupIssue = { step: SetupPage; message: string };
export type SetupRecoveryStatus = {
  backup_saved: boolean;
  backup_verified: boolean;
  codes_saved: boolean;
  backup_path: string;
  codes_path: string;
  backup_problem: string;
  codes_problem: string;
};

/** One thing an earlier CARE Desktop left on this computer. */
export type ResidueTrace = { id: string; label: string; detail: string };

/** What ScanResidue found. `clean` is what the wizard gates on. */
export type ResidueReport = { clean: boolean; traces: ResidueTrace[] };

/**
 * What FindClinic found. Reading this changes nothing on the computer;
 * `already_trusted` means connecting needs no certificate installation.
 */
export type ClinicInfo = {
  url: string;
  host: string;
  fingerprint: string;
  already_trusted: boolean;
};

/**
 * Whether the clinic is answering. A clinic that is switched off is
 * `reachable: false` with a `detail`, not an error.
 */
export type ClientReachability = {
  reachable: boolean;
  checked_at: number;
  detail: string;
};

/** What connecting would clean up. Read-only; no administrator approval. */
export type ClientPreflight = {
  hosts_entry: boolean;
  old_certificate: boolean;
  unfinished_server_setup: boolean;
  engine_leftovers: string;
};

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
  default?: boolean;
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

export type CareCheck = { running: boolean; found: boolean; error?: string };

export type BackupPolicy = { interval_seconds: number; retention_days: number };
export type QuitRequest = { id: number; title: string; message: string };
export type ConfirmationRequest = { id: number; title: string; message: string };

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
  reason: "" | "disk_full" | "database_unavailable" | "error";
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

export type AppUpdatePhase = "downloading" | "verifying" | "installing" | "restarting" | "installer";

export type AppUpdateProgress = { phase: AppUpdatePhase; done: number; total: number };

export type AppUpdate = {
  current: string;
  version: string;
  available: boolean;
  notes_url: string;
  asset: string;
  size: number;
};

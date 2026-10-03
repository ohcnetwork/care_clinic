import type {
  AppState, AppUpdate, AppUpdateProgress, Backup, BackupPolicy, BackupSpace, ChannelStatus, ClientPreflight,
  ClientReachability, ConfirmationRequest, DiskStatus, DockerStatus, NetworkStatus, ResidueReport,
  CarePlugin, Health, ImportedBackup, PluginCatalogEntry, QuitRequest, RestartPlan, Section, SetupIssue, SetupRecoveryStatus, StorageReport, ToolPlan,
} from "../../src/types";

type Host = Window["go"]["main"]["App"];
type Method = keyof Host;
type Call = { method: Method; args: unknown[] };
type Handler = (...data: unknown[]) => void;
type Results = { [K in Method]: Awaited<ReturnType<Host[K]>> };
type Fixtures = {
  disk: DiskStatus;
  docker: DockerStatus;
  git: DockerStatus;
  wsl: NetworkStatus;
  network: NetworkStatus;
  restart: RestartPlan;
  residue: ResidueReport;
  recovery: SetupRecoveryStatus;
  preflight: ClientPreflight;
  reachability: ClientReachability;
  folder: string;
  folderProblem: string;
  addressTaken: boolean;
  health: Health;
  clinicStatus: string;
  backups: Backup[];
  backupDir: string;
  backupPolicy: BackupPolicy;
  storage: StorageReport;
  channel: ChannelStatus;
  autostart: boolean;
  importedBackup: ImportedBackup;
  recoveryFile: string;
  adminPassword: string;
  finishJobs: boolean;
  quitAccepted: boolean;
  env: Record<Section, string>;
  plugins: CarePlugin[];
  catalog: PluginCatalogEntry[];
  recoveryCodes: string[];
  recoveryGeneration: number;
  recoveryFailures: number;
  recoveryRetryAfter: number;
  canRemoveApp: boolean;
  rancherInstalled: boolean;
  appRemoved: boolean;
  setupStarted: boolean;
};

declare global {
  interface Window {
    careTest: {
      calls: Call[];
      logs: string[];
      state: AppState;
      fixtures: Fixtures;
      respond: <K extends Method>(method: K, value: Results[K]) => void;
      emit: (event: string, ...data: unknown[]) => void;
      failNext: (method: Method, error: string) => void;
      hold: (method: Method) => void;
      release: (method: Method) => void;
      progress: (value: AppUpdateProgress) => void;
      finishUpdate: (error?: string) => void;
      setUpdate: (value: AppUpdate) => void;
      finishJob: (action: string, error?: string) => void;
      requestQuit: (action: string) => void;
      requestConfirmation: (title: string, message: string) => void;
      cancelConfirmation: () => void;
    };
  }
}

export function installTestHost() {
  if (!import.meta.env.DEV || import.meta.env.MODE !== "test") {
    throw new Error("The simulated host is only available to automated tests.");
  }
  const scenario = new URLSearchParams(location.search).get("scenario") || "current";
  const savedRole = new URLSearchParams(location.search).get("role");
  const state: AppState = {
    role: savedRole === "client" || savedRole === "server" ? savedRole : "",
    client_url: "",
    version: "0.1.5",
    platform: new URLSearchParams(location.search).get("platform") || "darwin",
    setup_done: false,
    mdns_name: "",
    docker: { ok: true, message: "" },
    restore_pending: false,
  };
  const calls: Call[] = [];
  const logs: string[] = [];
  const listeners = new Map<string, Set<Handler>>();
  const failures = new Map<Method, string>();
  const responses = new Map<Method, unknown>();
  const holds = new Map<Method, { wait: Promise<void>; release: () => void }>();
  let quitRequest: QuitRequest | null = null;
  let quitSequence = 0;
  let confirmation: ConfirmationRequest | null = null;
  let confirmationSequence = 0;
  const now = Math.floor(Date.now() / 1000);
  const exampleBackup: Backup = {
    db_dump: "care-20261003-020004.dump.enc", files_archive: "files-20261003-020004.tar.gz.enc",
    label: "20261003-020004 - database + files - encrypted", encrypted: true, manual: false, size_bytes: 2.1 * 2 ** 30,
  };
  const backupDir = "/test-fixtures/CLINIC-BACKUP/care-db-backups";
  const catalog: PluginCatalogEntry[] = [
    { plugin: { id: "preview-appointments", label: "Appointments (preview)", frontend: { slug: "appointments", url: "https://example.invalid/appointments/remoteEntry.js" } }, description: "A simulated catalog entry. No plugin code is downloaded.", default: false },
    { plugin: { id: "preview-reports", label: "Reports (preview)", backend: { name: "care_reports", package_name: "care_reports", version: "==1.0.0" } }, description: "A simulated server plugin. Nothing is installed.", default: false },
  ];
  const sampleCodes = (generation: number) => Array.from({ length: 6 }, (_, index) =>
    `AAAA-BBBB-CCCC-DDDD-EEEE-FFFF-${generation.toString(16).padStart(4, "0")}-${(index + 1).toString(16).padStart(4, "0")}`.toUpperCase());
  const backupSpace: BackupSpace = {
    dir: backupDir, free: 418 * 2 ** 30, total: 500 * 2 ** 30,
    need: 6 * 2 ** 30, set_bytes: exampleBackup.size_bytes, days_left: 190,
    shares_docker_drive: false, level: "ok", message: "",
  };
  const fixtures: Fixtures = {
    disk: { ok: true, message: "212 GB free", how: "", free: 212 * 2 ** 30, need: 30 * 2 ** 30 },
    docker: { ok: true, message: "" },
    git: { ok: true, message: "" },
    wsl: { applicable: state.platform === "windows", ok: true, message: "", how: "", fixable: true },
    network: { applicable: state.platform === "windows", ok: true, message: "", how: "", fixable: true },
    restart: { needed: false, title: "Restart Windows to finish", detail: "Save anything you have open. CARE Desktop opens again afterwards.", label: "Restart now" },
    residue: { clean: true, traces: [] },
    recovery: { backup_saved: false, backup_verified: false, codes_saved: false, backup_path: "", codes_path: "", backup_problem: "", codes_problem: "" },
    preflight: { hosts_entry: false, old_certificate: false, unfinished_server_setup: false, engine_leftovers: "" },
    reachability: { reachable: true, checked_at: 0, detail: "" },
    folder: "/test-fixtures/CLINIC-BACKUP",
    folderProblem: "",
    addressTaken: false,
    health: { active: true, code: 200, detail: "" },
    clinicStatus: "backend\nfrontend\ndb\nbackup",
    backups: [exampleBackup, { ...exampleBackup, db_dump: "care-20261002-020004.dump.enc", files_archive: "files-20261002-020004.tar.gz.enc", label: "20261002-020004 - database + files - encrypted" }],
    backupDir,
    backupPolicy: { interval_seconds: 86_400, retention_days: 0 },
    storage: {
      checked_at: now, level: "ok", headline: "",
      drives: [
        { id: "docker", label: "This computer's drive", path: "/preview", free: 212 * 2 ** 30, total: 500 * 2 ** 30, level: "ok", message: "", cleanable: false },
        { id: "vm", label: "Rancher Desktop disk", path: "", free: 63 * 2 ** 30, total: 100 * 2 ** 30, level: "ok", message: "", cleanable: true },
      ],
      backup: backupSpace, last_run: { state: "ok", reason: "", at: now - 3_600, need_bytes: 0, free_bytes: 0, message: "" },
      newest_backup_at: now - 3_600, stale: false,
    },
    channel: { backend_branch: "main", frontend_branch: "main", backend: "preview-backend-current", frontend: "preview-frontend-current", pending_backend: "", pending_frontend: "" },
    autostart: true,
    importedBackup: { path: `${backupDir}/${exampleBackup.db_dump}`, dir: backupDir, db_dump: exampleBackup.db_dump, files_archive: exampleBackup.files_archive, label: exampleBackup.label, encrypted: true },
    recoveryFile: "/test-fixtures/recovery/CARE-backup-recovery.pem",
    adminPassword: "ClinicTest123",
    finishJobs: true,
    quitAccepted: false,
    env: {
      backend: "DB_BACKUP_RETENTION_PERIOD=0\nTIME_ZONE=Asia/Kolkata\nCORAZA_MODE=Off\nDEFAULT_FROM_EMAIL=clinic@example.invalid\nENABLE_OTP_LOGIN=False\nALLOW_PUBLIC_READ=False\n",
      frontend: "REACT_CARE_API_URL=https://care.local\nREACT_CARE_ENABLE_DASHBOARD=true\n",
    },
    plugins: [{ ...catalog[0].plugin, catalog: true }],
    catalog,
    recoveryCodes: sampleCodes(1),
    recoveryGeneration: 1,
    recoveryFailures: 0,
    recoveryRetryAfter: 0,
    canRemoveApp: new URLSearchParams(location.search).get("removable") === "1",
    rancherInstalled: true,
    appRemoved: false,
    setupStarted: false,
  };
  if (scenario.startsWith("setup-")) state.role = "server";
  if (scenario === "setup-space") fixtures.disk = { ...fixtures.disk, ok: false, free: 12 * 2 ** 30 };
  if (scenario === "setup-windows") {
    state.platform = "windows";
    fixtures.wsl.applicable = true;
    fixtures.wsl.ok = false;
    fixtures.network.applicable = true;
    fixtures.network.ok = false;
  }
  if (scenario === "setup-software") { fixtures.docker.ok = false; fixtures.git.ok = false; }
  if (scenario === "setup-cleanup") fixtures.residue = { clean: false, traces: [
    { id: "containers", label: "Containers and volumes from the earlier clinic", detail: "" },
    { id: "images", label: "Images Rancher Desktop downloaded", detail: "" },
    { id: "config", label: "Saved settings and configuration files", detail: "" },
    { id: "hosts", label: "The old clinic address and its certificate", detail: "" },
  ] };
  if (scenario === "setup-address") fixtures.addressTaken = true;
  if (scenario.startsWith("client-")) state.role = "client";
  if (scenario === "client-saved" || scenario === "client-offline") {
    state.client_url = "https://care.local";
    if (scenario === "client-offline") { fixtures.reachability.reachable = false; fixtures.reachability.detail = "the server did not answer"; }
  }
  if (scenario.startsWith("panel-") || new URLSearchParams(location.search).get("screen") === "panel") {
    state.role = "server";
    state.setup_done = true;
    state.mdns_name = "care.local";
  }
  if (new URLSearchParams(location.search).get("screen") === "remove") {
    state.role = "server";
    state.setup_done = true;
    state.mdns_name = "care.local";
  }
  if (scenario === "panel-no-backups") {
    fixtures.backups = [];
    fixtures.storage.last_run.state = "";
    fixtures.storage.newest_backup_at = 0;
  }
  if (scenario === "panel-backup-failed") {
    fixtures.storage.last_run = { ...fixtures.storage.last_run, state: "failed", reason: "disk_full" };
    fixtures.storage.backup = { ...backupSpace, level: "critical", free: 200e6 };
    fixtures.storage.level = "critical";
    fixtures.storage.headline = "The backup location is running out of space.";
  }
  if (scenario === "panel-storage-low") {
    fixtures.storage.drives![1] = { ...fixtures.storage.drives![1], free: 3 * 2 ** 30, level: "critical" };
    fixtures.storage.level = "critical";
    fixtures.storage.headline = "Space for the clinic is running low.";
  }
  if (scenario === "panel-requirements") { fixtures.docker.ok = false; fixtures.git.ok = false; }
  if (scenario === "panel-update") fixtures.channel.pending_backend = "preview-backend-next";
  const available = ["available", "downloading", "installing", "verifying", "restarting", "installer", "failed", "download-failed", "unavailable"].includes(scenario);
  let update: AppUpdate = {
    current: "0.1.5",
    version: available ? "0.1.6" : "0.1.5",
    available,
    notes_url: "https://github.com/ohcnetwork/care_desktop/releases",
    asset: "CARE-Desktop-preview.dmg",
    size: 50_000_000,
  };
  let updateRunning = false;

  const emit = (event: string, ...data: unknown[]) => {
    listeners.get(event)?.forEach((handler) => handler(...data));
  };
  const finishJob = (action: string, error?: string) => {
    if (error) {
      emit("care-log", `error: ${error}`);
      const title = action === "restore" ? "Restore didn't finish" : action === "backup-now" ? "Backup didn't finish" : "CARE couldn't finish that";
      if (action !== "setup") emit("care-error", title, error);
    } else {
      if (["start", "restart", "update", "restore"].includes(action)) {
        fixtures.health = { active: true, code: 200, detail: "" };
        fixtures.clinicStatus = "backend\nfrontend\ndb\nbackup";
      }
      if (action === "stop") { fixtures.health.active = false; fixtures.clinicStatus = ""; }
      if (action === "restore") state.restore_pending = false;
      if (action === "update") {
        fixtures.channel.backend = fixtures.channel.pending_backend || fixtures.channel.backend;
        fixtures.channel.frontend = fixtures.channel.pending_frontend || fixtures.channel.frontend;
        fixtures.channel.pending_backend = "";
        fixtures.channel.pending_frontend = "";
      }
      if (action === "backup-now") {
        const stamp = new Date().toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
        fixtures.backups.unshift({ ...exampleBackup, db_dump: `care-manual-${stamp}.dump.enc`, files_archive: `files-manual-${stamp}.tar.gz.enc`, label: `${stamp} - database + files - encrypted`, manual: true });
        fixtures.storage.last_run = { state: "ok", reason: "", at: Math.floor(Date.now() / 1000), need_bytes: 0, free_bytes: 0, message: "" };
        fixtures.storage.newest_backup_at = fixtures.storage.last_run.at;
        fixtures.storage.stale = false;
      }
      if (action === "free-space") {
        fixtures.storage.drives = fixtures.storage.drives?.map((drive) => drive.cleanable ? { ...drive, free: Math.min(drive.total, drive.free + 2 ** 30) } : drive) ?? null;
      }
      if (action === "uninstall") {
        state.role = "";
        state.setup_done = false;
        state.mdns_name = "";
        state.client_url = "";
        fixtures.setupStarted = false;
        fixtures.recovery = { backup_saved: false, backup_verified: false, codes_saved: false, backup_path: "", codes_path: "", backup_problem: "", codes_problem: "" };
        emit("uninstalled", true);
      }
      if (action === "setup") {
        state.setup_done = true;
        fixtures.health = { active: true, code: 200, detail: "" };
        fixtures.clinicStatus = "backend\nfrontend\ndb\nbackup";
        emit("setup-done", true);
      }
    }
    emit("care-done", error ? 1 : 0, action);
  };
  const acceptJob = (action: string) => {
    if (fixtures.finishJobs) window.setTimeout(() => finishJob(action), 350);
  };
  const finishUpdate = (error?: string) => {
    if (error) {
      emit("care-log", `error: ${error}`);
      emit("care-error", "The CARE Desktop update didn't finish", error);
    }
    updateRunning = false;
    emit("care-done", error ? 1 : 0, "app-update");
  };
  const requireServer = () => {
    if (state.role !== "server") throw new Error("This operation requires the server role.");
  };
  const wait = (ms: number) => new Promise<void>((resolve) => window.setTimeout(resolve, ms));
  const softwarePlan = (id: "docker" | "git"): ToolPlan => ({
    action: "install", label: `Install ${id === "docker" ? "Rancher Desktop" : "Git"}`,
    detail: "Simulated installation only.", url: "", download_preview: id === "docker",
  });
  const installSoftware = async (id: "docker" | "git") => {
    requireServer();
    for (const phase of ["connecting", "downloading", "verifying", "complete"] as const) {
      emit("prereq-download-progress", { name: id, phase, done: phase === "downloading" ? 263e6 : 612e6, total: 612e6 });
      await wait(150);
    }
    fixtures[id].ok = true;
    return "Test only: the software is now ready.";
  };
  const methods = {
    GetState: async () => ({ ...state }),
    UninstallRequested: async () => new URLSearchParams(location.search).get("screen") === "remove",
    BeginServerSetup: async () => {
      if (state.role === "client") throw new Error("this computer is already a client; its role cannot be changed");
      state.role = "server";
    },
    ClearRole: async () => {
      if (state.setup_done || state.client_url || fixtures.setupStarted) throw new Error("Uninstall the current setup first.");
      state.role = "";
      state.mdns_name = "";
      fixtures.recovery = { backup_saved: false, backup_verified: false, codes_saved: false, backup_path: "", codes_path: "", backup_problem: "", codes_problem: "" };
    },
    CanRemoveApp: async () => fixtures.canRemoveApp,
    ClientPreflight: async () => ({ ...fixtures.preflight }),
    FindClinic: async (address: string) => {
      if (fixtures.preflight.unfinished_server_setup) throw new Error("this computer has an unfinished clinic setup; remove it in Setup before connecting");
      if (scenario === "client-not-found") throw new Error("could not reach the clinic; check its address, network and main computer");
      const name = address.replace(/^https?:\/\//, "").replace(/\/$/, "");
      return { url: `https://${name}`, host: name, fingerprint: "PREVIEW", already_trusted: false };
    },
    ConnectClient: async (address: string) => {
      if (fixtures.preflight.unfinished_server_setup) throw new Error("this computer has an unfinished clinic setup; remove it in Setup before connecting");
      state.role = "client";
      state.client_url = address.startsWith("https://") ? address : `https://${address}`;
      for (const phase of ["finding", "connecting", "checking", "opening"]) {
        emit("client-connect-progress", phase);
        await wait(150);
      }
      logs.push("Test only: the clinic browser would open.");
    },
    ClientReachable: async () => ({ ...fixtures.reachability, checked_at: Math.floor(Date.now() / 1000) }),
    DisconnectClient: async () => { state.role = ""; state.client_url = ""; },
    SetMDNSName: async (name: string) => {
      requireServer();
      state.mdns_name = name;
    },
    GetSetupRecoveryStatus: async () => {
      requireServer();
      return { ...fixtures.recovery };
    },
    BackupDirSpace: async (dir: string): Promise<BackupSpace> => {
      requireServer();
      return {
        dir: `${dir || "/test-fixtures/Desktop"}/care-db-backups`, free: 418 * 2 ** 30, total: 500 * 2 ** 30, need: 6 * 2 ** 30,
        set_bytes: 0, days_left: -1, shares_docker_drive: false, level: "ok", message: "",
      };
    },
    DockerStatus: async () => ({ ...fixtures.docker }),
    GitStatus: async () => ({ ...fixtures.git }),
    WSLStatus: async () => ({ ...fixtures.wsl }),
    NetworkStatus: async () => ({ ...fixtures.network }),
    DiskStatus: async () => ({ ...fixtures.disk }),
    MDNSStatus: async () => ({ ok: !fixtures.addressTaken, message: fixtures.addressTaken ? "care.local is already in use on this network (192.0.2.1)" : "" }),
    ScanResidue: async () => ({ ...fixtures.residue }),
    ValidateDomain: async (name: string) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.local)?$/i.test(name) ? "" : "Invalid clinic name",
    ValidatePassword: async (password: string) => [...password].length < 8 ? "Password must be at least 8 characters."
      : [...password].length > 20 ? "Password must be 20 characters or fewer."
      : !/\p{Lu}/u.test(password) || !/\p{Ll}/u.test(password) || !/\p{Nd}/u.test(password) ? "Password must include an uppercase letter, a lowercase letter, and a number." : "",
    ValidateBackupDir: async () => fixtures.folderProblem,
    ChooseFolder: async () => fixtures.folder,
    SaveSetupBackupRecovery: async () => {
      requireServer();
      fixtures.recovery.backup_saved = true;
      fixtures.recovery.backup_verified = false;
      fixtures.recovery.backup_path = "/test-fixtures/recovery/CARE-backup-recovery.pem";
      fixtures.recovery.backup_problem = "";
      return true;
    },
    ReplaceSetupBackupRecovery: async () => {
      requireServer();
      fixtures.recovery.backup_saved = true;
      fixtures.recovery.backup_verified = false;
      fixtures.recovery.backup_path = "/test-fixtures/recovery/CARE-backup-recovery-new.pem";
      fixtures.recovery.backup_problem = "";
      return true;
    },
    VerifySetupBackupRecovery: async () => { requireServer(); fixtures.recovery.backup_verified = true; fixtures.recovery.backup_problem = ""; return true; },
    SaveAdminRecoveryCodes: async (password: string) => {
      requireServer();
      if (state.setup_done && password !== fixtures.adminPassword) throw new Error("the Desktop admin password does not match this installation");
      fixtures.recovery.codes_saved = true;
      fixtures.recovery.codes_path = "/test-fixtures/recovery/CARE-desktop-admin-codes.txt";
      fixtures.recovery.codes_problem = "";
      fixtures.recoveryCodes = sampleCodes(++fixtures.recoveryGeneration);
      fixtures.recoveryFailures = 0;
      fixtures.recoveryRetryAfter = 0;
      return true;
    },
    OpenSetupRecoveryCodes: async () => { logs.push("Test only: the codes would open for printing."); },
    OpenSetupRecoveryFolder: async () => { logs.push("Test only: the recovery folder would open."); },
    ClinicHealth: async () => ({ ...fixtures.health }),
    ClinicStatus: async () => fixtures.clinicStatus,
    ListBackups: async () => fixtures.backups.map((backup) => ({ ...backup })),
    GetBackupDir: async () => fixtures.backupDir,
    GetBackupPolicy: async () => ({ ...fixtures.backupPolicy }),
    SetBackupDir: async (dir: string) => {
      requireServer();
      if (fixtures.folderProblem) throw new Error(fixtures.folderProblem);
      fixtures.backupDir = `${dir}/care-db-backups`;
      fixtures.storage.backup.dir = fixtures.backupDir;
      return fixtures.backupDir;
    },
    StorageStatus: async () => structuredClone(fixtures.storage),
    RecheckStorage: async () => {
      fixtures.storage.checked_at = Math.floor(Date.now() / 1000);
      return structuredClone(fixtures.storage);
    },
    AutostartEnabled: async () => fixtures.autostart,
    SetAutostart: async (value: boolean) => { fixtures.autostart = value; },
    WasAutostartLaunched: async () => false,
    CareUpdateStatus: async () => ({ ...fixtures.channel }),
    CheckCareUpdate: async () => {
      emit("care-check", { running: true, found: false });
      window.setTimeout(() => {
        const update = { backend: fixtures.channel.pending_backend, frontend: fixtures.channel.pending_frontend };
        emit("care-check", { running: false, found: !!(update.backend || update.frontend) });
        if (update.backend || update.frontend) emit("care-update", update);
      }, 150);
    },
    DismissCareUpdate: async () => { logs.push("Test only: the update was deferred until next start."); },
    ClinicAction: async (action: string) => {
      requireServer();
      if (state.restore_pending && !["start", "stop"].includes(action)) throw new Error("a restore is unfinished; start CARE to recover it before making other changes");
      if (action === "backup-now") fixtures.storage.last_run.state = "running";
      acceptJob(action);
    },
    VerifyAdminPassword: async (password: string) => password === fixtures.adminPassword,
    ChooseRecoveryFile: async () => fixtures.recoveryFile,
    ChooseBackupFile: async () => fixtures.importedBackup.path,
    InspectBackupFile: async (path: string) => {
      if (path !== fixtures.importedBackup.path) throw new Error("couldn't open that file");
      return { ...fixtures.importedBackup };
    },
    RestoreFromFile: async (path: string, recoveryFile: string, password: string) => {
      requireServer();
      if (password !== fixtures.adminPassword) throw new Error("the Desktop admin password does not match this installation");
      if (path !== fixtures.importedBackup.path) throw new Error("couldn't open that file");
      if (fixtures.importedBackup.encrypted && (!recoveryFile || recoveryFile !== fixtures.recoveryFile)) throw new Error("the recovery file does not match this backup");
      acceptJob("restore");
    },
    ReadEnv: async (section: string, password: string) => {
      if (password !== fixtures.adminPassword) throw new Error("the Desktop admin password does not match this installation");
      if (section !== "backend" && section !== "frontend") throw new Error("unknown env file");
      return fixtures.env[section];
    },
    WriteEnv: async (section: string, content: string, password: string) => {
      requireServer();
      if (password !== fixtures.adminPassword) throw new Error("the Desktop admin password does not match this installation");
      if (state.restore_pending) throw new Error("a restore is unfinished; start CARE to recover it before making other changes");
      if (section !== "backend" && section !== "frontend") throw new Error("unknown env file");
      fixtures.env[section] = content;
      if (section === "backend") {
        const retention = /^DB_BACKUP_RETENTION_PERIOD=["']?(\d+)["']?\s*$/m.exec(content);
        if (retention) fixtures.backupPolicy.retention_days = Number(retention[1]);
      }
    },
    ReadPlugins: async () => structuredClone(fixtures.plugins),
    PluginCatalog: async () => structuredClone(fixtures.catalog),
    SavePlugins: async (plugins: CarePlugin[]) => {
      requireServer();
      if (state.restore_pending) throw new Error("a restore is unfinished; start CARE to recover it before making other changes");
      if (new Set(plugins.map((plugin) => plugin.id)).size !== plugins.length) throw new Error("duplicate plugin id");
      if (plugins.some((plugin) => !plugin.id || (!plugin.frontend && !plugin.backend))) throw new Error("each plugin needs an id and a frontend or backend");
      fixtures.plugins = structuredClone(plugins);
    },
    ChangeAdminPassword: async (current: string, next: string) => {
      requireServer();
      if (current !== fixtures.adminPassword) throw new Error("the Desktop admin password does not match this installation");
      const problem = await methods.ValidatePassword(next);
      if (problem) throw new Error(problem);
      fixtures.adminPassword = next;
    },
    ResetAdminPassword: async (code: string, next: string) => {
      requireServer();
      const now = Math.floor(Date.now() / 1000);
      if (fixtures.recoveryRetryAfter > now) throw new Error(`too many recovery attempts; try again in ${fixtures.recoveryRetryAfter - now} seconds`);
      const problem = await methods.ValidatePassword(next);
      if (problem) throw new Error(problem);
      const normalize = (value: string) => value.replace(/[\s-]/g, "").toUpperCase();
      const index = fixtures.recoveryCodes.findIndex((candidate) => candidate && normalize(candidate) === normalize(code));
      if (index < 0) {
        fixtures.recoveryFailures++;
        if (fixtures.recoveryFailures >= 5) fixtures.recoveryRetryAfter = now + (fixtures.recoveryFailures - 4) * 60;
        throw new Error("that recovery code is invalid or already used; use an unused code from the latest set");
      }
      fixtures.recoveryCodes[index] = "";
      fixtures.adminPassword = next;
      fixtures.recoveryFailures = 0;
      fixtures.recoveryRetryAfter = 0;
    },
    RancherDesktopInstalled: async () => fixtures.rancherInstalled,
    RunUninstall: async (_images: boolean, _backups: boolean, _rancher: boolean, password: string) => {
      requireServer();
      if (password !== fixtures.adminPassword) throw new Error("the Desktop admin password does not match this installation");
      acceptJob("uninstall");
    },
    RemoveApp: async () => {
      if (state.setup_done || state.client_url) throw new Error("uninstall the clinic setup or disconnect this computer before removing the app");
      if (!fixtures.canRemoveApp) throw new Error("This preview has no removable application bundle.");
      fixtures.appRemoved = true;
      logs.push("Test only: the desktop application would close and be removed.");
    },
    ExitUninstall: async () => { logs.push("Test only: the uninstaller would finish."); },
    LogPath: async () => "/test-fixtures/logs/care-desktop.log",
    CleanupFailedInstall: async () => {
      requireServer();
      if (state.setup_done) throw new Error("this clinic is installed; use Uninstall instead");
      fixtures.recovery = { backup_saved: false, backup_verified: false, codes_saved: false, backup_path: "", codes_path: "", backup_problem: "", codes_problem: "" };
      fixtures.backupDir = "/test-fixtures/Desktop/care-db-backups";
      fixtures.setupStarted = false;
      fixtures.residue = { clean: true, traces: [] };
    },
    DockerPlan: async () => softwarePlan("docker"),
    GitPlan: async () => softwarePlan("git"),
    RancherDownloadInfo: async () => ({ name: "Rancher Desktop preview", size: 612e6 }),
    InstallDocker: () => installSoftware("docker"),
    InstallGit: () => installSoftware("git"),
    OpenDocker: async () => { requireServer(); fixtures.docker.ok = true; },
    InstallWSL: async () => { requireServer(); fixtures.wsl.ok = true; return ""; },
    FixNetwork: async () => { requireServer(); fixtures.network.ok = true; },
    RestartPlan: async () => ({ ...fixtures.restart }),
    PurgeResidue: async (confirmed: boolean) => {
      requireServer();
      if (!confirmed) throw new Error("confirm the removal of the earlier CARE Desktop before it can be removed");
      fixtures.residue = { clean: true, traces: [] };
      fixtures.recovery = { ...fixtures.recovery, backup_saved: false, backup_verified: false, codes_saved: false, backup_path: "", codes_path: "" };
      return { ...fixtures.residue };
    },
    ValidateSetup: async (name: string, password: string, dir: string): Promise<SetupIssue[]> => {
      requireServer();
      const result: SetupIssue[] = [];
      if (!fixtures.disk.ok || !fixtures.disk.need) result.push({ step: "space", message: "Space has run low since you checked." });
      if (fixtures.wsl.applicable && (!fixtures.wsl.ok || fixtures.restart.needed)) result.push({ step: "windows", message: "Windows still needs to restart." });
      if (!fixtures.docker.ok || !fixtures.git.ok) result.push({ step: "software", message: "The required software is no longer ready." });
      if (!fixtures.residue.clean) result.push({ step: "cleanup", message: "Something from the earlier setup is back." });
      if (fixtures.network.applicable && !fixtures.network.ok) result.push({ step: "network", message: "This network is set to Public again." });
      if (fixtures.addressTaken || await methods.ValidateDomain(name)) result.push({ step: "address", message: "The clinic address needs another look." });
      if (fixtures.folderProblem || !fixtures.recovery.backup_verified || fixtures.recovery.backup_problem) result.push({ step: "backup", message: "Check the backup location and recovery file." });
      if (!fixtures.recovery.codes_saved || fixtures.recovery.codes_problem || await methods.ValidatePassword(password)) result.push({ step: "admin", message: "Save a valid password and the six recovery codes." });
      void dir;
      return result;
    },
    RunSetup: async (name: string, password: string, dir: string): Promise<void> => {
      if (new URLSearchParams(location.search).get("simulateInstall") !== "1") throw new Error("This action is disabled in the automated test fixture.");
      const issues = await methods.ValidateSetup(name, password, dir);
      if (issues.length) throw new Error(`setup needs attention (${issues[0].step}): ${issues[0].message}`);
      state.mdns_name = name.endsWith(".local") ? name : `${name}.local`;
      fixtures.adminPassword = password;
      fixtures.setupStarted = true;
      fixtures.backupDir = `${dir || "/test-fixtures/Desktop"}/care-db-backups`;
      fixtures.storage.backup.dir = fixtures.backupDir;
      fixtures.backups = [];
      fixtures.storage.newest_backup_at = 0;
      fixtures.storage.last_run.state = "";
      fixtures.residue = { clean: false, traces: [{ id: "config", label: "Settings from the unfinished installation", detail: "" }] };
      if (!fixtures.finishJobs) return;
      void (async () => {
        for (const line of [
          "Backup encryption ready; only the public certificate is installed.",
          "Generated a random DJANGO_SECRET_KEY in backend.env",
          "Building CARE's images - the backend and app build in the background while setup continues...",
          "Starting the secure gateway so this computer can be set up now...",
          "Waiting for the backend and app images to finish building...",
          "Starting CARE...",
          "Applying database migrations...",
          "Waiting for CARE to become healthy...",
        ]) {
          emit("care-log", line);
          await wait(700);
        }
        finishJob("setup", scenario === "installation-failed" ? "Simulated installation failure for preview." : undefined);
      })();
    },
    CheckAppUpdate: async () => {
      if (scenario === "offline") throw new Error("couldn't reach GitHub to check for updates: network is unreachable");
      if (scenario === "check-error") throw new Error("GitHub answered HTTP 403 for https://api.github.com");
      if (scenario === "unavailable") throw new Error("release 0.1.6 has no installer for this computer");
      return { ...update };
    },
    InstallAppUpdate: async () => {
      if (updateRunning) throw new Error("something else is still running - wait for it to finish");
      updateRunning = true;
      // Fixture events only. No native updater, filesystem, or network is used.
      queueMicrotask(() => {
        if (scenario === "failed" || scenario === "download-failed") {
          finishUpdate(scenario === "failed"
            ? "couldn't replace CARE Desktop, so the current version was kept: exit status 1"
            : "the CARE Desktop 0.1.6 update didn't download properly and was deleted without being installed (SHA-256 mismatch)");
          return;
        }
        const phase: AppUpdateProgress["phase"] =
          scenario === "installing" || scenario === "verifying" ||
          scenario === "restarting" || scenario === "installer"
            ? scenario : "downloading";
        emit("app-update-progress", { phase, done: 31_000_000, total: 50_000_000 });
        if (phase === "installer" || phase === "restarting") finishUpdate();
      });
    },
    OpenURL: async () => { logs.push("Test only: release notes requested."); },
    OpenLogFolder: async () => { logs.push("Test only: log file requested."); },
    SetQuitDialogReady: async (ready: boolean) => {
      if (!ready) quitRequest = null;
      return quitRequest ? { ...quitRequest } : null;
    },
    RespondToQuit: async (id: number, quit: boolean) => {
      if (!quitRequest || quitRequest.id !== id) throw new Error("this quit request is no longer active");
      quitRequest = null;
      fixtures.quitAccepted = quit;
      if (quit) logs.push("Test only: CARE Desktop would close after confirmation.");
    },
    SetConfirmationDialogReady: async (ready: boolean) => {
      if (!ready) confirmation = null;
      return confirmation ? { ...confirmation } : null;
    },
    RespondToConfirmation: async (id: number, approved: boolean) => {
      if (!confirmation || confirmation.id !== id) throw new Error("this permission request is no longer active");
      confirmation = null;
      logs.push(`Test only: permission ${approved ? "approved" : "declined"}.`);
    },
  } satisfies Partial<Host>;

  const unavailable = async (): Promise<never> => {
    throw new Error("This action is disabled in the automated test fixture.");
  };
  // Every omitted action fails closed. Keeping the full type also catches new
  // bindings so a preview cannot accidentally inherit a real native method.
  const host: Host = new Proxy({
    SelectRole: unavailable,
    RestartNow: unavailable,
    RestoreBackup: unavailable,
    ...methods,
  }, {
    get(target, key) {
      if (typeof key !== "string" || !Object.prototype.hasOwnProperty.call(target, key)) return undefined;
      return async (...args: unknown[]) => {
        const method = key as Method;
        calls.push({ method, args });
        await holds.get(method)?.wait;
        if (failures.has(method)) {
          const error = failures.get(method);
          failures.delete(method);
          throw new Error(error);
        }
        if (responses.has(method)) return responses.get(method);
        const handler: unknown = Reflect.get(target, key);
        if (typeof handler !== "function") throw new Error(`${key} is disabled in the automated test fixture.`);
        return Reflect.apply(handler, target, args);
      };
    },
  });

  window.go = { main: { App: host } };
  window.runtime = {
    EventsOn(event, handler) {
      const handlers = listeners.get(event) ?? new Set<Handler>();
      handlers.add(handler);
      listeners.set(event, handlers);
      return () => { handlers.delete(handler); };
    },
    EventsEmit: emit,
    LogPrint: (line) => { logs.push(line); },
  };
  window.careTest = {
    calls, logs, state, emit, fixtures, finishJob,
    requestQuit: (action) => {
      quitRequest = { id: ++quitSequence, title: "CARE is still working", message: `CARE is still ${action}. Quitting now stops it before it finishes. Commands that already started may keep running in the background for a few minutes.` };
      emit("quit-requested", { ...quitRequest });
    },
    requestConfirmation: (title, message) => {
      confirmation = { id: ++confirmationSequence, title, message };
      emit("confirmation-requested", { ...confirmation });
    },
    cancelConfirmation: () => {
      if (!confirmation) return;
      const id = confirmation.id;
      confirmation = null;
      emit("confirmation-cancelled", id);
    },
    respond: (method, value) => { responses.set(method, value); },
    failNext: (method, error) => { failures.set(method, error); },
    hold(method) {
      let release = () => {};
      const wait = new Promise<void>((resolve) => { release = resolve; });
      holds.set(method, { wait, release });
    },
    release(method) {
      holds.get(method)?.release();
      holds.delete(method);
    },
    progress: (value) => emit("app-update-progress", value),
    finishUpdate,
    setUpdate: (value) => { update = value; },
  };
}

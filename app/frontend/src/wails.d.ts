// Ambient types for the Wails bridge that the runtime injects on window.
// Go methods (main.App) are exposed as window.go.main.App.<Method> returning
// Promises; events go through window.runtime.
import type {
  AppState,
  AppUpdate,
  Backup,
  BackupPolicy,
  BackupSpace,
  CarePlugin,
  PluginCatalogEntry,
  QuitRequest,
  ChannelStatus,
  ClientPreflight,
  ClientReachability,
  ClinicInfo,
  ConfirmationRequest,
  DiskStatus,
  DockerStatus,
  DownloadInfo,
  Health,
  ImportedBackup,
  NameStatus,
  NetworkStatus,
  ResidueReport,
  RestartPlan,
  SetupIssue,
  SetupRecoveryStatus,
  StorageReport,
  ToolPlan,
  WSLStatus,
} from "./types";

declare global {
  interface Window {
    go: {
      main: {
        App: {
          GetState(): Promise<AppState>;
          SelectRole(role: "server" | "client"): Promise<void>;
          BeginServerSetup(): Promise<void>;
          ClearRole(): Promise<void>;
          FindClinic(address: string): Promise<ClinicInfo>;
          ClientPreflight(): Promise<ClientPreflight>;
          ClientReachable(): Promise<ClientReachability>;
          ConnectClient(address: string): Promise<void>;
          DisconnectClient(): Promise<void>;
          DockerStatus(): Promise<DockerStatus>;
          GitStatus(): Promise<DockerStatus>;
          MDNSStatus(name: string): Promise<NameStatus>;
          NetworkStatus(): Promise<NetworkStatus>;
          FixNetwork(): Promise<void>;
          WSLStatus(): Promise<WSLStatus>;
          InstallWSL(): Promise<string>;
          DockerPlan(): Promise<ToolPlan>;
          RancherDownloadInfo(): Promise<DownloadInfo>;
          GitPlan(): Promise<ToolPlan>;
          InstallDocker(): Promise<string>;
          InstallGit(): Promise<string>;
          OpenDocker(): Promise<void>;
          ScanResidue(): Promise<ResidueReport>;
          PurgeResidue(confirmed: boolean): Promise<ResidueReport>;
          RestartPlan(): Promise<RestartPlan>;
          RestartNow(): Promise<void>;
          ClinicHealth(): Promise<Health>;
          ValidatePassword(pw: string): Promise<string>;
          ValidateDomain(name: string): Promise<string>;
          ValidateBackupDir(dir: string): Promise<string>;
          BackupDirSpace(dir: string): Promise<BackupSpace>;
          DiskStatus(): Promise<DiskStatus>;
          StorageStatus(): Promise<StorageReport>;
          RecheckStorage(): Promise<StorageReport>;
          SetMDNSName(name: string): Promise<void>;
          VerifyAdminPassword(pw: string): Promise<boolean>;
          GetSetupRecoveryStatus(): Promise<SetupRecoveryStatus>;
          SaveSetupBackupRecovery(backupDir: string): Promise<boolean>;
          ReplaceSetupBackupRecovery(backupDir: string): Promise<boolean>;
          VerifySetupBackupRecovery(backupDir: string): Promise<boolean>;
          ChooseRecoveryFile(): Promise<string>;
          SaveAdminRecoveryCodes(adminPassword: string, backupDir: string): Promise<boolean>;
          OpenSetupRecoveryCodes(): Promise<void>;
          OpenSetupRecoveryFolder(codes: boolean): Promise<void>;
          ValidateSetup(name: string, password: string, backupDir: string): Promise<SetupIssue[]>;
          ChangeAdminPassword(currentPassword: string, newPassword: string): Promise<void>;
          ResetAdminPassword(code: string, newPassword: string): Promise<void>;
          ClinicAction(action: string, adminPassword: string): Promise<void>;
          ClinicStatus(): Promise<string>;
          RunSetup(
            mdnsName: string,
            adminPassword: string,
            backupDir: string,
          ): Promise<void>;
          CleanupFailedInstall(): Promise<void>;
          ReadEnv(name: string, adminPassword: string): Promise<string>;
          WriteEnv(name: string, content: string, adminPassword: string): Promise<void>;
          ReadPlugins(): Promise<CarePlugin[]>;
          SavePlugins(plugins: CarePlugin[]): Promise<void>;
          PluginCatalog(): Promise<PluginCatalogEntry[]>;
          ListBackups(): Promise<Backup[]>;
          GetBackupDir(): Promise<string>;
          GetBackupPolicy(): Promise<BackupPolicy>;
          SetBackupDir(dir: string): Promise<string>;
          ChooseBackupFile(): Promise<string>;
          InspectBackupFile(path: string): Promise<ImportedBackup>;
          RestoreFromFile(path: string, recoveryFile: string, adminPassword: string): Promise<void>;
          RestoreBackup(dbDump: string, filesArchive: string, recoveryFile: string, adminPassword: string): Promise<void>;
          RancherDesktopInstalled(): Promise<boolean>;
          RunUninstall(removeImages: boolean, removeBackups: boolean, removeRancher: boolean, adminPassword: string): Promise<void>;
          OpenURL(url: string): Promise<void>;
          ChooseFolder(title: string): Promise<string>;
          LogPath(): Promise<string>;
          OpenLogFolder(): Promise<void>;
          WasAutostartLaunched(): Promise<boolean>;
          UninstallRequested(): Promise<boolean>;
          ExitUninstall(): Promise<void>;
          CanRemoveApp(): Promise<boolean>;
          RemoveApp(): Promise<void>;
          AutostartEnabled(): Promise<boolean>;
          SetAutostart(on: boolean): Promise<void>;
          CareUpdateStatus(): Promise<ChannelStatus>;
          CheckCareUpdate(): Promise<void>;
          DismissCareUpdate(): Promise<void>;
          CheckAppUpdate(): Promise<AppUpdate>;
          InstallAppUpdate(): Promise<void>;
          SetQuitDialogReady(ready: boolean): Promise<QuitRequest | null>;
          RespondToQuit(id: number, quit: boolean): Promise<void>;
          SetConfirmationDialogReady(ready: boolean): Promise<ConfirmationRequest | null>;
          RespondToConfirmation(id: number, approved: boolean): Promise<void>;
        };
      };
    };
    runtime: {
      EventsOn(event: string, cb: (...data: any[]) => void): () => void;
      EventsEmit(event: string, ...data: any[]): void;
      LogPrint(message: string): void;
    };
  }
}

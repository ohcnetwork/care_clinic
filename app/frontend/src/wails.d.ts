// Ambient types for the Wails bridge that the runtime injects on window.
// Go methods (main.App) are exposed as window.go.main.App.<Method> returning
// Promises; events go through window.runtime.
import type {
  AppState,
  AppUpdate,
  Backup,
  BackupSpace,
  CarePlugin,
  PluginCatalogEntry,
  ChannelStatus,
  DiskStatus,
  DockerStatus,
  Health,
  ImportedBackup,
  NameStatus,
  NetworkStatus,
  ResidueReport,
  RestartPlan,
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
          ClearRole(): Promise<void>;
          ConnectClient(address: string): Promise<void>;
          DisconnectClient(): Promise<void>;
          DockerStatus(): Promise<DockerStatus>;
          GitStatus(): Promise<DockerStatus>;
          MDNSStatus(): Promise<NameStatus>;
          NetworkStatus(): Promise<NetworkStatus>;
          FixNetwork(): Promise<void>;
          WSLStatus(): Promise<WSLStatus>;
          InstallWSL(): Promise<string>;
          DockerPlan(): Promise<ToolPlan>;
          GitPlan(): Promise<ToolPlan>;
          InstallDocker(): Promise<string>;
          InstallGit(): Promise<string>;
          OpenDocker(): Promise<void>;
          ScanResidue(): Promise<ResidueReport>;
          PurgeResidue(): Promise<void>;
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
          ClinicAction(action: string, adminPassword: string): Promise<void>;
          ClinicStatus(): Promise<string>;
          RunSetup(
            mdnsName: string,
            adminPassword: string,
            backupPassword: string,
            backupDir: string,
          ): Promise<void>;
          CleanupFailedInstall(): Promise<void>;
          ReadEnv(name: string, adminPassword: string): Promise<string>;
          WriteEnv(name: string, content: string, adminPassword: string): Promise<void>;
          ReadPlugins(adminPassword: string): Promise<CarePlugin[]>;
          SavePlugins(plugins: CarePlugin[], adminPassword: string): Promise<void>;
          PluginCatalog(): Promise<PluginCatalogEntry[]>;
          ListBackups(): Promise<Backup[]>;
          GetBackupDir(): Promise<string>;
          SetBackupDir(dir: string): Promise<string>;
          ChooseBackupFile(): Promise<string>;
          InspectBackupFile(path: string): Promise<ImportedBackup>;
          RestoreFromFile(path: string, passphrase: string, adminPassword: string): Promise<void>;
          RestoreBackup(dbDump: string, filesArchive: string, passphrase: string, adminPassword: string): Promise<void>;
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

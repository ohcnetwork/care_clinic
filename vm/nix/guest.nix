{ config, lib, pkgs, modulesPath, version, ... }:
let
  agent = import ./agent.nix { inherit pkgs lib version; };
in
{
  imports = [ "${modulesPath}/profiles/minimal.nix" "${modulesPath}/profiles/headless.nix" ];
  system.stateVersion = "25.05";
  networking.hostName = "care-engine";
  nix.enable = false;
  system.switch.enable = false;
  documentation.enable = false;
  environment.defaultPackages = [ ];
  security.sudo.enable = false;

  microvm = {
    # The host app boots the kernel/initrd/disks itself (Virtualization.framework
    # on macOS, QEMU+WHPX on Windows); "qemu" here only shapes the guest config.
    hypervisor = "qemu";
    mem = 4096;
    vcpu = 2;
    storeOnDisk = true;
    storeDiskType = "erofs";
    storeDiskErofsFlags = [ "-zlz4hc" "-Eztailpacking" "-Efragments" "-Ededupe" ];
    volumes = [{ image = "data.img"; mountPoint = "/var/lib"; size = 65536; }];
  };

  # The host attaches the data disk with the serial/identifier "care-data".
  fileSystems."/var/lib" = {
    device = lib.mkForce "/dev/disk/by-id/virtio-care-data";
    fsType = "ext4";
    autoResize = true;
    neededForBoot = true;
  };
  boot.initrd.supportedFilesystems = [ "erofs" "ext4" ];
  boot.initrd.availableKernelModules = [ "virtio_pci" "virtio_blk" "virtio_mmio" ];
  boot.kernelModules = [ "vmw_vsock_virtio_transport" "virtio_net" "virtiofs" "fuse" "qemu_fw_cfg" "virtio_balloon" "overlay" "br_netfilter" ];
  boot.kernel.sysctl."net.ipv4.ip_forward" = 1;

  networking.useDHCP = true;
  networking.firewall.enable = true;
  # 80/443 for the clinic (Caddy container); 2376/2377 are the TLS docker and
  # control ports used on Windows, where QEMU forwards host loopback to them.
  networking.firewall.allowedTCPPorts = [ 80 443 2376 2377 ];
  networking.firewall.trustedInterfaces = [ "docker0" "br-+" ];

  time.timeZone = "UTC";
  services.timesyncd.enable = true;
  services.fstrim.enable = true;
  services.journald.settings.Journal = {
    Storage = "persistent";
    SystemMaxUse = "200M";
    RuntimeMaxUse = "50M";
    MaxRetentionSec = "7day";
  };

  virtualisation.docker = {
    enable = true;
    # Docker's data lives on the persistent disk mounted at /var/lib.
    daemon.settings = {
      data-root = "/var/lib/docker";
      log-driver = "journald";
      log-opts = { mode = "non-blocking"; max-buffer-size = "4m"; };
      features.buildkit = true;
    };
  };

  environment.systemPackages = [ pkgs.docker pkgs.util-linux pkgs.e2fsprogs ];

  systemd.services.care-engine-agent = {
    description = "CARE engine agent (docker and control proxy for the host app)";
    wantedBy = [ "multi-user.target" ];
    wants = [ "docker.service" ];
    after = [ "local-fs.target" "docker.socket" ];
    path = [ pkgs.systemd pkgs.util-linux ];
    serviceConfig = {
      ExecStart = "${agent}/bin/care-engine-agent";
      Restart = "always";
      RestartSec = 1;
    };
  };
}

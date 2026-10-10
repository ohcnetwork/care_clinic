{
  description = "CARE engine: the small Linux VM that runs Docker for CARE Clinic on macOS and Windows";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/75d3d33e5e727d548fb60e391cf23c2fd3cebd69";
    microvm = {
      url = "github:microvm-nix/microvm.nix/a4251ae36520c59a31afc07ffc32aa4fd7f214d2";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
  outputs = { self, nixpkgs, microvm }:
    let
      systems = [ "aarch64-linux" "x86_64-linux" ];
      forSystems = nixpkgs.lib.genAttrs systems;
      version = nixpkgs.lib.removeSuffix "\n" (builtins.readFile ./VERSION);
    in {
      nixosConfigurations = forSystems (system: nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs = { inherit version; };
        modules = [ microvm.nixosModules.microvm ./nix/guest.nix ];
      });
      packages = forSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          bundle = import ./nix/bundle.nix { inherit pkgs version; guest = self.nixosConfigurations.${system}; };
          default = self.packages.${system}.bundle;
        });
    };
}

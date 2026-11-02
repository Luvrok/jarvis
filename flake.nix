{
  description = "jarvis: a local voice assistant for nixos";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      # services.jarvis, see the readme
      nixosModules.default = ./module.nix;

      packages.${system}.default = pkgs.callPackage ./package.nix { };

      # the module on a bare system, evaluated only: building it would
      # download every model
      checks.${system}.module =
        let
          toplevel =
            (nixpkgs.lib.nixosSystem {
              inherit system;
              modules = [
                self.nixosModules.default
                {
                  services.jarvis.enable = true;
                  boot.loader.grub.enable = false;
                  fileSystems."/" = {
                    device = "none";
                    fsType = "tmpfs";
                  };
                  system.stateVersion = "26.05";
                }
              ];
            }).config.system.build.toplevel;
        in
        pkgs.writeText "jarvis-module-evaluates" (builtins.unsafeDiscardStringContext toplevel.drvPath);
    };
}

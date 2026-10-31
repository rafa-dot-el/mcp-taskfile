{
  description = "mcp-taskfile: Taskfile-to-MCP bridge (Go)";

  # This flake serves this repo only (its CI, `nix run`, cache:push).
  # Consumers do NOT use it: they import ./package.nix as source
  # (`flake = false`) and build it with their own pkgs.
  # The dev shell is standalone devenv; devenv.yaml pins the same nixpkgs
  # revision as flake.lock (`task check:pins`).
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        mcp-taskfile = pkgs.callPackage ./package.nix { };
        default = mcp-taskfile;
      });

      checks = forAllSystems (pkgs: {
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.mcp-taskfile;
      });
    };
}

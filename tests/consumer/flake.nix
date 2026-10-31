{
  # Fixture for `task check:consumer`: a consumer on a DIFFERENT nixpkgs branch
  # that imports this repo the way nix-hermes does (option C): source only
  # (`flake = false`), built by package.nix with the consumer's own pkgs.
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    mcp-taskfile = {
      url = "path:../..";
      flake = false;
    };
  };

  outputs =
    { nixpkgs, mcp-taskfile, ... }:
    let
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
    in
    {
      consumerNixpkgsRev = nixpkgs.rev;
      consumerGoVersion = pkgs.go_1_26.version;
      mcp-taskfile = pkgs.callPackage "${mcp-taskfile}/package.nix" { };
    };
}

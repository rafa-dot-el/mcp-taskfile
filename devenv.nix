{ pkgs, ... }:

{
  packages = with pkgs; [
    go_1_26 # same toolchain as package.nix
    gopls
    golangci-lint
    go-task
    govulncheck
    gitleaks
    osv-scanner
    nixfmt
    statix
    deadnix
    actionlint
    shellcheck
    jq
    curl
    prek
    cachix
  ];

  # Pure-Go build: no C compiler needed, static binary.
  env.CGO_ENABLED = "0";

  git-hooks.hooks = {
    gofmt.enable = true;
    golangci-lint.enable = true;
    nixfmt.enable = true;
    actionlint.enable = true;
    shellcheck.enable = true;
    gitleaks.enable = true;
  };
}

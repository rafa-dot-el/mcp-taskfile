{
  lib,
  buildGoModule,
  go_1_26,
}:

let
  # Single version source, shared with `task build`.
  version = lib.fileContents ./VERSION;
  # go.mod requires go >= 1.26.0 (golang.org/x/net fixes need it); stated here
  # instead of relying on whatever `go` the consumer's nixpkgs defaults to.
  buildGo126Module = buildGoModule.override { go = go_1_26; };
in
buildGo126Module {
  pname = "mcp-taskfile";
  inherit version;

  # Only what the Go build and tests read: editing README, CI or dev-env files
  # must not change the derivation hash (keeps binary-cache substitution stable).
  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./go.mod
      ./go.sum
      (lib.fileset.fileFilter (file: file.hasExt "go") ./.)
      ./testdata
    ];
  };

  vendorHash = "sha256-GJcxrj+IdYk6FnHMPepVZ/rvnm6aklHqnitor9QEJTc=";

  env.CGO_ENABLED = "0";
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  meta = {
    description = "MCP server that exposes Taskfile tasks as tools";
    license = lib.licenses.gpl3Plus;
    mainProgram = "mcp-taskfile";
  };
}

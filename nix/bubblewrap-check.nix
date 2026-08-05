{ pkgs, repowolf-client }:

let
  jailClosure = pkgs.closureInfo {
    rootPaths = [
      repowolf-client
      pkgs.gitMinimal
      pkgs.bash
    ];
  };
in
pkgs.buildGoModule {
  pname = "repowolf-bubblewrap-check";
  version = "dev";

  src = pkgs.lib.cleanSource ../.;
  vendorHash = "sha256-gTntGkqO04KwcyrJi3jNVwNAevKQddTGU3npLupIWik=";

  nativeCheckInputs = [
    pkgs.bubblewrap
    pkgs.gitMinimal
    pkgs.procps
  ];

  REPOWOLF_TEST_BUBBLEWRAP = "${pkgs.bubblewrap}/bin/bwrap";
  REPOWOLF_TEST_CLIENT_ROOT = "${repowolf-client}";
  REPOWOLF_TEST_GIT_ROOT = "${pkgs.gitMinimal}";
  REPOWOLF_TEST_SHELL = "${pkgs.bash}/bin/bash";
  REPOWOLF_TEST_CLOSURE_FILE = "${jailClosure}/store-paths";

  dontBuild = true;
  doCheck = true;
  preCheck = ''
    if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      git init -q
      git add -A
      git -c user.name='RepoWolf Nix check' -c user.email='nix-check@invalid' \
        commit -qm 'Nix check source snapshot'
    fi
  '';
  checkPhase = ''
    runHook preCheck
    go test -trimpath=false -tags=integration ./integration -run '^TestBubblewrap' -count=1
    runHook postCheck
  '';
  installPhase = ''
    touch "$out"
  '';

  passthru.testConfig = {
    bubblewrap = "${pkgs.bubblewrap}/bin/bwrap";
    clientRoot = "${repowolf-client}";
    gitRoot = "${pkgs.gitMinimal}";
    shell = "${pkgs.bash}/bin/bash";
    closureFile = "${jailClosure}/store-paths";
  };
}

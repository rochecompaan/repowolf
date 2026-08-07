{ inputs, pkgs, ... }:

let
  repowolfPackages = inputs.repowolf.packages.${pkgs.stdenv.system};
in
{
  packages = [
    pkgs.go
    pkgs.goreleaser
    pkgs.skopeo
    repowolfPackages.repowolf
    repowolfPackages."repowolf-client"
  ];

  env.REPOWOLF_DOGFOOD_REAL_GH = "${pkgs.gh}/bin/gh";
  env.REPOWOLF_DOGFOOD_REAL_SSH = "${pkgs.openssh}/bin/ssh";

  tasks."repowolf:bootstrap" = {
    exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} bootstrap";
    status = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} status";
    before = [ "devenv:enterShell" ];
  };

  processes.repowolf = {
    exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} serve";
    restart.on = "on_failure";
    ready.exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} probe";
  };

  scripts.dogfood-reset.exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} reset";

  enterShell = ''
    export REPOWOLF_ENDPOINT="https://localhost:9443"
    export REPOWOLF_TOKEN="$(cat "$DEVENV_ROOT/.devenv/repowolf/token")"
    export REPOWOLF_CA_FILE="$DEVENV_ROOT/.devenv/repowolf/tls/ca.crt"
    export GIT_SSH_COMMAND="repowolf-git-ssh"
    if [ -z "''${SSH_AUTH_SOCK:-}" ]; then
      echo "repowolf-dogfood: warning: SSH_AUTH_SOCK is unset; brokered Git operations will fail" >&2
    fi
    if ! (exec 3<>/dev/tcp/127.0.0.1/9443) 2>/dev/null; then
      echo "repowolf-dogfood: starting broker in the background (first activation may take a few minutes)"
      nohup ${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} autostart >/dev/null 2>&1 &
    fi
  '';
}

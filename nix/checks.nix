{ pkgs, repowolf, repowolf-client, ociImage }:

let
  clientClosure = pkgs.closureInfo {
    rootPaths = [ repowolf-client ];
  };
in
{
  server-package = repowolf;
  client-package = repowolf-client;
  oci-image = ociImage;
  bubblewrap = pkgs.callPackage ./bubblewrap-check.nix { inherit repowolf-client; };

  client-closure = pkgs.runCommand "repowolf-client-closure-check" { } ''
    set -eu

    test -x ${repowolf-client}/bin/repowolf-client
    test "$(readlink ${repowolf-client}/bin/gh)" = repowolf-client
    test "$(readlink ${repowolf-client}/bin/tea)" = repowolf-client
    test "$(readlink ${repowolf-client}/bin/repowolf-git-ssh)" = repowolf-client
    set +e
    diagnostic="$(${repowolf-client}/bin/tea login 2>&1)"
    status=$?
    set -e
    test "$status" -eq 2
    test "$diagnostic" = 'tea: expected repos OWNER/REPO --repo OWNER/REPO or issues [list|INDEX] --repo OWNER/REPO [approved flags]'

    closure="$(cat ${clientClosure}/store-paths)"
    for forbidden in ${pkgs.gh} ${pkgs.openssh} ${repowolf}; do
      if printf '%s\n' "$closure" | grep -Fqx "$forbidden"; then
        echo "forbidden provider or service package in client closure: $forbidden" >&2
        exit 1
      fi
    done

    while IFS= read -r path; do
      if find "$path" -type f \( \
        -name 'repowolf.yaml' -o \
        -name 'tls.key' -o \
        -name 'server.key' -o \
        -name 'ca.key' \
      \) -print -quit | grep -q .; then
        echo "service configuration or private key in client closure: $path" >&2
        exit 1
      fi
    done <<EOF_CLOSURE
$closure
EOF_CLOSURE

    touch $out
  '';
}

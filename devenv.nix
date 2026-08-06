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
}

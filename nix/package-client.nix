{ lib, buildGoModule }:

buildGoModule {
  pname = "repowolf-client";
  version = "dev";

  src = lib.cleanSource ../.;
  vendorHash = "sha256-gTntGkqO04KwcyrJi3jNVwNAevKQddTGU3npLupIWik=";

  subPackages = [ "cmd/repowolf-client" ];
  env.CGO_ENABLED = "0";
  ldflags = [
    "-s"
    "-w"
    "-X github.com/rochecompaan/repowolf/internal/buildinfo.Version=dev"
  ];

  postInstall = ''
    ln -s repowolf-client $out/bin/gh
    ln -s repowolf-client $out/bin/repowolf-git-ssh
  '';

  meta = {
    description = "Credential-free RepoWolf sandbox client";
    homepage = "https://github.com/rochecompaan/repowolf";
    mainProgram = "repowolf-client";
    platforms = lib.platforms.linux;
  };
}

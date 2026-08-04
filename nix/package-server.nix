{ lib, buildGoModule }:

buildGoModule {
  pname = "repowolf";
  version = "dev";

  src = lib.cleanSource ../.;
  vendorHash = "sha256-gTntGkqO04KwcyrJi3jNVwNAevKQddTGU3npLupIWik=";

  subPackages = [ "cmd/repowolf" ];
  env.CGO_ENABLED = "0";
  ldflags = [
    "-s"
    "-w"
    "-X github.com/rochecompaan/repowolf/internal/buildinfo.Version=dev"
  ];

  meta = {
    description = "Repository-scoped access broker service and administration CLI";
    homepage = "https://github.com/rochecompaan/repowolf";
    mainProgram = "repowolf";
    platforms = lib.platforms.linux;
  };
}

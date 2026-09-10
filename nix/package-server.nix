{ lib, buildGo126Module }:

buildGo126Module {
  pname = "repowolf";
  version = "dev";

  src = lib.cleanSource ../.;
  vendorHash = "sha256-CHlDZegb+zk6UmpYmDKZUOdJ6Vs2UKnVbhMJqaULoTg=";

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

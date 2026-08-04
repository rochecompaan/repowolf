{ lib, dockerTools, cacert, gh, openssh, repowolf }:

dockerTools.buildLayeredImage {
  name = "repowolf";
  tag = "mvp";

  contents = [ repowolf gh openssh cacert ];

  extraCommands = ''
    mkdir -p etc tmp
    chmod 1777 tmp
    cat > etc/passwd <<'PASSWD'
root:x:0:0:root:/root:/sbin/nologin
repowolf:x:65532:65532:RepoWolf:/tmp:/sbin/nologin
PASSWD
    cat > etc/group <<'GROUP'
root:x:0:
repowolf:x:65532:
GROUP
    cat > etc/nsswitch.conf <<'NSSWITCH'
passwd: files
group: files
hosts: files dns
NSSWITCH
  '';

  config = {
    User = "65532:65532";
    Entrypoint = [ "${repowolf}/bin/repowolf" ];
    Cmd = [ "serve" ];
    ExposedPorts."8443/tcp" = { };
    WorkingDir = "/tmp";
    Env = [
      "HOME=/tmp"
      "PATH=${lib.makeBinPath [ repowolf gh openssh ]}"
      "SSL_CERT_FILE=${cacert}/etc/ssl/certs/ca-bundle.crt"
    ];
  };
}

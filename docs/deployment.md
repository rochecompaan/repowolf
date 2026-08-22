# Deployment

Run RepoWolf under a supervisor. Keep provider credentials, policy, and TLS
private keys outside every agent sandbox.

## OCI container

```sh
docker run --rm -p 8443:8443 \
  --env-file /run/repowolf/service.env \
  --mount type=bind,src=/etc/repowolf/repowolf.yaml,dst=/etc/repowolf/repowolf.yaml,readonly \
  --mount type=bind,src=/run/repowolf/tls,dst=/run/repowolf/tls,readonly \
  ghcr.io/rochecompaan/repowolf:v0.1.0 \
  serve --config /etc/repowolf/repowolf.yaml
```

The mounted paths must be readable by UID 65532. Mount provider authentication only into the service container, never into an agent container.

## Systemd

Run the service under systemd, Home Manager, Kubernetes, or another supervisor that restarts it after startup-only inputs change.

A system service can use a root-owned environment file:

```ini
[Unit]
Description=RepoWolf access broker
After=network-online.target
Wants=network-online.target

[Service]
User=repowolf
Group=repowolf
EnvironmentFile=/run/repowolf/service.env
ExecStart=/usr/local/bin/repowolf serve --config /etc/repowolf/repowolf.yaml
Restart=on-failure
PrivateTmp=true
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

## Home Manager

Protect `service.env`, provider credentials, and TLS keys from untrusted users. A Home Manager deployment can define the equivalent user unit with a Nix store path:

```nix
systemd.user.services.repowolf = {
  Unit.Description = "RepoWolf access broker";
  Service = {
    EnvironmentFile = "%t/repowolf/service.env";
    ExecStart = "${inputs.repowolf.packages.${pkgs.system}.repowolf}/bin/repowolf serve --config %h/.config/repowolf/config.yaml";
    Restart = "on-failure";
  };
  Install.WantedBy = [ "default.target" ];
};
```

Keep the environment file outside the Nix store because it contains secret values.

## Kubernetes

Mount strict policy YAML from a ConfigMap, and mount the server certificate and key from a Secret. Expose each service token using a Secret-backed environment variable whose name exactly matches the policy's `tokenEnvs` entry. Provider tokens, provider configuration, and SSH keys are separate service-only Secrets.

Run the published image with `runAsUser: 65532`, `runAsGroup: 65532`, a writable temporary volume at `/tmp`, and `repowolf serve --config /etc/repowolf/repowolf.yaml`. A Service exposes port 8443. Agent pods receive only the client binary, their role's `REPOWOLF_TOKEN`, `REPOWOLF_ENDPOINT`, and a public CA mounted from a ConfigMap or Secret; they never mount service policy, provider credentials, or TLS private keys.

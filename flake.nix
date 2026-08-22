{
  description = "RepoWolf repository-scoped access broker";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          repowolf = pkgs.callPackage ./nix/package-server.nix { };
          repowolf-client = pkgs.callPackage ./nix/package-client.nix { };
          ociImage = pkgs.callPackage ./nix/oci.nix { inherit repowolf; };
        in
        {
          default = repowolf;
          inherit repowolf repowolf-client ociImage;
        });

      checks = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          repowolf = pkgs.callPackage ./nix/package-server.nix { };
          repowolf-client = pkgs.callPackage ./nix/package-client.nix { };
          ociImage = pkgs.callPackage ./nix/oci.nix { inherit repowolf; };
        in
        import ./nix/checks.nix {
          inherit pkgs repowolf repowolf-client ociImage;
        });

      devShells = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          default = pkgs.mkShell {
            packages = with pkgs; [ go goreleaser jq shellcheck skopeo ];
          };
        });
    };
}

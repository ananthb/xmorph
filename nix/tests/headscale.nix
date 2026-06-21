{ pkgs, lib, xmorph-package }:

let
  # Self-signed TLS cert for headscale hostname
  tls-cert = pkgs.runCommand "headscale-tls" {
    nativeBuildInputs = [ pkgs.openssl ];
  } ''
    mkdir -p $out
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
      -sha256 -days 1 -nodes -out $out/cert.pem -keyout $out/key.pem \
      -subj '/CN=headscale' -addext 'subjectAltName=DNS:headscale'
  '';

  # Minimal busybox rootfs. tsnet runs in xmorph itself post-pivot, so the
  # rootfs no longer needs tailscaled/tailscale binaries.
  test-rootfs = pkgs.runCommand "xmorph-test-rootfs" {
    nativeBuildInputs = [ pkgs.gnutar pkgs.gzip ];
  } ''
    mkdir -p rootfs/{bin,sbin,usr/bin,usr/sbin,etc/ssl/certs,dev/net,proc,sys,tmp}
    mkdir -p rootfs/var/{lib/tailscale,run}

    # Busybox
    cp ${pkgs.pkgsStatic.busybox}/bin/busybox rootfs/bin/
    for cmd in sh ls cat echo mount umount mkdir sleep hostname poweroff; do
      ln -sf busybox rootfs/bin/$cmd
    done

    # Trust headscale's self-signed cert alongside system CAs
    cat ${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt > rootfs/etc/ssl/certs/ca-certificates.crt
    cat ${tls-cert}/cert.pem >> rootfs/etc/ssl/certs/ca-certificates.crt

    # DNS: headscale node is at 192.168.1.1 on the test VLAN
    echo "192.168.1.1 headscale" > rootfs/etc/hosts
    echo "nameserver 192.168.1.1" > rootfs/etc/resolv.conf

    # Entrypoint: just wait. xmorph --init handles tsnet/SSH itself.
    cat > rootfs/bin/start.sh <<'SCRIPT'
#!/bin/sh
exec sleep infinity
SCRIPT
    chmod +x rootfs/bin/start.sh

    mkdir -p $out
    tar czf $out/rootfs.tar.gz -C rootfs .
  '';
in
pkgs.testers.nixosTest {
  name = "xmorph-headscale";

  nodes.headscale = { ... }: {
    services.headscale = {
      enable = true;
      port = 8080;
      settings = {
        server_url = "https://headscale";
        dns.base_domain = "xmorph.test";
        dns.nameservers.global = [ "127.0.0.1" ];
        derp.server = {
          enabled = true;
          region_id = 999;
          stun_listen_addr = "0.0.0.0:3478";
        };
        # Disable fetching upstream DERP map (no internet in test VM)
        derp.urls = [ ];
        noise.private_key_path = "/var/lib/headscale/noise_private.key";
      };
    };

    # TLS reverse proxy in front of headscale
    services.nginx = {
      enable = true;
      virtualHosts.headscale = {
        forceSSL = true;
        sslCertificate = "${tls-cert}/cert.pem";
        sslCertificateKey = "${tls-cert}/key.pem";
        locations."/" = {
          proxyPass = "http://127.0.0.1:8080";
          proxyWebsockets = true;
        };
      };
    };

    networking.firewall.allowedTCPPorts = [ 443 ];
    networking.firewall.allowedUDPPorts = [ 3478 ];
  };

  nodes.xmorph = { ... }: {
    environment.systemPackages = [ xmorph-package ];
    security.pki.certificateFiles = [ "${tls-cert}/cert.pem" ];
    virtualisation.memorySize = 4096;
  };

  testScript = ''
    start_all()

    # Wait for headscale + nginx
    headscale.wait_for_unit("headscale")
    headscale.wait_for_unit("nginx")
    headscale.wait_for_open_port(443)

    # Create user and pre-auth key (v0.28+ uses numeric user IDs)
    headscale.succeed("headscale users create xmorph")
    user_id = headscale.succeed(
        "headscale users list -o json | ${pkgs.jq}/bin/jq -r '.[0].id'"
    ).strip()
    authkey = headscale.succeed(
        f"headscale preauthkeys create --user {user_id} --reusable --expiration 1h"
    ).strip()

    xmorph.wait_for_unit("multi-user.target")

    # Run xmorph in container mode (blocks, so background it). tsnet handles
    # tailscale itself — no tailscaled/tailscale binaries needed in the rootfs.
    xmorph.succeed(
        "xmorph pivot --contain --force --no-init-coord --skip-verify "
        + "--rootfs ${test-rootfs}/rootfs.tar.gz "
        + f"--tailscale.authkey={authkey} "
        + "--tailscale.server=https://headscale "
        + "--entrypoint /bin/start.sh "
        + "--verbose "
        + "&>/tmp/xmorph.log &"
    )

    # Wait for node to register with headscale.
    # Default hostname is <hostname>-xmorph (from resolveTailscaleArgs).
    headscale.wait_until_succeeds(
        "headscale nodes list -o json-line | grep -q xmorph",
        timeout=120,
    )

    # Verify node appeared
    result = headscale.succeed("headscale nodes list -o json-line")
    assert "xmorph" in result, f"Node not found in headscale output: {result}"

    # Dump xmorph logs for debugging on failure
    print(xmorph.succeed("cat /tmp/xmorph.log 2>/dev/null || true"))
  '';
}

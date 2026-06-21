# Any Linux → Fedora CoreOS

Replaces the running OS with [Fedora CoreOS](https://fedoraproject.org/coreos/)
using the official `coreos-installer` container.

## Trusted source

| Artifact | Source |
|---|---|
| Installer image | `quay.io/coreos/coreos-installer:release` (built by the CoreOS project) |
| FCOS disk image | `https://builds.coreos.fedoraproject.org/streams/<stream>.json` (downloaded by the installer at runtime) |
| Config | Your own Ignition config, baked into the Containerfile |

No third-party hosting, no xmorph-hosted artifacts.

## Prerequisites

- Outbound HTTPS to `quay.io` and `builds.coreos.fedoraproject.org`
- Target disk at least ~10 GiB (FCOS metal image is fixed-size)
- 2 GiB+ RAM
- BIOS or UEFI; the FCOS metal image is a hybrid GPT and boots both
- A target disk that is not the one the running OS is using *as a mount* —
  fine here since xmorph has pivoted to RAM by the time the installer runs

## Files

### `config.bu` — Butane source for your Ignition config

```yaml
variant: fcos
version: 1.5.0
passwd:
  users:
    - name: core
      ssh_authorized_keys:
        - ssh-ed25519 AAAA... your-key-here
storage:
  files:
    - path: /etc/hostname
      mode: 0644
      contents:
        inline: myhost
```

Transpile to Ignition JSON before building the image:

```sh
podman run --rm -i quay.io/coreos/butane:release < config.bu > config.ign
```

### `install.sh` — entrypoint

```sh
#!/bin/sh
set -eu

coreos-installer install /dev/sda \
  --ignition-file /etc/config.ign \
  --stream stable \
  --architecture x86_64 \
  --platform metal

sync
reboot -f 2>/dev/null || echo b > /proc/sysrq-trigger
```

Make it executable: `chmod +x install.sh`.

### `Containerfile`

```dockerfile
FROM quay.io/coreos/coreos-installer:release
COPY install.sh /install.sh
COPY config.ign /etc/config.ign
ENTRYPOINT ["/install.sh"]
```

## Run it

From the directory containing `Containerfile`, `install.sh`, and `config.ign`:

```sh
sudo xmorph pivot --containerfile ./Containerfile
```

For an unattended run (no confirmation prompt):

```sh
sudo xmorph pivot --containerfile ./Containerfile --force
```

## What happens

1. xmorph builds an OCI image from the Containerfile, layering your
   `config.ign` and `install.sh` on top of `coreos-installer:release`.
2. The image is extracted into a tmpfs in RAM.
3. xmorph stops the running OS's services and `pivot_root`s into the
   new rootfs.
4. `install.sh` runs `coreos-installer install`, which:
   - Streams the metal FCOS image from `builds.coreos.fedoraproject.org`
   - Writes it to `/dev/sda` (hybrid GPT, boots BIOS and UEFI)
   - Embeds your Ignition config into the disk's `ignition` partition
5. The script syncs and reboots.
6. Firmware loads the freshly written disk; FCOS boots and runs Ignition
   on first boot, applying your users / files / units.

## Verify

After reboot you should be able to SSH in as the user you configured:

```sh
ssh core@<host>
rpm-ostree status   # confirms FCOS is running
```

## Notes and gotchas

- **`--stream`** picks `stable` / `testing` / `next`. Pin to `--image-url`
  + `--image-file` if you want exact reproducibility.
- **4Kn disks**: pass `--architecture x86_64` is fine; the metal image
  has 512b-sector and 4K-native variants — pick `--image-url` explicitly
  if you have a 4Kn disk.
- **Air-gapped install**: pass `--image-file /path/to/fcos.raw.xz` (baked
  into your Containerfile) instead of letting the installer download.
- **`--insecure-ignition`** is required if your Ignition is fetched over
  plain HTTP at runtime (use `--ignition-url` + `--ignition-hash` instead).

## Source docs

- [coreos-installer](https://coreos.github.io/coreos-installer/getting-started/)
- [FCOS bare-metal install](https://github.com/coreos/fedora-coreos-docs/blob/main/modules/ROOT/pages/bare-metal.adoc)
- [Butane config spec](https://coreos.github.io/butane/specs/)

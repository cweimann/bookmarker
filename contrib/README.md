# Service examples

Example process supervisors for Bookmarker. Paths and the `bookmarker` user are
conventions — edit them to match the host.

| Layout | Role |
|--------|------|
| [`daemontools/run`](daemontools/run) + [`daemontools/log/run`](daemontools/log/run) | daemontools / runit-style `supervise` |
| [`freebsd/rc.d/bookmarker`](freebsd/rc.d/bookmarker) | FreeBSD `/usr/local/etc/rc.d` |
| [`systemd/bookmarker.service`](systemd/bookmarker.service) | Linux systemd (preferred on modern Linux) |

## Shared setup

1. Build: `go build -o bookmarker .` (or `bookmarker.exe` on Windows, then copy the Linux/FreeBSD binary from a matching build).
2. Install the binary as `/usr/local/bin/bookmarker`.
3. Create an unprivileged user, e.g. `bookmarker`.
4. Copy [`../bookmarker.example.yaml`](../bookmarker.example.yaml) to the config path below and set an **absolute** `data:` directory owned by that user.
5. Set `listen` / `edit` / `view` for that host (do not leave OfficeAMD LAN addresses on a public VPS).

Suggested paths:

| | daemontools / FreeBSD | systemd (Linux) |
|--|----------------------|-----------------|
| Binary | `/usr/local/bin/bookmarker` | `/usr/local/bin/bookmarker` |
| Config | `/usr/local/etc/bookmarker.yaml` | `/etc/bookmarker/bookmarker.yaml` |
| Data | `/var/db/bookmarker` | `/var/lib/bookmarker` |
| User | `bookmarker` | `bookmarker` |

Example `data:` line in the installed yaml:

```yaml
data: /var/db/bookmarker
```

(or `/var/lib/bookmarker` on Linux).

## Enable

### daemontools

```sh
# After installing run + log/run (mode 755) under your service tree:
ln -s /var/service-avail/bookmarker /service/bookmarker
# Logs: /service/bookmarker/log/main
```

### FreeBSD

```sh
cp contrib/freebsd/rc.d/bookmarker /usr/local/etc/rc.d/bookmarker
chmod 755 /usr/local/etc/rc.d/bookmarker
sysrc bookmarker_enable=YES
service bookmarker start
```

### systemd

```sh
install -d /etc/bookmarker /var/lib/bookmarker
cp bookmarker.example.yaml /etc/bookmarker/bookmarker.yaml
# edit yaml: absolute data: /var/lib/bookmarker, listen/edit/view for this host
cp contrib/systemd/bookmarker.service /etc/systemd/system/bookmarker.service
systemctl daemon-reload
systemctl enable --now bookmarker
systemctl status bookmarker
```

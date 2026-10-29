# Windows service (NSSM)

[NSSM](https://nssm.cc/) (Non-Sucking Service Manager) wraps `bookmarker.exe` as a Windows service. Prefer this over raw `sc.exe` create — NSSM sets the working directory, captures stdout/stderr, and restarts cleanly.

## Layout

| Item | Example |
|------|---------|
| Install dir | `C:\Bookmarker\` |
| Binary | `C:\Bookmarker\bookmarker.exe` |
| Config | `C:\Bookmarker\bookmarker.yaml` (from `bookmarker.example.yaml`) |
| Data | absolute `data:` in the yaml, e.g. `C:\Bookmarker\data` |

Build once: `go build -o bookmarker.exe .` and copy the exe next to the yaml (or pass `-config`).

## Install

Download NSSM, then in an elevated PowerShell (adjust paths):

```powershell
nssm install Bookmarker C:\Bookmarker\bookmarker.exe
nssm set Bookmarker AppDirectory C:\Bookmarker
nssm set Bookmarker AppParameters "-config C:\Bookmarker\bookmarker.yaml"
nssm set Bookmarker DisplayName "Bookmarker"
nssm set Bookmarker Start SERVICE_AUTO_START
nssm set Bookmarker AppStdout C:\Bookmarker\logs\service.log
nssm set Bookmarker AppStderr C:\Bookmarker\logs\service.err.log
New-Item -ItemType Directory -Force -Path C:\Bookmarker\logs | Out-Null
nssm start Bookmarker
```

Manage with `nssm start|stop|restart Bookmarker` or `services.msc`.

Remove: `nssm stop Bookmarker` then `nssm remove Bookmarker confirm`.

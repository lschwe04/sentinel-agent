# SentinelCore Endpoint Agent 🤖

Der SentinelCore Agent läuft als ressourcenschonender Systemdienst auf Linux- und Windows-Endpunkten, erfasst Telemetriedaten, überwacht die Integrität (FIM) und setzt CIS-Hardening-Richtlinien um.

---

## ⚡ Quick Start / Installation

### Linux (Systemd & Sandbox-Hardening)
Führen Sie das Installationsskript mit einem gültigen Enrollment-Token aus:
```bash
sudo ./deployments/linux/install-agent.sh <DEIN_ENROLLMENT_TOKEN> "[https://hub.sentinel-core.local:8443](https://hub.sentinel-core.local:8443)"

Hinweis: Der Agent richtet sich automatisch als systemd-Dienst mit striktem Sandboxing (ProtectSystem=strict) ein.

Windows (PowerShell / GPO / MSI-Wrapper)
Führen Sie die PowerShell als Administrator aus:

Set-ExecutionPolicy Bypass -Scope Process -Force
.\deployments\windows\install-agent.ps1 -EnrollmentToken "<DEIN_ENROLLMENT_TOKEN>" -HubUrl "[https://hub.sentinel-core.local:8443](https://hub.sentinel-core.local:8443)"

Hinweis: Registriert den Agenten automatisch als Windows-Dienst (SentinelAgent) mit automatischem Start.

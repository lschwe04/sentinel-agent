# Sentinel Agent 🛰️

Sentinel Agent ist der leichtgewichtige, hochsichere Node-Agent, der auf Zielservern deployed wird, um lokale Befehle auszuführen, Telemetriedaten zu erfassen und den Sicherheitsstatus an den SentinelCore Hub zu melden.

## 🚀 Kernfunktionen

* **Sichere Telemetrie:** Erfasst kontinuierlich Systemauslastungen (CPU, RAM) über `gopsutil` und streamt diese gesichert an den Hub.
* **Lokale Ausführung:** Triggert und überwacht lokale Ansible-Playbooks zur Systemhärtung (`POST /trigger-ansible`)
* **Backup-Statusprüfung:** Liefert standardisierte Restic-Snapshot-Berichte über gesicherte HTTP-Endpunkte (`GET /backup-status`).

---

## 🔒 Hardened Security & Sandboxing

* **WireGuard-Binding:** Der HTTP-Server des Agenten lauscht strikt und ausschließlich auf der lokalen WireGuard-VPN-IP (`10.0.0.15`).
* **Netzwerk-Validierung:** Eingehende Anfragen werden durch eine Middleware gegen unautorisierte Subnetze geprüft (`validateVPNConnection`).
* **Systemd Enterprise Sandboxing:** Der Agent läuft als unprivilegierter Systemdienst unter strikten Kernel- und Dateisystem-Einschränkungen (`ProtectSystem=strict`, `NoNewPrivileges=true`, `MemoryDenyWriteExecute`).

---

## 🛠️ Technologie-Stack

* **Sprache:** Go 1.22+
* **Systemintegration:** Systemd, WireGuard, `gopsutil`
* **Deployment:** Tarball-Release-Artefakte inklusive vordefinierter Systemd-Unit-Files

---

## 📦 Installation als Systemd Service

1. Binärdatei kompilieren:
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sentinel-agent cmd/agent/main.go
Nach /usr/local/bin/ verschieben und Systemd-Service einrichten:

Bash
sudo cp sentinel-agent /usr/local/bin/
sudo cp deployments/systemd/sentinel-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sentinel-agent.service

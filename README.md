---

# Teil 2: Sentinel-Agent (`README.md` für das Agent-Repository)

```markdown
# Sentinel Agent 🛰️

Sentinel Agent ist der leichtgewichtige, hochsichere Node-Agent, der auf Zielservern deployed wird, um lokale Befehle auszuführen, Telemetriedaten zu erfassen und den Sicherheitsstatus an den SentinelCore Hub zu melden.

## 🚀 Kernfunktionen

* **Sichere Telemetrie:** Erfasst kontinuierlich Systemauslastungen (CPU, RAM) über `gopsutil` und streamt diese gesichert an den Hub[cite: 11, 12].
* **Lokale Ausführung:** Triggert und überwacht lokale Ansible-Playbooks zur Systemhärtung (`POST /trigger-ansible`)[cite: 11, 12].
* **Backup-Statusprüfung:** Liefert standardisierte Restic-Snapshot-Berichte über gesicherte HTTP-Endpunkte (`GET /backup-status`)[cite: 11, 12].

---

## 🔒 Hardened Security & Sandboxing

* **WireGuard-Binding:** Der HTTP-Server des Agenten lauscht strikt und ausschließlich auf der lokalen WireGuard-VPN-IP (`10.0.0.15`)[cite: 11, 12].
* **Netzwerk-Validierung:** Eingehende Anfragen werden durch eine Middleware gegen unautorisierte Subnetze geprüft (`validateVPNConnection`)[cite: 11, 12].
* **Systemd Enterprise Sandboxing:** Der Agent läuft als unprivilegierter Systemdienst unter strikten Kernel- und Dateisystem-Einschränkungen (`ProtectSystem=strict`, `NoNewPrivileges=true`, `MemoryDenyWriteExecute`)[cite: 11, 12].

---

## 🛠️ Technologie-Stack

* **Sprache:** Go 1.22+[cite: 12]
* **Systemintegration:** Systemd, WireGuard, `gopsutil`[cite: 11, 12]
* **Deployment:** Tarball-Release-Artefakte inklusive vordefinierter Systemd-Unit-Files[cite: 12]

---

## 📦 Installation als Systemd Service

1. Binärdatei kompilieren[cite: 12]:
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sentinel-agent cmd/agent/main.go
Nach /usr/local/bin/ verschieben und Systemd-Service einrichten[cite: 11, 12]:

Bash
sudo cp sentinel-agent /usr/local/bin/
sudo cp deployments/systemd/sentinel-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sentinel-agent.service

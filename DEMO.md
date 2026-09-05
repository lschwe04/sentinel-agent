# SentinelCore Demo

Diese Demo zeigt die Kette `Agent -> verschlüsselter Disk-Buffer -> GZIP -> Hub` lokal. Der lokale HTTP-Modus ist ausschließlich für diese Demo gedacht. Für Produktion bleiben mTLS, Zertifikatsprüfung und der systemd-Sandbox-Betrieb aktiv.

## Voraussetzungen

- Go passend zu `go.mod`
- Python 3
- PowerShell unter Windows oder Bash unter Linux

## Lokale Demo

1. Öffne ein Terminal im Repository und starte:

   ```powershell
   .\demo\run-demo.ps1 -DurationSeconds 30
   ```

   Das Skript startet den Mock-Hub auf `http://127.0.0.1:8080`, setzt einen 32-Byte-Demo-Schlüssel, führt Enrollment aus und sendet alle fünf Sekunden verschlüsselt gepufferte und GZIP-komprimierte Metriken.

2. Erwartete Ausgabe des Mock-Hubs enthält unter anderem:

   ```text
   received 1 metric(s)
   ```

3. Der lokale Zustand liegt unter `demo/state/buffer.dat`. Die Datei ist AES-256-GCM-verschlüsselt und wird nach erfolgreicher Übertragung geleert.

Alternativ manuell:

```powershell
python demo/mock_hub.py
$env:NODE_ID="demo-node"
$env:TENANT_ID="demo-tenant"
$env:CUSTOMER_ID="1"
$env:HUB_BASE_URL="http://127.0.0.1:8080"
$env:HUB_METRICS_URL="http://127.0.0.1:8080/api/v1/metrics"
$env:ENROLL_TOKEN="demo-token"
$env:AGENT_DEMO_MODE="true"
$env:AGENT_REPORT_INTERVAL_SECONDS="5"
$env:AGENT_STATE_DIR="$PWD/demo/state"
$env:AGENT_ENCRYPTION_KEY="01234567890123456789012345678901"
go run ./cmd/agent
```

## Tests

```powershell
go test ./...
```

Die Tests prüfen unter anderem AES-256-Schlüssellänge, Persistenz/Wiederherstellung, falsche Schlüssel und persistente FIM-Alarme.

## Rocky Linux 9 Präsentation

1. Baue das Linux-Binary auf Rocky Linux oder in einer passenden Build-Umgebung:

   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/sentinel-agent ./cmd/agent
   ```

2. Lege mTLS-Dateien als root ab:

   ```text
   /etc/sentinel/certs/agent.crt
   /etc/sentinel/certs/agent.key
   /etc/sentinel/certs/ca.crt
   ```

3. Installiere mit den echten Hub-Werten. Der Installer erzeugt automatisch einen zufälligen 32-Byte-Schlüssel, sofern `AGENT_ENCRYPTION_KEY` nicht gesetzt ist:

   ```bash
   sudo env HUB_BASE_URL=https://hub.example.com \
     ENROLL_TOKEN='aus-Secret-Store' TENANT_ID='systemhaus-demo' CUSTOMER_ID='1' \
     bash install.sh
   ```

4. Prüfe die Härtung:

   ```bash
   systemctl status sentinel-agent
   journalctl -u sentinel-agent -f
   systemd-analyze security sentinel-agent.service
   ```

   `ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`, Kernel-/Control-Group-Schutz und der beschränkte Schreibpfad für `/var/lib/sentinel` sind aktiv.

5. Für den Live-Nachweis trenne drei Szenen: erfolgreicher mTLS-Telemetrieversand, kurzzeitiger Hub-Ausfall mit Buffer-Aufbau und Wiederanbindung mit anschließendem Flush. Niemals den Demo-HTTP-Modus oder Testschlüssel in Kundensystemen verwenden.

## Bekannte bewusste Grenzen

Remote-Hardening, Reverse-SSH-Tunnel und signierte Self-Updates sind nicht Teil des lokalen Demo-Pfads. Diese Funktionen benötigen einen produktiven Hub-Vertrag, eine Host-Key-/Signaturverwaltung und eine definierte Freigabe-Policy; sie sollten nicht mit Mock-Daten vorgeführt werden.

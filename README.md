# SentinelCore Agent

SentinelCore ist ein Linux/systemd-Agent für Telemetrie, verschlüsseltes Offline-Puffern und mTLS-Übertragung an einen Hub. Die unterstützte Produktionsplattform ist Rocky Linux 9; Windows ist derzeit kein implementiertes Deployment-Ziel.

## Demo

Die lokale End-to-End-Demo mit Mock-Hub steht in [DEMO.md](DEMO.md). Sie benötigt nur Go und Python und zeigt Enrollment, AES-256-GCM-Disk-Buffer, GZIP-Batch und Telemetrie-Upload.

## Produktion

Der Installer ist [install.sh](install.sh). Er benötigt `HUB_BASE_URL`, `ENROLL_TOKEN`, `TENANT_ID`, `CUSTOMER_ID` sowie vor dem Dienststart gültige mTLS-Dateien unter `/etc/sentinel/certs/`. Ein zufälliger 32-Byte-Buffer-Schlüssel wird automatisch erzeugt und in `/etc/default/sentinel-agent` abgelegt.

```bash
sudo env HUB_BASE_URL=https://hub.example.com \
	ENROLL_TOKEN='aus-Secret-Store' TENANT_ID='systemhaus-demo' CUSTOMER_ID='1' \
	bash install.sh
```

Ansible-Deployment ist in [deployments/ansible/install-agent.yml](deployments/ansible/install-agent.yml) beschrieben. Secrets müssen über Ansible Vault oder einen externen Secret-Store als `vault_*`-Variablen geliefert werden.

## Validierung

```bash
go test ./...
```

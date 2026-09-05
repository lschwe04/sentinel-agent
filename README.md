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

## Identity, OTA und Diagnose

Die lokale Identity liegt unter `/etc/sentinel-agent/identity.json` und enthält `agent_id` sowie `tenant_id`. OTA-Updates benötigen zusätzlich einen Ed25519-Public-Key als Hexwert in `AGENT_UPDATE_PUBLIC_KEY`. Der Hub signiert dabei den rohen SHA-256-Digest des Binary:

```bash
openssl genpkey -algorithm ED25519 -out update-signing.key
openssl pkey -in update-signing.key -pubout -outform DER | tail -c 32 | xxd -p -c 64 > update-signing-public.hex
```

Der Public-Key wird über einen Secret-Store verteilt. Private Keys bleiben ausschließlich in der signierenden Hub-/Release-Pipeline. Die Signatur wird als Hex-Ed25519-Signatur im Update-Manifest übertragen; ohne gültige Signatur wird kein Binary ersetzt.

pprof ist ausschließlich über `/run/sentinel-agent-debug.sock` erreichbar. Der Socket hat `0600` und wird beim Context-gesteuerten Shutdown entfernt. Für den reproduzierbaren Race-Test steht `make test-race` bereit; die Ausführung erfolgt in `Dockerfile.test` mit Linux-GCC.

## Validierung

```bash
go test ./...
```

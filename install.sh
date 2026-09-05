#!/bin/bash
set -euo pipefail

if [ "$EUID" -ne 0 ]; then
  echo "[-] Fehler: Bitte als Root ausführen (sudo bash install.sh)."
  exit 1
fi

echo "[+] Starte Sentinel-Agent Installation..."

HUB_BASE_URL="${HUB_BASE_URL:-https://hub.yourdomain.com}"
ENROLL_TOKEN="${ENROLL_TOKEN:-}"
TENANT_ID="${TENANT_ID:-}"
CUSTOMER_ID="${CUSTOMER_ID:-}"
NODE_ID="${NODE_ID:-$(hostname)}"

if [ -z "$ENROLL_TOKEN" ] || [ -z "$TENANT_ID" ] || [ -z "$CUSTOMER_ID" ]; then
  echo "[-] Fehler: Die Variablen ENROLL_TOKEN, TENANT_ID und CUSTOMER_ID müssen gesetzt sein."
  echo "Beispiel: TENANT_ID=systemhaus-xy CUSTOMER_ID=1 ENROLL_TOKEN=xyz bash install.sh"
  exit 1
fi

INSTALL_DIR="/opt/sentinel-agent"
mkdir -p /etc/sentinel/certs "$INSTALL_DIR" /etc/default

# Automatisches Bauen des Binaries, falls der Go-Quellcode vorhanden ist
if [ -f "cmd/agent/main.go" ]; then
  echo "[*] Kompiliere Sentinel-Agent direkt ins Zielverzeichnis..."
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o "$INSTALL_DIR/sentinel-agent" cmd/agent/main.go
  chmod +x "$INSTALL_DIR/sentinel-agent"
else
  echo "[-] Warnung: 'cmd/agent/main.go' nicht gefunden. Stellen Sie sicher, dass das Binary manuell unter $INSTALL_DIR/sentinel-agent abgelegt wird."
fi

cat << EOF > /etc/default/sentinel-agent
NODE_ID=${NODE_ID}
TENANT_ID=${TENANT_ID}
CUSTOMER_ID=${CUSTOMER_ID}
HUB_BASE_URL=${HUB_BASE_URL}
ENROLL_TOKEN=${ENROLL_TOKEN}
HUB_METRICS_URL=${HUB_BASE_URL}/api/v1/metrics
AGENT_CERT_PATH=/etc/sentinel/certs/agent.crt
AGENT_KEY_PATH=/etc/sentinel/certs/agent.key
CA_CERT_PATH=/etc/sentinel/certs/ca.crt
EOF

chmod 600 /etc/default/sentinel-agent

cat << EOF > /etc/systemd/system/sentinel-agent.service
[Unit]
Description=SentinelCore Enterprise Security & Telemetry Agent
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/sentinel-agent
Restart=always
RestartSec=10

ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
MemoryDenyWriteExecute=true

EnvironmentFile=/etc/default/sentinel-agent

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable sentinel-agent

echo "[+] Sentinel-Agent erfolgreich eingerichtet!"
echo "[*] Vergessen Sie nicht, die mTLS-Zertifikate unter /etc/sentinel/certs/ zu hinterlegen."

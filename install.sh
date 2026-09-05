#!/bin/bash
set -euo pipefail

if [ "$EUID" -ne 0 ]; then
  echo "[-] Fehler: Bitte als Root ausführen (sudo bash install.sh)."
  exit 1
fi

echo "[+] Starte Sentinel-Agent Installation..."

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

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
mkdir -p /etc/sentinel/certs "$INSTALL_DIR" /etc/default /var/lib/sentinel

if ! command -v go >/dev/null 2>&1; then
  echo "[-] Fehler: Go wurde nicht gefunden. Bitte Go installieren und das Skript erneut ausführen."
  exit 1
fi

if [ ! -f "$SCRIPT_DIR/cmd/agent/main.go" ]; then
  echo "[-] Fehler: Go-Quellcode nicht gefunden unter $SCRIPT_DIR/cmd/agent/main.go"
  exit 1
fi

echo "[*] Kompiliere Sentinel-Agent direkt ins Zielverzeichnis..."
(
  cd "$SCRIPT_DIR"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o "$INSTALL_DIR/sentinel-agent" ./cmd/agent
)
chmod 755 "$INSTALL_DIR/sentinel-agent"

if [ ! -x "$INSTALL_DIR/sentinel-agent" ]; then
  echo "[-] Fehler: Das Binary wurde nicht ausführbar unter $INSTALL_DIR/sentinel-agent erzeugt."
  exit 1
fi

cat << EOF > /etc/default/sentinel-agent
NODE_ID=${NODE_ID}
TENANT_ID=${TENANT_ID}
CUSTOMER_ID=${CUSTOMER_ID}
HUB_BASE_URL=${HUB_BASE_URL}
ENROLL_TOKEN=${ENROLL_TOKEN}
HUB_METRICS_URL=${HUB_BASE_URL}/api/v1/metrics
AGENT_STATE_DIR=/var/lib/sentinel
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
StateDirectory=sentinel

EnvironmentFile=/etc/default/sentinel-agent

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now sentinel-agent

echo "[+] Sentinel-Agent erfolgreich eingerichtet!"
echo "[*] Vergessen Sie nicht, die mTLS-Zertifikate unter /etc/sentinel/certs/ zu hinterlegen."

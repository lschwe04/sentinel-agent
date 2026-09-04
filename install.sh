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

mkdir -p /etc/sentinel/certs /opt/sentinel /etc/default

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
WorkingDirectory=/opt/sentinel
ExecStart=/opt/sentinel/sentinel-agent
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
echo "[+] Sentinel-Agent erfolgreich eingerichtet! Starte den Service nach Platzieren der mTLS Zertifikate mit: systemctl start sentinel-agent"

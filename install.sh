#!/bin/bash
set -e

# Überprüfe Root-Rechte
if [ "$EUID" -ne 0 ]; then
  echo "[-] Bitte als Root ausführen (sudo bash install.sh)."
  exit 1
fi

echo "[+] Starte Sentinel-Agent Installation..."

# Variablen aus Umgebung oder Parametern
HUB_BASE_URL="${HUB_BASE_URL:-https://hub.yourdomain.com}"
ENROLL_TOKEN="${ENROLL_TOKEN}"
TENANT_ID="${TENANT_ID}"
CUSTOMER_ID="${CUSTOMER_ID}"
NODE_ID="${NODE_ID:-$(hostname)}"

if [ -z "$ENROLL_TOKEN" ] || [ -z "$TENANT_ID" ] || [ -z "$CUSTOMER_ID" ]; then
  echo "[-] Fehler: ENROLL_TOKEN, TENANT_ID und CUSTOMER_ID müssen gesetzt sein."
  echo "Beispiel: TENANT=haus CUSTOMER=1 TOKEN=xyz bash install.sh"
  exit 1
fi

# 1. Verzeichnisse anlegen
mkdir -p /etc/sentinel/certs
mkdir -p /opt/sentinel

# 2. Binary herunterladen (Platzhalter für deine Build-Pipeline)
echo "[+] Lade Agent-Binary herunter..."
# curl -sSL -o /opt/sentinel/sentinel-agent https://yourdomain.com/downloads/sentinel-agent
# chmod +x /opt/sentinel/sentinel-agent

# 3. Systemd Service mit Hardening anlegen
cat << EOF > /etc/systemd/system/sentinel-agent.service
[Unit]
Description=SentinelCore Enterprise Security & Telemetry Agent
After=network.target

[Service]
Type=simple
User=root
ExecStart=/opt/sentinel/sentinel-agent
Restart=always
RestartSec=10

# Hardening / Sandboxing
ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true
MemoryDenyWriteExecute=true

Environment=NODE_ID=${NODE_ID}
Environment=TENANT_ID=${TENANT_ID}
Environment=CUSTOMER_ID=${CUSTOMER_ID}
Environment=HUB_BASE_URL=${HUB_BASE_URL}
Environment=ENROLL_TOKEN=${ENROLL_TOKEN}
Environment=HUB_METRICS_URL=${HUB_BASE_URL}/api/v1/metrics
Environment=ENTERPRISE_AUTH_TOKEN=${ENROLL_TOKEN}

[Install]
WantedBy=multi-user.target
EOF

# 4. Systemd neu laden und Dienst starten
systemctl daemon-reload
systemctl enable sentinel-agent
systemctl restart sentinel-agent

echo "[+] Sentinel-Agent erfolgreich installiert und gestartet! Node-ID: ${NODE_ID}"

param(
    [int]$DurationSeconds = 30
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$hub = $null
$agent = $null

try {
    $hub = Start-Process -FilePath "python" -ArgumentList "demo/mock_hub.py" -WorkingDirectory $repo -PassThru
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        try {
            Invoke-RestMethod "http://127.0.0.1:8080/healthz" | Out-Null
            break
        } catch {
            Start-Sleep -Milliseconds 250
        }
    }

    $env:NODE_ID = "demo-node"
    $env:TENANT_ID = "demo-tenant"
    $env:CUSTOMER_ID = "1"
    $env:HUB_BASE_URL = "http://127.0.0.1:8080"
    $env:HUB_METRICS_URL = "http://127.0.0.1:8080/api/v1/metrics"
    $env:ENROLL_TOKEN = "demo-token"
    $env:AGENT_DEMO_MODE = "true"
    $env:AGENT_REPORT_INTERVAL_SECONDS = "5"
    $env:AGENT_STATE_DIR = Join-Path $repo "demo/state"
    $env:AGENT_ENCRYPTION_KEY = "01234567890123456789012345678901"

    New-Item -ItemType Directory -Force -Path $env:AGENT_STATE_DIR | Out-Null
    & go build -o (Join-Path $repo "demo/sentinel-agent-demo.exe") ./cmd/agent
    if ($LASTEXITCODE -ne 0) { throw "agent build failed" }
    Write-Host "Starting agent demo for $DurationSeconds seconds..."
    $agent = Start-Process -FilePath (Join-Path $repo "demo/sentinel-agent-demo.exe") -WorkingDirectory $repo -NoNewWindow -PassThru
    Start-Sleep -Seconds $DurationSeconds
}
finally {
    if ($agent -and -not $agent.HasExited) { Stop-Process -Id $agent.Id -Force }
    if ($hub -and -not $hub.HasExited) { Stop-Process -Id $hub.Id -Force }
}

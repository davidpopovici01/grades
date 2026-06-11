$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot -Parent
Set-Location $repo

$DATA_DIR = if ($args[0]) { $args[0] } else { "./data" }
$STATIC_DIR = "./portal-web/dist"

# Build frontend if needed
if (-not (Test-Path $STATIC_DIR)) {
    Write-Host "Building frontend..."
    Set-Location portal-web
    npm run build
    Set-Location $repo
}

# Build portal binary for Windows
Write-Host "Building portal binary..."
go build -o dist/portal-local.exe ./cmd/portal

# Generate test JWT secret
$SECRET_FILE = ".jwt-secret-local"
if (-not (Test-Path $SECRET_FILE)) {
    $secret = -join ((1..32) | ForEach-Object { [Convert]::ToBase64String([byte[]](Get-Random -Maximum 256))[0] })
    [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($secret)) | Set-Content $SECRET_FILE
    Write-Host "Generated test JWT secret: $SECRET_FILE"
}

# Publish grades if needed
if (-not (Test-Path "$DATA_DIR/accounts.json")) {
    Write-Host "Publishing grades to $DATA_DIR..."
    go run ./cmd/grades web publish $DATA_DIR
}

Write-Host ""
Write-Host "Starting local portal server..."
Write-Host "  Data dir:   $DATA_DIR"
Write-Host "  Static dir: $STATIC_DIR"
Write-Host "  URL:        http://localhost:8080"
Write-Host ""

$env:PORTAL_DATA_DIR = $DATA_DIR
$env:PORTAL_STATIC_DIR = $STATIC_DIR
$env:PORTAL_JWT_SECRET_FILE = $SECRET_FILE
$env:PORTAL_ADDR = "localhost:8080"
$env:PORTAL_COOKIE_SECURE = "false"

& ./dist/portal-local.exe

# SFTP-only deployment (no SSH required)
# Uploads data and static files. Server must already be running.

$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot -Parent
Set-Location $repo

$SERVER = if ($env:SERVER) { $env:SERVER } else { "user@singapore-vps" }
$REMOTE_DIR = "/opt/portal"

echo "Building frontend..."
Set-Location portal-web
npm run build
Set-Location $repo

echo "Publishing grades locally..."
go run . publish ./data

echo "Uploading to ${SERVER} via SFTP..."

# Use WinSCP or OpenSSH sftp
# This assumes OpenSSH sftp is available
$batch = @"
cd ${REMOTE_DIR}
put -r data
put -r portal-web/dist
bye
"@

$batch | sftp ${SERVER}

echo "Upload complete!"
echo "Note: The running server will pick up new data on next request."
echo "If you changed the Go binary, you need SSH to restart the service."

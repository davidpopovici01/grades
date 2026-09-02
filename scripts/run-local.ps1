# Run the student portal server locally for testing.
# Serves the React frontend from portal-web/dist with a throwaway SQLite DB.
$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot -Parent
Set-Location $repo

$STATIC_DIR = "./portal-web/dist"
$LOCAL_DIR = "./dist/portal-local"

# Build frontend if needed
if (-not (Test-Path $STATIC_DIR)) {
    Write-Host "Building frontend..."
    Push-Location portal-web
    try {
        npm ci
        npm run build
    }
    finally {
        Pop-Location
    }
}

New-Item -ItemType Directory -Force -Path $LOCAL_DIR | Out-Null

# Generate a test JWT secret and teacher token if not present
$SECRET_FILE = "$LOCAL_DIR/jwt-secret"
$TOKEN_FILE = "$LOCAL_DIR/teacher-token"
if (-not (Test-Path $SECRET_FILE)) {
    [Convert]::ToBase64String((1..32 | ForEach-Object { [byte](Get-Random -Maximum 256) })) | Set-Content $SECRET_FILE -NoNewline
}
if (-not (Test-Path $TOKEN_FILE)) {
    [Convert]::ToBase64String((1..32 | ForEach-Object { [byte](Get-Random -Maximum 256) })) | Set-Content $TOKEN_FILE -NoNewline
}
$TOKEN = (Get-Content $TOKEN_FILE -Raw).Trim()

Write-Host ""
Write-Host "Starting local portal server..."
Write-Host "  URL:           http://localhost:8080"
Write-Host "  Admin UI:      http://localhost:8080/admin"
Write-Host "  Teacher token: $TOKEN"
Write-Host "  Database:      $LOCAL_DIR/grades-portal.db"
Write-Host ""
Write-Host "To push grades to this server, set in ~/.grades/config.yaml:"
Write-Host "  portal.url: http://localhost:8080"
Write-Host "  portal.teacher_token: $TOKEN"
Write-Host "then run: grades publish"
Write-Host ""
Write-Host "For a quick preview without this server, use: grades web serve"
Write-Host ""

$env:PORTAL_DB_PATH = "$LOCAL_DIR/grades-portal.db"
$env:PORTAL_STATIC_DIR = $STATIC_DIR
$env:PORTAL_JWT_SECRET_FILE = $SECRET_FILE
$env:PORTAL_TEACHER_TOKEN_FILE = $TOKEN_FILE
$env:PORTAL_ADDR = "localhost:8080"
$env:PORTAL_COOKIE_SECURE = "false"

go run ./cmd/portal

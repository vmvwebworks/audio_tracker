# Comprobaciones que tienen que pasar antes de cada commit o PR (las mismas
# que ejecuta el CI): formato, vet, tests y compilacion.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$failed = $false

function Step($name, [scriptblock]$cmd) {
    Write-Host "== $name" -ForegroundColor Cyan
    & $cmd
    if ($LASTEXITCODE -ne 0) { $script:failed = $true; Write-Host "   FALLO: $name" -ForegroundColor Red }
}

Step "gofmt" {
    $bad = gofmt -l .
    if ($bad) { $bad | ForEach-Object { Write-Host "   sin formato: $_" }; $global:LASTEXITCODE = 1 } else { $global:LASTEXITCODE = 0 }
}
Step "go vet" { go vet ./... }
Step "go test" { go test ./... }
Step "go build" { go build -ldflags="-H windowsgui" -o "$env:TEMP\audio_tracker_check.exe" . }

if ($failed) { Write-Host "Hay fallos." -ForegroundColor Red; exit 1 }
Write-Host "Todo OK." -ForegroundColor Green

# Prepares the development environment: checks Go, verifies the bundled
# SoundFont and downloads the Go modules. Safe to run any number of times.
# (Only for developers: users just download AudioTracker.exe.)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$sfPath = Join-Path $root "assets\GeneralUser-GS.sf2"
$sfHash = "9575028C7A1F589F5770FCCC8CFF2734566AF40CD26ED836944E9A5152688CFE"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "No se encuentra Go. Instalalo desde https://go.dev/dl/ (version indicada en go.mod)."
}
$need = (Select-String -Path go.mod -Pattern '^go (\S+)').Matches[0].Groups[1].Value
Write-Host "Go: $(go env GOVERSION) (go.mod pide $need)"

# The SoundFont is part of the repository and gets embedded in the .exe.
if (-not (Test-Path $sfPath)) {
    throw "Falta $sfPath. Esta en el repositorio: ejecuta git checkout -- assets"
}
$h = (Get-FileHash $sfPath -Algorithm SHA256).Hash
if ($h -ne $sfHash) { Write-Warning "La SoundFont no coincide con la esperada ($h)." }
else { Write-Host "SoundFont: OK" }

go mod download
Write-Host "Listo. Siguiente paso: scripts\check.ps1 (tests) o scripts\run.ps1 (abrir la app)."

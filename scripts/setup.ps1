# Prepara el entorno de desarrollo: comprueba Go, descarga la SoundFont y las
# dependencias. Es idempotente: se puede ejecutar tantas veces como haga falta.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$sfUrl  = "https://github.com/mrbumpy409/GeneralUser-GS/raw/main/GeneralUser-GS.sf2"
$sfPath = Join-Path $root "assets\GeneralUser-GS.sf2"
$sfHash = "9575028C7A1F589F5770FCCC8CFF2734566AF40CD26ED836944E9A5152688CFE"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "No se encuentra Go. Instálalo desde https://go.dev/dl/ (versión indicada en go.mod)."
}
$need = (Select-String -Path go.mod -Pattern '^go (\S+)').Matches[0].Groups[1].Value
Write-Host "Go: $(go env GOVERSION) (go.mod pide $need)"

if (Test-Path $sfPath) {
    $h = (Get-FileHash $sfPath -Algorithm SHA256).Hash
    if ($h -ne $sfHash) { Write-Warning "La SoundFont no coincide con la esperada ($h). Bórrala y vuelve a ejecutar este script si da problemas." }
    else { Write-Host "SoundFont: OK" }
} else {
    Write-Host "Descargando la SoundFont GeneralUser GS (32 MB)..."
    New-Item -ItemType Directory -Force (Split-Path $sfPath) | Out-Null
    $ProgressPreference = "SilentlyContinue"
    Invoke-WebRequest -Uri $sfUrl -OutFile "$sfPath.part"
    $h = (Get-FileHash "$sfPath.part" -Algorithm SHA256).Hash
    if ($h -ne $sfHash) {
        Remove-Item "$sfPath.part"
        throw "La SoundFont descargada no coincide (SHA-256 $h). Revisa sfUrl/sfHash en scripts/setup.ps1."
    }
    Move-Item "$sfPath.part" $sfPath -Force
    Write-Host "SoundFont: descargada"
}

go mod download
Write-Host "Listo. Siguiente paso: scripts\check.ps1 (tests) o scripts\run.ps1 (abrir la app)."

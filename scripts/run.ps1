<#
.SYNOPSIS
  Compila y abre la app.
.PARAMETER Sandbox
  Usa una configuracion temporal (APPDATA aislado) y abre la partitura de
  prueba, sin tocar la configuracion ni las tomas del usuario. Es lo que deben
  usar los agentes para probar cambios de interfaz. Imprime el PID.
.PARAMETER Song
  Partitura .gp que abrir en modo Sandbox (por defecto, la de prueba).
.PARAMETER Console
  Compila sin -H windowsgui para ver los logs en la terminal.
#>
# Keep this file ASCII: Windows PowerShell 5.1 reads BOM-less scripts as ANSI.
param([switch]$Sandbox, [string]$Song = "", [switch]$Console)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$exe = Join-Path $root "audio_tracker.exe"
$buildArgs = @("build")
if (-not $Console) { $buildArgs += "-ldflags=-H windowsgui" }
$buildArgs += @("-o", $exe, ".")
# If the app is running Windows locks the .exe, but it can still be renamed.
if (Test-Path $exe) { try { [IO.File]::OpenWrite($exe).Close() } catch { Move-Item $exe "$exe.old" -Force } }
go @buildArgs
if ($LASTEXITCODE -ne 0) { exit 1 }

if (-not $Sandbox) {
    if ($Console) { & $exe } else { Start-Process $exe -WorkingDirectory $root }
    return
}

$box = Join-Path $env:TEMP "audio_tracker_sandbox"
$cfgDir = Join-Path $box "appdata\audio_tracker"
New-Item -ItemType Directory -Force $cfgDir | Out-Null
if (-not $Song) {
    # Package the test score as a .gp (a zip holding Content/score.gpif).
    $Song = Join-Path $box "Prueba minima.gp"
    Remove-Item $Song -ErrorAction SilentlyContinue
    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::Open($Song, "Create")
    try {
        $w = New-Object IO.StreamWriter($zip.CreateEntry("Content/score.gpif").Open())
        $w.Write([IO.File]::ReadAllText((Join-Path $root "internal\gp\testdata\minimal.gpif")))
        $w.Close()
    } finally { $zip.Dispose() }
}
$cfg = @{ driver = "ASIO4ALL v2"; last_file = (Resolve-Path $Song).Path; out_r = 1 } | ConvertTo-Json
[IO.File]::WriteAllText((Join-Path $cfgDir "config.json"), $cfg)

# The app reads its config from %APPDATA%; the child inherits this override.
$saved = $env:APPDATA
$env:APPDATA = Join-Path $box "appdata"
try { $p = Start-Process $exe -WorkingDirectory $root -PassThru } finally { $env:APPDATA = $saved }
Write-Host "Sandbox en $box (PID $($p.Id))"

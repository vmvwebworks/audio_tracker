<#
.SYNOPSIS
  Drives a running app window for manual/agent UI checks.
.EXAMPLE
  scripts\ui.ps1 -ProcessId 1234 -Action shot -Out captura.png
  scripts\ui.ps1 -ProcessId 1234 -Action click -X 120 -Y 440
  scripts\ui.ps1 -ProcessId 1234 -Action key -Keys "^a0:12,5~"
.NOTES
  X/Y are relative to the window's top-left corner (title bar included).
  -Keys uses SendKeys syntax: ~ = Enter, ^ = Ctrl, {ESC}, {HOME}, " " = Space.
  -Out is resolved against the current directory unless absolute.
  Always pass -ProcessId: the user may have their own instance open.
#>
param(
    [Parameter(Mandatory)][int]$ProcessId,
    [ValidateSet("shot", "click", "key")][string]$Action = "shot",
    [int]$X = 0, [int]$Y = 0,
    [string]$Keys = "",
    [string]$Out = "captura.png"
)
$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
if (-not ("AtWin" -as [type])) {
    Add-Type @"
using System; using System.Runtime.InteropServices;
public class AtWin {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint x, uint y, int d, IntPtr e);
  public struct RECT { public int L, T, R, B; }
}
"@
}

$p = Get-Process -Id $ProcessId
for ($i = 0; $i -lt 40 -and $p.MainWindowHandle -eq 0; $i++) { Start-Sleep -Milliseconds 250; $p.Refresh() }
$h = $p.MainWindowHandle
if ($h -eq 0) { throw "El proceso $ProcessId no tiene ventana" }
[AtWin]::SetForegroundWindow($h) | Out-Null
Start-Sleep -Milliseconds 250
$r = New-Object AtWin+RECT
[AtWin]::GetWindowRect($h, [ref]$r) | Out-Null

switch ($Action) {
    "click" {
        [AtWin]::SetCursorPos($r.L + $X, $r.T + $Y) | Out-Null
        Start-Sleep -Milliseconds 80
        [AtWin]::mouse_event(2, 0, 0, 0, [IntPtr]::Zero); Start-Sleep -Milliseconds 60
        [AtWin]::mouse_event(4, 0, 0, 0, [IntPtr]::Zero)
    }
    "key" { [System.Windows.Forms.SendKeys]::SendWait($Keys) }
    "shot" {
        $path = $Out
        if (-not [IO.Path]::IsPathRooted($Out)) { $path = Join-Path (Get-Location).Path $Out }
        $bmp = New-Object Drawing.Bitmap ($r.R - $r.L), ($r.B - $r.T)
        $g = [Drawing.Graphics]::FromImage($bmp)
        $hdc = $g.GetHdc()
        [AtWin]::PrintWindow($h, $hdc, 2) | Out-Null
        $g.ReleaseHdc($hdc)
        $bmp.Save($path)
        Write-Host $path
    }
}

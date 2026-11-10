# One-time ONLINE fetch (Windows): vendor PlatformIO packages + libraries so
# later builds run fully offline. Re-run to update.
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$env:PLATFORMIO_CORE_DIR = "$root\vendor\platformio"
pio run -e cardputer-adv
pio pkg list -e cardputer-adv | Out-File offline\resolved-versions.txt
Write-Host "Offline bundle ready. Build later with:"
Write-Host '  $env:PLATFORMIO_CORE_DIR = "$PWD\vendor\platformio"; pio run'

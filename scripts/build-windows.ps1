# Builds the portable Windows folder: the window plus a frozen data layer,
# so it runs on a machine with no Python and no Go installed.
#
#   powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
#
# Output: dist\value-screener\value-screener.exe and its data layer.
# The venv must exist and have futu-api, protobuf<4 and pyinstaller.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$python = Join-Path $root ".venv\Scripts\python.exe"
if (-not (Test-Path $python)) {
    throw "No .venv: run `python -m venv .venv` and install futu-api, 'protobuf<4' and pyinstaller first."
}

$dist = Join-Path $root "dist\value-screener"
$work = Join-Path $root "build"
Remove-Item -Recurse -Force $dist, $work -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Write-Host "Freezing the data layer..."
& $python -m PyInstaller `
    --noconfirm --onefile --console `
    --name value-screener-dl `
    --paths . `
    --collect-all futu `
    --collect-all google `
    --distpath $dist `
    --workpath $work `
    --specpath $work `
    scripts\datalayer_entry.py
if ($LASTEXITCODE -ne 0) { throw "PyInstaller failed" }

Write-Host "Building the window..."
$env:CGO_ENABLED = "0"
& go build -trimpath `
    -tags "mygo_noinspector" `
    -ldflags "-s -w -H=windowsgui -X github.com/egoist/mygo.production=1" `
    -o (Join-Path $dist "value-screener.exe") .
if ($LASTEXITCODE -ne 0) { throw "go build failed" }

Copy-Item "criteria.example.json" (Join-Path $dist "criteria.json")
Copy-Item "README.md" $dist

Write-Host ""
Write-Host "Done: $dist"
Get-ChildItem $dist | Format-Table Name, Length

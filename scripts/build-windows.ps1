# Builds the Windows app: one executable with the data layer packed inside
# it, and an installer that wraps it.
#
#   powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
#
# Output:
#   dist\value-screener.exe                     run it where it lies, nothing
#                                               to install
#   dist\Value screen Setup 1.0.0 amd64.exe     the same app as a per-user
#                                               install with shortcuts and an
#                                               uninstaller
#
# The data layer unpacks itself into the user's cache the first time a run
# starts, so neither needs Python or Go on the machine that runs it.
#
# The venv must exist and have futu-api, protobuf<4 and pyinstaller.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$python = Join-Path $root ".venv\Scripts\python.exe"
if (-not (Test-Path $python)) {
    throw "No .venv: run `python -m venv .venv` and install futu-api, 'protobuf<4' and pyinstaller first."
}

$dist = Join-Path $root "dist"
$work = Join-Path $root "build"
$bin = Join-Path $root "app\datalayer.bin"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Write-Host "Freezing the data layer..."
& $python -m PyInstaller `
    --noconfirm --onefile --console `
    --name value-screener-dl `
    --paths . `
    --collect-all futu `
    --collect-all google `
    --distpath $work `
    --workpath (Join-Path $work "pyinstaller") `
    --specpath $work `
    scripts\datalayer_entry.py
if ($LASTEXITCODE -ne 0) { throw "PyInstaller failed" }

# The frozen layer is packed into the executable as a resource, so a release
# is one file.
Move-Item -Force (Join-Path $work "value-screener-dl.exe") $bin
Write-Host "  $([math]::Round((Get-Item $bin).Length / 1MB)) MB"

Write-Host "Building the app..."
$env:CGO_ENABLED = "0"
# mygo build reads the tags of its own from GOFLAGS, which is where the
# embedded data layer is asked for.
$env:GOFLAGS = "-tags=datalayer_embedded"
& go run github.com/egoist/mygo/cmd/mygo@v0.6.3 build -o $dist
$code = $LASTEXITCODE
Remove-Item $bin -ErrorAction SilentlyContinue
Remove-Item Env:\GOFLAGS
if ($code -ne 0) { throw "mygo build failed" }

# The build's own folder, flattened into the names a release is read by.
$stage = Join-Path $dist "windows-amd64"
Move-Item -Force (Join-Path $stage "Value screen.exe") (Join-Path $dist "value-screener.exe")
Move-Item -Force (Join-Path $stage "Value screen Setup 1.0.0 amd64.exe") (Join-Path $dist "Value screen Setup 1.0.0 amd64.exe")
Remove-Item -Recurse -Force $stage

Write-Host ""
Write-Host "Done: $dist"
Get-ChildItem $dist | Format-Table Name, Length

[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$RuntimeDir,
    [Parameter(Mandatory)][string]$RuntimeLicense,
    [ValidatePattern('^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$')][string]$Version = '0.1.0',
    [string]$ISCC = 'ISCC.exe'
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$runtime = (Resolve-Path -LiteralPath $RuntimeDir).Path
$license = (Resolve-Path -LiteralPath $RuntimeLicense).Path
if (-not (Test-Path -LiteralPath (Join-Path $runtime 'llama-server.exe') -PathType Leaf)) {
    throw 'RuntimeDir must contain llama-server.exe and all required DLLs.'
}
$compiler = Get-Command $ISCC -ErrorAction SilentlyContinue
if (-not $compiler) { throw 'Install Inno Setup 6.3+ and supply -ISCC <path to ISCC.exe>.' }
# A fresh, explicit allowlist prevents local config, keys, models and build
# leftovers from entering a release. No recursive copy of the repository.
$stage = Join-Path $repo ('dist\staging-' + [guid]::NewGuid().ToString('N'))
$stageRuntime = Join-Path $stage 'runtime'
New-Item -ItemType Directory -Path $stageRuntime -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $repo 'LICENSE') -Destination $stage
Copy-Item -LiteralPath (Join-Path $repo 'THIRD_PARTY_NOTICES.md') -Destination $stage
& (Join-Path $PSScriptRoot 'collect-licenses.ps1') -Destination (Join-Path $stage 'licenses')
Copy-Item -LiteralPath (Join-Path $runtime 'llama-server.exe') -Destination $stageRuntime
Get-ChildItem -LiteralPath $runtime -Filter '*.dll' -File | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination $stageRuntime
}
Copy-Item -LiteralPath $license -Destination (Join-Path $stageRuntime 'LICENSE-llama.cpp.txt')
if (Test-Path -LiteralPath (Join-Path $runtime 'licenses') -PathType Container) {
    Copy-Item -LiteralPath (Join-Path $runtime 'licenses') -Destination $stageRuntime -Recurse
}
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
Push-Location $repo
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & go build -buildvcs=false -trimpath -ldflags "-H=windowsgui -X main.version=v$Version" -o (Join-Path $stage 'managed-llama.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    & (Join-Path $PSScriptRoot 'test-release-version.ps1') -Binary (Join-Path $stage 'managed-llama.exe') -ExpectedVersion "v$Version"
    & $compiler.Source "/DAppVersion=$Version" "/DPayloadDir=$stage" (Join-Path $repo 'installer\managed-llama.iss')
    if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed.' }
    $output = Join-Path $repo "dist\ManagedLlamaSetup-$Version-windows-amd64.exe"
    $hash = Get-FileHash -Algorithm SHA256 -LiteralPath $output
    [IO.File]::WriteAllText($output + '.sha256', ($hash.Hash.ToLowerInvariant() + '  ' + [IO.Path]::GetFileName($output) + "`n"), [Text.UTF8Encoding]::new($false))
    $hash
} finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    Pop-Location
}

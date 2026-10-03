[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidatePattern('^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$')][string]$Version
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$output = Join-Path $repo "dist\release-v$Version"
New-Item -ItemType Directory -Path $output -Force | Out-Null
$oldGOOS, $oldGOARCH = $env:GOOS, $env:GOARCH
Push-Location $repo
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $binary = Join-Path $output 'managed-llama-windows-amd64.exe'
    & go build -buildvcs=false -trimpath -ldflags "-H=windowsgui -X main.version=v$Version" -o $binary .
    if ($LASTEXITCODE -ne 0) { throw 'Release build failed.' }
    & (Join-Path $PSScriptRoot 'test-release-version.ps1') -Binary $binary -ExpectedVersion "v$Version"
    foreach ($name in @('LICENSE', 'README.md', 'SECURITY.md', 'THIRD_PARTY_NOTICES.md')) {
        Copy-Item -LiteralPath (Join-Path $repo $name) -Destination $output
    }
    & (Join-Path $PSScriptRoot 'collect-licenses.ps1') -Destination (Join-Path $output 'licenses')
    $hash = Get-FileHash -Algorithm SHA256 -LiteralPath $binary
    [IO.File]::WriteAllText($binary + '.sha256', ($hash.Hash.ToLowerInvariant() + '  ' + [IO.Path]::GetFileName($binary) + "`n"), [Text.UTF8Encoding]::new($false))
    $hash
} finally {
    $env:GOOS, $env:GOARCH = $oldGOOS, $oldGOARCH
    Pop-Location
}

[CmdletBinding()]
param([Parameter(Mandatory)][string]$Destination)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
New-Item -ItemType Directory -Path $Destination -Force | Out-Null
$destinationPath = (Resolve-Path -LiteralPath $Destination).Path
Push-Location $repo
try {
    & go mod download
    if ($LASTEXITCODE -ne 0) { throw 'Dependency download failed.' }
    $modules = & go list -buildvcs=false -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}' .
    if ($LASTEXITCODE -ne 0) { throw 'Dependency inventory failed.' }
    $inventory = @()
    foreach ($module in ($modules | Sort-Object -Unique)) {
        if ([string]::IsNullOrWhiteSpace($module)) { continue }
        $parts = $module.Split('|')
        if ($parts.Count -ne 3 -or -not $parts[2]) { throw "Missing module directory: $module" }
        $notices = @(Get-ChildItem -LiteralPath $parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE|PATENTS)(\..*)?$' })
        if (-not ($notices | Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING)' })) {
            throw "No license found for $($parts[0]); review before distributing."
        }
        $folder = Join-Path $destinationPath (($parts[0] -replace '[/\\]', '_') + '@' + $parts[1])
        New-Item -ItemType Directory -Path $folder -Force | Out-Null
        foreach ($notice in $notices) { Copy-Item -LiteralPath $notice.FullName -Destination $folder -Force }
        $inventory += "$($parts[0]) $($parts[1])"
    }
    $goRoot = & go env GOROOT
    if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Go toolchain license.' }
    Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $destinationPath 'LICENSE-Go.txt')
    Copy-Item -LiteralPath (Join-Path $goRoot 'PATENTS') -Destination (Join-Path $destinationPath 'PATENTS-Go.txt')
    $inventory | Set-Content -LiteralPath (Join-Path $destinationPath 'modules.txt') -Encoding utf8
} finally { Pop-Location }

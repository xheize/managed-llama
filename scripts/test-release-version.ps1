[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Binary,
    [Parameter(Mandatory)][string]$ExpectedVersion
)
$ErrorActionPreference = 'Stop'
$info = [Diagnostics.ProcessStartInfo]::new()
$info.FileName = (Resolve-Path -LiteralPath $Binary).Path
$info.Arguments = '-version'
$info.UseShellExecute = $false
$info.CreateNoWindow = $true
$info.RedirectStandardOutput = $true
$info.RedirectStandardError = $true
$process = [Diagnostics.Process]::new()
$process.StartInfo = $info
try {
    if (-not $process.Start()) { throw 'Version check did not start.' }
    if (-not $process.WaitForExit(10000)) { $process.Kill(); throw 'Version check timed out.' }
    $actual = $process.StandardOutput.ReadToEnd().Trim()
    $errorOutput = $process.StandardError.ReadToEnd()
    if ($process.ExitCode -ne 0 -or $actual -ne $ExpectedVersion) {
        throw "Version check failed: expected '$ExpectedVersion', got '$actual'. $errorOutput"
    }
} finally { $process.Dispose() }

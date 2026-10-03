param(
    [string]$SourcePath = (Join-Path $PSScriptRoot 'managed-local-llm-tray-source.png')
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$sizes = @(16, 20, 24, 32, 40, 48, 64, 128, 256)

function New-ThemedBitmap {
    param(
        [System.Drawing.Bitmap]$Source,
        [System.Drawing.Color]$Foreground
    )

    $result = [System.Drawing.Bitmap]::new($Source.Width, $Source.Height, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $matrix = [System.Drawing.Imaging.ColorMatrix]::new()
    $matrix.Matrix00 = 0; $matrix.Matrix01 = 0; $matrix.Matrix02 = 0
    $matrix.Matrix10 = 0; $matrix.Matrix11 = 0; $matrix.Matrix12 = 0
    $matrix.Matrix20 = 0; $matrix.Matrix21 = 0; $matrix.Matrix22 = 0
    # Suppress faint generation residue while keeping antialiased edges crisp.
    $matrix.Matrix33 = 2.5
    $matrix.Matrix43 = -0.5
    $matrix.Matrix40 = $Foreground.R / 255.0
    $matrix.Matrix41 = $Foreground.G / 255.0
    $matrix.Matrix42 = $Foreground.B / 255.0

    $attributes = [System.Drawing.Imaging.ImageAttributes]::new()
    $attributes.SetColorMatrix($matrix)
    $graphics = [System.Drawing.Graphics]::FromImage($result)
    $graphics.Clear([System.Drawing.Color]::Transparent)
    $rect = [System.Drawing.Rectangle]::new(0, 0, $Source.Width, $Source.Height)
    $graphics.DrawImage($Source, $rect, 0, 0, $Source.Width, $Source.Height, [System.Drawing.GraphicsUnit]::Pixel, $attributes)
    $graphics.Dispose()
    $attributes.Dispose()
    return $result
}

function Resize-IconBitmap {
    param(
        [System.Drawing.Bitmap]$Source,
        [int]$Size
    )

    $result = [System.Drawing.Bitmap]::new($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $graphics = [System.Drawing.Graphics]::FromImage($result)
    $graphics.Clear([System.Drawing.Color]::Transparent)
    $graphics.CompositingMode = [System.Drawing.Drawing2D.CompositingMode]::SourceCopy
    $graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
    $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $graphics.DrawImage($Source, 0, 0, $Size, $Size)
    $graphics.Dispose()
    return $result
}

function Save-MultiSizeIcon {
    param(
        [System.Drawing.Bitmap]$Source,
        [string]$OutputPath
    )

    $frames = @()
    foreach ($size in $sizes) {
        $bitmap = Resize-IconBitmap -Source $Source -Size $size
        $stream = [System.IO.MemoryStream]::new()
        $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
        $frames += ,@($size, $stream.ToArray())
        $bitmap.Dispose()
        $stream.Dispose()
    }

    $file = [System.IO.File]::Open($OutputPath, [System.IO.FileMode]::Create)
    $writer = [System.IO.BinaryWriter]::new($file)
    $writer.Write([uint16]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]$frames.Count)

    $offset = 6 + (16 * $frames.Count)
    foreach ($frame in $frames) {
        $size = [int]$frame[0]
        $bytes = [byte[]]$frame[1]
        $dimensionByte = if ($size -eq 256) { [byte]0 } else { [byte]$size }
        $writer.Write($dimensionByte)
        $writer.Write($dimensionByte)
        $writer.Write([byte]0)
        $writer.Write([byte]0)
        $writer.Write([uint16]1)
        $writer.Write([uint16]32)
        $writer.Write([uint32]$bytes.Length)
        $writer.Write([uint32]$offset)
        $offset += $bytes.Length
    }

    foreach ($frame in $frames) {
        $writer.Write([byte[]]$frame[1])
    }

    $writer.Dispose()
    $file.Dispose()
}

$source = [System.Drawing.Bitmap]::FromFile($SourcePath)
$master = Resize-IconBitmap -Source $source -Size 256
$light = New-ThemedBitmap -Source $master -Foreground ([System.Drawing.Color]::FromArgb(255, 18, 21, 27))
$dark = New-ThemedBitmap -Source $master -Foreground ([System.Drawing.Color]::FromArgb(255, 242, 246, 250))

$lightPreview = Resize-IconBitmap -Source $light -Size 256
$darkPreview = Resize-IconBitmap -Source $dark -Size 256
$lightPreview.Save((Join-Path $PSScriptRoot 'managed-local-llm-tray-light-preview.png'), [System.Drawing.Imaging.ImageFormat]::Png)
$darkPreview.Save((Join-Path $PSScriptRoot 'managed-local-llm-tray-dark-preview.png'), [System.Drawing.Imaging.ImageFormat]::Png)

Save-MultiSizeIcon -Source $light -OutputPath (Join-Path $PSScriptRoot 'managed-local-llm-tray-light.ico')
Save-MultiSizeIcon -Source $dark -OutputPath (Join-Path $PSScriptRoot 'managed-local-llm-tray-dark.ico')

$lightPreview.Dispose()
$darkPreview.Dispose()
$light.Dispose()
$dark.Dispose()
$master.Dispose()
$source.Dispose()

Write-Output 'Tray icon assets generated.'

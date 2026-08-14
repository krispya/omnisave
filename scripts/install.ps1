# Installs the omnisave client on Windows.
#
#   irm https://raw.githubusercontent.com/krispya/omnisave/main/scripts/install.ps1 | iex
#
# The script is run by piping it into Invoke-Expression, so it executes in the
# caller's session: the work lives in a function and reports failure by
# throwing, because a bare `exit` would close the user's shell.
#
#   $env:OMNISAVE_VERSION        version to install, without the leading v (default: latest)
#   $env:OMNISAVE_BIN            directory to install into (default: %LOCALAPPDATA%\omnisave)
#   $env:OMNISAVE_NO_MODIFY_PATH set to any value to leave the user PATH alone

function Install-Omnisave {
    [CmdletBinding()]
    param()

    $ErrorActionPreference = 'Stop'

    $repo = if ($env:OMNISAVE_REPO) { $env:OMNISAVE_REPO } else { 'krispya/omnisave' }
    $installDir = if ($env:OMNISAVE_BIN) { $env:OMNISAVE_BIN } else { Join-Path $env:LOCALAPPDATA 'omnisave' }
    $version = if ($env:OMNISAVE_VERSION) { $env:OMNISAVE_VERSION } else { 'latest' }

    # Windows PowerShell 5.1 still negotiates TLS 1.0 by default, which GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'x86' { throw 'omnisave: 32-bit Windows is not supported' }
        'ARM64' { throw 'omnisave: Windows on ARM is not published yet; run the amd64 build under emulation or open an issue' }
        default { throw "omnisave: unsupported architecture $($env:PROCESSOR_ARCHITECTURE)" }
    }

    if ($version -eq 'latest') {
        # The releases API names the newest published tag. Set
        # $env:OMNISAVE_VERSION to skip this request entirely.
        try {
            $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing
        } catch {
            throw "omnisave: could not reach the GitHub releases API; set `$env:OMNISAVE_VERSION to install a known version"
        }
        if (-not $release.tag_name) { throw "omnisave: no published release found for $repo" }
        $version = $release.tag_name -replace '^v', ''
    }

    $archive = "omnisave-$version-windows-$arch.zip"
    $checksums = "omnisave-$version-checksums.txt"
    $baseUrl = "https://github.com/$repo/releases/download/v$version"

    Write-Host "Installing omnisave $version for windows/$arch"

    $work = Join-Path ([IO.Path]::GetTempPath()) ("omnisave-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $work -Force | Out-Null
    try {
        $archivePath = Join-Path $work $archive
        $checksumPath = Join-Path $work $checksums
        try {
            Invoke-WebRequest -Uri "$baseUrl/$archive" -OutFile $archivePath -UseBasicParsing
            Invoke-WebRequest -Uri "$baseUrl/$checksums" -OutFile $checksumPath -UseBasicParsing
        } catch {
            throw "omnisave: could not download $archive from release v$version"
        }

        $expected = (Get-Content $checksumPath |
            Where-Object { $_ -match "\s$([regex]::Escape($archive))$" } |
            ForEach-Object { ($_ -split '\s+')[0] } |
            Select-Object -First 1)
        if (-not $expected) { throw "omnisave: $archive is not listed in $checksums" }

        $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash
        if ($actual -ne $expected.ToUpperInvariant()) {
            throw "omnisave: checksum mismatch for $archive; refusing to install"
        }

        Expand-Archive -Path $archivePath -DestinationPath $work -Force
        $binary = Join-Path $work 'omnisave.exe'
        if (-not (Test-Path $binary)) { throw "omnisave: $archive did not contain omnisave.exe" }

        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        $target = Join-Path $installDir 'omnisave.exe'
        # Windows locks a running executable, so a replacement fails while a
        # watch is running. Renaming the old file out of the way first lets the
        # install succeed; the stale copy is removed on the next run.
        if (Test-Path $target) {
            $stale = Join-Path $installDir ('omnisave.exe.old-' + [Guid]::NewGuid().ToString('N'))
            try { Move-Item -Path $target -Destination $stale -Force } catch {
                throw "omnisave: $target is in use; stop 'omnisave watch' and run this again"
            }
        }
        Get-ChildItem -Path $installDir -Filter 'omnisave.exe.old-*' -ErrorAction SilentlyContinue |
            ForEach-Object { Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue }
        Move-Item -Path $binary -Destination $target -Force

        Write-Host "Installed $target"
    } finally {
        Remove-Item -Path $work -Recurse -Force -ErrorAction SilentlyContinue
    }

    # ── PATH ──────────────────────────────────────────────────────────────────

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $onPath = ($userPath -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ieq $installDir.TrimEnd('\')) })
    if ($onPath) { return }

    if ($env:OMNISAVE_NO_MODIFY_PATH) {
        Write-Host ''
        Write-Host "$installDir is not on your PATH. Add it with:"
        Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"$installDir;`$env:Path`", 'User')"
        return
    }

    $updated = if ([string]::IsNullOrEmpty($userPath)) { $installDir } else { "$userPath;$installDir" }
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    $env:Path = "$env:Path;$installDir"

    Write-Host ''
    Write-Host "Added $installDir to your user PATH"
    Write-Host 'Open a new terminal to pick it up.'
}

Install-Omnisave

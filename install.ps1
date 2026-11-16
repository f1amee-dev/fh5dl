# Install the latest fh5dl release on Windows:
#   irm https://raw.githubusercontent.com/f1amee-dev/fh5dl/main/install.ps1 | iex
# Set FH5DL_NO_OPEN=1 to skip opening a new PowerShell window afterwards.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$repo = 'f1amee-dev/fh5dl'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "fh5dl_windows_$arch.zip"
$url = "https://github.com/$repo/releases/latest/download"
$dir = Join-Path $env:LOCALAPPDATA 'Programs\fh5dl'
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $tmp, $dir | Out-Null

try {
    Write-Host "Downloading $asset..."
    Invoke-WebRequest "$url/$asset" -OutFile "$tmp\$asset" -UseBasicParsing
    Invoke-WebRequest "$url/checksums.txt" -OutFile "$tmp\checksums.txt" -UseBasicParsing
    $line = Get-Content "$tmp\checksums.txt" | Where-Object { $_ -match " $([regex]::Escape($asset))$" }
    $want = ($line -split ' ')[0]
    $got = (Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash
    if (-not $want -or $want -ne $got.ToLower()) { throw 'checksum mismatch, not installing' }

    Expand-Archive "$tmp\$asset" -DestinationPath $tmp -Force
    Copy-Item "$tmp\fh5dl.exe" "$dir\fh5dl.exe" -Force
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir".TrimStart(';'), 'User')
}
if (($env:Path -split ';') -notcontains $dir) { $env:Path += ";$dir" }
Write-Host "Installed $(& "$dir\fh5dl.exe" --version) to $dir"

# A ready-to-use PowerShell window: a Start menu shortcut, and one opened now.
$welcome = "fh5dl --help; Write-Host; Write-Host 'fh5dl is ready. Type fh5dl and a space, paste a link, press Enter.'"
# The Downloads known folder, which can be redirected (e.g. to OneDrive).
$downloads = (New-Object -ComObject Shell.Application).NameSpace('shell:Downloads').Self.Path
if (-not $downloads -or -not (Test-Path $downloads)) { $downloads = $env:USERPROFILE }
$s = (New-Object -ComObject WScript.Shell).CreateShortcut((Join-Path ([Environment]::GetFolderPath('Programs')) 'fh5dl.lnk'))
$s.TargetPath = 'powershell.exe'
$s.Arguments = "-NoExit -Command `"$welcome`""
$s.WorkingDirectory = $downloads
$s.Save()

if ($env:CI -or $env:SSH_CONNECTION -or $env:FH5DL_NO_OPEN) {
    Write-Host 'Open a new terminal and run: fh5dl'
} else {
    Start-Process powershell.exe -WorkingDirectory $downloads -ArgumentList '-NoExit', '-Command', $welcome
    Write-Host 'Opened a new PowerShell window with fh5dl ready. Next time, open "fh5dl" from the Start menu.'
}

param([string]$Version = '0.2.0-rc.1', [string]$OutputRoot = '')
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($Version -notmatch '^\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$') { throw 'Invalid version' }
if (!$OutputRoot) { $OutputRoot = Join-Path $repo 'dist' }
$stage = Join-Path $OutputRoot "codefind-$Version-windows-amd64"
if (Test-Path -LiteralPath $stage) { throw "Package already exists: $stage. Choose a new version or inspect it before rebuilding." }
New-Item -ItemType Directory -Path $stage | Out-Null
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
Push-Location $repo
try {
    $env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
    & go build -trimpath -buildvcs=false -ldflags '-s -w' -o (Join-Path $stage 'codefind.exe') ./cmd/codefind
    if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
    $actual = & (Join-Path $stage 'codefind.exe') --version
    if ($LASTEXITCODE -ne 0 -or $actual.Trim() -ne $Version) { throw 'Binary version mismatch' }
    Copy-Item -LiteralPath README.md,README_CN.md,LICENSE,CHANGELOG.md,RELEASE_CHECKLIST.md,CONTRIBUTING.md,SECURITY.md -Destination $stage
    New-Item -ItemType Directory -Path (Join-Path $stage 'docs') | Out-Null
    Copy-Item -LiteralPath docs/json-contract.md -Destination (Join-Path $stage 'docs')
    $files = @('go.mod','LICENSE','README.md','README_CN.md','CHANGELOG.md','RELEASE_CHECKLIST.md','CONTRIBUTING.md','SECURITY.md','docs/json-contract.md')
    $files += @(Get-ChildItem cmd,internal -Recurse -File -Filter '*.go' | ForEach-Object { [IO.Path]::GetRelativePath($repo,$_.FullName).Replace('\','/') })
    $sources = @($files | Sort-Object -Unique | ForEach-Object { [ordered]@{path=$_;sha256=(Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash.ToLowerInvariant()} })
    foreach ($source in $sources) {
        $target = Join-Path (Join-Path $stage 'source') $source.path
        New-Item -ItemType Directory -Path (Split-Path $target -Parent) -Force | Out-Null
        Copy-Item -LiteralPath $source.path -Destination $target
    }
    $manifest = [ordered]@{version=$Version;platform='windows-amd64';base_commit=(& git rev-parse HEAD);source_state='working-tree';go_version=(& go version);build_environment=@{GOOS='windows';GOARCH='amd64';CGO_ENABLED='0'};build_flags='-trimpath -buildvcs=false -ldflags "-s -w"';binary_sha256=(Get-FileHash -LiteralPath (Join-Path $stage 'codefind.exe')).Hash.ToLowerInvariant();sources=$sources}
    $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $stage 'manifest.json') -Encoding utf8
    $zip= "$stage.zip"
    if (Test-Path -LiteralPath $zip) { throw "Archive already exists: $zip" }
    Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zip
    $digest=(Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
    "$digest  $([IO.Path]::GetFileName($zip))" | Set-Content -LiteralPath "$zip.sha256" -Encoding ascii
    Write-Output $zip
    Write-Output "SHA256=$digest"
} finally {
    Pop-Location
    $env:GOOS=$oldOS; $env:GOARCH=$oldArch; $env:CGO_ENABLED=$oldCGO
}

# lalmax-nvr lifecycle CLI for Windows.
# Usage: .\scripts\windows\nvr.ps1 <build|run|start|stop|restart|status|logs|test|help>

param(
    [Parameter(Position = 0)]
    [ValidateSet('build', 'run', 'start', 'stop', 'restart', 'status', 'logs', 'test', 'help')]
    [string]$Command = 'help',
    [ValidateSet('windows', 'linux', 'darwin')]
    [string]$BuildOS = '',
    [ValidateSet('amd64', 'arm64', 'arm', '386')]
    [string]$BuildArch = ''
)

$ErrorActionPreference = 'Stop'

. "$PSScriptRoot/env.ps1"

function Show-NvrHelp {
    Write-Host @"
lalmax-nvr scripts (Windows)

Usage:
  .\scripts\windows\nvr.ps1 build      build native bin/lalmax-nvr.exe
  .\scripts\windows\nvr.ps1 build -BuildOS linux -BuildArch amd64
                                   build Linux bin/lalmax-nvr
  .\scripts\windows\nvr.ps1 run        run in foreground
  .\scripts\windows\nvr.ps1 start      start in background
  .\scripts\windows\nvr.ps1 stop       stop background process
  .\scripts\windows\nvr.ps1 restart    restart background process
  .\scripts\windows\nvr.ps1 status     show pid and /api/health
  .\scripts\windows\nvr.ps1 logs       follow logs/lalmax-nvr.log
  .\scripts\windows\nvr.ps1 test       run all Go tests

Command Prompt:
  scripts\windows\start.bat
  scripts\windows\build.bat          native Windows amd64
  scripts\windows\build-linux.bat    cross-compile Linux amd64 (PowerShell only)
  ...

Environment overrides:
  `$env:CONFIG_FILE = '.\config\lalmax-nvr.dev.yaml'; .\scripts\windows\nvr.ps1 start
  `$env:BIN_PATH = '.\bin\lalmax-nvr-dev'; .\scripts\windows\nvr.ps1 build
  `$env:LINES = '200'; .\scripts\windows\nvr.ps1 logs
"@
}

function Invoke-NvrBuild {
    # Build the frontend, all Go modules, then the deployable embedded binary.
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go is required' }
    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
        throw 'npm is required; refusing to embed stale frontend assets'
    }
    if (-not (Test-Path -LiteralPath (Join-Path $RootDir 'web/package-lock.json'))) {
        throw 'web/package-lock.json is missing'
    }
    $Version = if ($env:VERSION) { $env:VERSION } else { 'dev' }
    $HostOs = (go env GOHOSTOS).Trim()
    $HostArch = (go env GOHOSTARCH).Trim()
    # Build target comes from script arguments, NOT inherited shell GOOS/GOARCH.
    $TargetOs = if ($BuildOS) { $BuildOS } else { $HostOs }
    $TargetArch = if ($BuildArch) { $BuildArch } else { $HostArch }
    $BuildJobs = if ($env:GO_BUILD_JOBS) { [int]$env:GO_BUILD_JOBS } else { 1 }
    if ($BuildJobs -lt 1) { throw 'GO_BUILD_JOBS must be >= 1' }

    $OutputBase = $BinPath
    if ($OutputBase.EndsWith('.exe', [System.StringComparison]::OrdinalIgnoreCase)) {
        $OutputBase = $OutputBase.Substring(0, $OutputBase.Length - 4)
    }
    # Keep the original deployment filenames; .exe is only for Windows PE.
    # Windows and Linux results coexist: lalmax-nvr.exe and lalmax-nvr.
    $OutputBin = $OutputBase
    if ($TargetOs -eq 'windows') { $OutputBin += '.exe' }
    $OutputDir = Split-Path -Parent $OutputBin
    if ($OutputDir -and -not (Test-Path -LiteralPath $OutputDir)) {
        New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    }

    if (-not $env:NODE_OPTIONS) { $env:NODE_OPTIONS = '--no-deprecation' }
    elseif ($env:NODE_OPTIONS -notmatch 'no-deprecation') {
        $env:NODE_OPTIONS = "$($env:NODE_OPTIONS) --no-deprecation"
    }

    # Save process environment so a direct nvr.ps1 invocation also leaves
    # GOOS/GOARCH/CGO_ENABLED exactly as they were before the build.
    $PreviousGOOS = [Environment]::GetEnvironmentVariable('GOOS', 'Process')
    $PreviousGOARCH = [Environment]::GetEnvironmentVariable('GOARCH', 'Process')
    $PreviousCGO = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')

    Write-Host "building complete $AppName source tree"
    Write-Host "  host:   $HostOs/$HostArch"
    Write-Host "  target: $TargetOs/$TargetArch"
    Write-Host "  output: $OutputBin"
    Write-Host "  cgo:    0 (temporary)"

    Push-Location $RootDir
    try {
        Write-Host '[1/5] synchronize frontend dependencies (npm ci)'
        npm --prefix web ci --prefer-offline --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }

        Write-Host '[2/5] build current frontend'
        npm --prefix web run build
        if ($LASTEXITCODE -ne 0) { throw 'npm run build failed' }
        $distDir = Join-Path $RootDir 'web/dist'
        if (-not (Test-Path -LiteralPath (Join-Path $distDir 'index.html')) -or
            -not (Test-Path -LiteralPath (Join-Path $distDir 'assets'))) {
            throw 'frontend output dist/index.html or dist/assets is missing'
        }

        Write-Host '[3/5] refresh embedded frontend assets'
        $staticDir = Join-Path $RootDir 'internal/ui/static'
        New-Item -ItemType Directory -Path $staticDir -Force | Out-Null
        $oldAssets = Join-Path $staticDir 'assets'
        if (Test-Path -LiteralPath $oldAssets) {
            Remove-Item -LiteralPath $oldAssets -Recurse -Force
        }
        Copy-Item -Path (Join-Path $distDir '*') -Destination $staticDir -Recurse -Force
        if (-not (Test-Path -LiteralPath (Join-Path $staticDir 'index.html')) -or
            -not (Test-Path -LiteralPath (Join-Path $staticDir 'assets'))) {
            throw 'copying frontend assets into Go embed failed'
        }

        # Temporary build-only target environment, restored in finally.
        $env:GOOS = $TargetOs
        $env:GOARCH = $TargetArch
        $env:CGO_ENABLED = '0'

        Write-Host '[4/5] compile every Go module'
        foreach ($module in @('.', 'third/lal', 'third/lalmax')) {
            Push-Location (Join-Path $RootDir $module)
            try {
                Write-Host "  module: $module"
                go build -p $BuildJobs ./...
                if ($LASTEXITCODE -ne 0) { throw "Go compilation failed in $module" }
            }
            finally {
                Pop-Location
            }
        }

        Write-Host '[5/5] build final NVR binary'
        $tempBin = "$OutputBin.build-tmp"
        if (Test-Path -LiteralPath $tempBin) { Remove-Item -LiteralPath $tempBin -Force }
        try {
            go build -p $BuildJobs -ldflags "-s -w -X main.appVersion=$Version" -o $tempBin ./cmd/lalmax-nvr
            if ($LASTEXITCODE -ne 0) { throw 'final NVR build failed' }
            if (-not (Test-Path -LiteralPath $tempBin)) { throw 'final binary is missing' }
            go version -m $tempBin
            if ($LASTEXITCODE -ne 0) { throw 'Go binary metadata check failed' }
            Move-Item -LiteralPath $tempBin -Destination $OutputBin -Force
        }
        finally {
            if (Test-Path -LiteralPath $tempBin) {
                Remove-Item -LiteralPath $tempBin -Force
            }
        }
        Write-Host "BUILD PASS: $OutputBin"
        # Deliberately do not execute cross-compiled binaries on Windows.
    }
    finally {
        [Environment]::SetEnvironmentVariable('GOOS', $PreviousGOOS, 'Process')
        [Environment]::SetEnvironmentVariable('GOARCH', $PreviousGOARCH, 'Process')
        [Environment]::SetEnvironmentVariable('CGO_ENABLED', $PreviousCGO, 'Process')
        Pop-Location
    }
}

function Invoke-NvrRun {
    Ensure-Config

    if (-not (Test-BinaryExists $BinPath)) {
        Invoke-NvrBuild
    }

    Write-Host "running $AppName"
    Write-Host "  config: $ConfigFile"
    Write-Host "  listen: $(Get-ConfigListen)"

    Push-Location $RootDir
    try {
        $actualBin = Get-ActualBinPath $BinPath
        & $actualBin -config $ConfigFile
        exit $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}

function Invoke-NvrStart {
    Ensure-Config

    $listenAddr = Get-ConfigListen

    if (Test-AppRunning) {
        Write-Host "$AppName already running, pid=$(Get-AppPid), listen=$listenAddr"
        return
    }

    if (-not (Test-BinaryExists $BinPath)) {
        Invoke-NvrBuild
    }

    Write-Host "starting $AppName"
    Write-Host "  config: $ConfigFile"
    Write-Host "  listen: $listenAddr"
    Write-Host "  pid:    $PidFile"
    Write-Host "  log:    $LogFile"

    $process = Start-AppBackground
    Start-Sleep -Milliseconds 500

    if ($process.HasExited) {
        Write-Error "$AppName failed to start, see $LogFile"
        Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
        exit 1
    }

    Write-Host "$AppName started, pid=$($process.Id)"
}

function Invoke-NvrStop {
    if (-not (Test-AppRunning)) {
        Write-Host "$AppName is not running"
        Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
        try { Invoke-CleanupHlsTemp } catch { }
        return
    }

    $processId = Get-AppPid
    Write-Host "stopping $AppName, pid=$processId"

    Stop-Process -Id $processId -ErrorAction SilentlyContinue

    for ($i = 0; $i -lt 50; $i++) {
        if (-not (Get-Process -Id $processId -ErrorAction SilentlyContinue)) {
            Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
            try { Invoke-CleanupHlsTemp } catch { }
            Write-Host "$AppName stopped"
            return
        }
        Start-Sleep -Milliseconds 100
    }

    Write-Host "$AppName did not stop within 5s, sending force kill"
    Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    try { Invoke-CleanupHlsTemp } catch { }
    Write-Host "$AppName stopped"
}

function Invoke-NvrRestart {
    Invoke-NvrStop
    Invoke-NvrStart
}

function Invoke-NvrStatus {
    if (Test-AppRunning) {
        Write-Host "$AppName running, pid=$(Get-AppPid)"
    }
    else {
        Write-Host "$AppName not running"
        exit 1
    }

    $listen = Get-ConfigListen
    if ($listen -like ':*') {
        $url = "http://127.0.0.1$listen/api/health"
    }
    else {
        $url = "http://$listen/api/health"
    }

    Write-Host "health: $url"

    try {
        if (Get-Command curl.exe -ErrorAction SilentlyContinue) {
            curl.exe -fsS $url
            Write-Host ''
        }
        else {
            $response = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 5
            Write-Host $response.Content
        }
    }
    catch {
        Write-Host $_.Exception.Message
    }
}

function Invoke-NvrLogs {
    Ensure-LogFile
    $lines = if ($env:LINES) { [int]$env:LINES } else { 100 }
    Get-Content -LiteralPath $LogFile -Tail $lines -Wait
}

function Invoke-NvrTest {
    Push-Location $RootDir
    try {
        $env:CGO_ENABLED = $CgoEnabled
        if ($env:GOCACHE) { $env:GOCACHE = $env:GOCACHE }
        if ($env:GOMODCACHE) { $env:GOMODCACHE = $env:GOMODCACHE }

        go test ./...
        exit $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}

switch ($Command) {
    'build' { Invoke-NvrBuild }
    'run' { Invoke-NvrRun }
    'start' { Invoke-NvrStart }
    'stop' { Invoke-NvrStop }
    'restart' { Invoke-NvrRestart }
    'status' { Invoke-NvrStatus }
    'logs' { Invoke-NvrLogs }
    'test' { Invoke-NvrTest }
    default { Show-NvrHelp }
}

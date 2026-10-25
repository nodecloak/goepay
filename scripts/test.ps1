param([string]$DatabaseURL = $env:GOEPAY_TEST_DATABASE_URL, [ValidateRange(1,32)][int]$PackageParallel = 2)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $taskRoot
# Keep build scratch files inside the ignored workspace tools directory rather
# than exhausting the Windows system volume during Docker/browser verification.
if (!$env:GOTMPDIR) { $env:GOTMPDIR = Join-Path $taskRoot '.tools\go-tmp' }
if (!$env:GOCACHE) { $env:GOCACHE = Join-Path $taskRoot '.tools\go-cache' }
if (!$env:npm_config_cache) { $env:npm_config_cache = Join-Path $taskRoot '.tools\npm-cache' }
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
$goCommand = Get-Command go -ErrorAction SilentlyContinue
$goBinary = if ($goCommand) { $goCommand.Source } else { 'D:\GoLang\sdk\go1.21.3\bin\go.exe' }
if (!(Test-Path -LiteralPath $goBinary)) { throw 'Go 1.27.3+ toolchain required' }
if (!$DatabaseURL) { throw 'Pass -DatabaseURL for an isolated PostgreSQL test database; database tests may not be silently skipped' }
$testDatabaseUri = [Uri]$DatabaseURL
if ($testDatabaseUri.Scheme -notin @('postgres', 'postgresql') -or !$testDatabaseUri.IsLoopback) { throw 'This local validation script requires a loopback PostgreSQL URL' }
$env:GOEPAY_TEST_DATABASE_URL = $DatabaseURL
& $goBinary test -timeout 40m -p $PackageParallel ./... -count=1
if ($LASTEXITCODE) { throw 'Go tests failed' }
& $goBinary vet ./...
if ($LASTEXITCODE) { throw 'Go vet failed' }
& $goBinary build ./cmd/...
if ($LASTEXITCODE) { throw 'Go build failed' }
node docs/tools/build-openapi.mjs
if ($LASTEXITCODE) { throw 'OpenAPI generation/check failed' }
Push-Location -LiteralPath 'web'
try {
  npm.cmd ci
  if ($LASTEXITCODE) { throw 'npm ci failed' }
  npm.cmd test
  if ($LASTEXITCODE) { throw 'frontend tests failed' }
  npm.cmd run build
  if ($LASTEXITCODE) { throw 'frontend build failed' }
} finally { Pop-Location }

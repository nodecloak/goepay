$ErrorActionPreference = 'Stop'
$testRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $testRoot
$testImage = 'goepay-race-local'
docker build -f Dockerfile.test -t $testImage .
if ($LASTEXITCODE) { throw 'Race test image build failed' }
# Reuse the local Compose service's environment and network, but replace its
# entrypoint/image. The actual PG tests create/drop only their own random schema.
# No credentials are printed, no real channel configuration is loaded.
$localEnv = @{}
foreach ($line in Get-Content -LiteralPath (Join-Path $testRoot '.env')) {
  if ($line -match '^([A-Z_]+)=(.*)$') { $localEnv[$matches[1]] = $matches[2] }
}
if (!$localEnv['POSTGRES_PASSWORD']) { throw 'Local Compose password required' }
$raceDatabaseURL = 'postgres://goepay:' + [Uri]::EscapeDataString($localEnv['POSTGRES_PASSWORD']) + '@postgres:5432/goepay_final?sslmode=disable'
docker run --rm --network goepay_default -e "GOEPAY_TEST_DATABASE_URL=$raceDatabaseURL" $testImage
if ($LASTEXITCODE) { throw 'Linux race or vet failed' }

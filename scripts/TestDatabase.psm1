function Invoke-GoepayTestDatabase {
 [CmdletBinding()]
 param(
  [Parameter(Mandatory)][string]$MetadataPath,
  [Parameter(Mandatory)][ValidatePattern('^[a-z][a-z0-9_]{0,17}$')][string]$Label,
  [Parameter(Mandatory)][scriptblock]$Work
 )
 $ErrorActionPreference='Stop'
 $workspace=Split-Path -Parent $PSScriptRoot
 $toolsRoot=[IO.Path]::GetFullPath((Join-Path $workspace '.tools'))+[IO.Path]::DirectorySeparatorChar
 $metadataFile=[IO.Path]::GetFullPath($MetadataPath)
 if(!$metadataFile.StartsWith($toolsRoot,[StringComparison]::OrdinalIgnoreCase)){throw 'Test pool metadata must be inside workspace .tools'}
 $pool=Get-Content -LiteralPath $metadataFile -Raw|ConvertFrom-Json
 if($pool.synthetic_only -ne $true -or $pool.loopback_only -ne $true){throw 'Explicit synthetic loopback test pool required'}
 $container=if($pool.container){[string]$pool.container}else{[string]$pool.pg}
 if($container -notmatch '^goepay-(globalaccess179-|group144-affected-pg-|shared-test-)[a-z0-9-]+$'){throw 'An existing designated test pool is required; preview containers are prohibited'}
 $pgUser=if($pool.user){[string]$pool.user}else{'goepay'}
 if($pgUser -notmatch '^[a-z][a-z0-9_]{0,62}$' -or !$pool.password -or [int]$pool.port -lt 1 -or [int]$pool.port -gt 65535){throw 'Invalid test pool connection metadata'}
 $raw=& docker inspect $container
 if($LASTEXITCODE){throw 'Existing test pool is unavailable; no replacement container will be created'}
 $details=@($raw|ConvertFrom-Json)
 if($details.Count -ne 1 -or $details[0].Name -cne ('/'+$container) -or !$details[0].State.Running){throw 'Existing test pool identity or running state mismatch'}
 $bindings=@($details[0].HostConfig.PortBindings.'5432/tcp')
 if($bindings.Count -ne 1 -or $bindings[0].HostIp -cne '127.0.0.1' -or [int]$bindings[0].HostPort -ne [int]$pool.port){throw 'Test pool loopback binding mismatch'}
 $database='goepay_test_'+$Label+'_'+[guid]::NewGuid().ToString('N')
 $previousUrl=$env:GOEPAY_TEST_DATABASE_URL
 $previousExpected=$env:GOEPAY_EXPECTED_TEST_DATABASE
 $created=$false;$workFailure=$null
 try{
  & docker exec $container psql -U $pgUser -d postgres -v ON_ERROR_STOP=1 -c ('CREATE DATABASE "'+$database+'" OWNER '+$pgUser) >$null
  if($LASTEXITCODE){throw 'Isolated test database creation failed'}
  $created=$true
  $env:GOEPAY_TEST_DATABASE_URL='postgres://'+[Uri]::EscapeDataString($pgUser)+':'+[Uri]::EscapeDataString([string]$pool.password)+'@127.0.0.1:'+([int]$pool.port)+'/'+$database+'?sslmode=disable'
  $env:GOEPAY_EXPECTED_TEST_DATABASE=$database
  & $Work $database
 }catch{
  $workFailure=$_
  throw
 }finally{
  try{
   if($created){
    $clients=& docker exec $container psql -U $pgUser -d postgres -At -v ON_ERROR_STOP=1 -c ("SELECT count(*) FROM pg_stat_activity WHERE datname='"+$database+"'")
    if($LASTEXITCODE -or ($clients -join '').Trim() -notmatch '^0$'){throw 'Temporary test database retained: active connections or failed connection check'}
    & docker exec $container psql -U $pgUser -d postgres -v ON_ERROR_STOP=1 -c ('DROP DATABASE "'+$database+'"') >$null
    if($LASTEXITCODE){throw 'Temporary test database cleanup failed; database retained'}
   }
  }catch{
   if($workFailure){Write-Warning $_.Exception.Message}else{throw}
  }finally{
   $env:GOEPAY_TEST_DATABASE_URL=$previousUrl
   $env:GOEPAY_EXPECTED_TEST_DATABASE=$previousExpected
  }
 }
}

Export-ModuleMember -Function Invoke-GoepayTestDatabase

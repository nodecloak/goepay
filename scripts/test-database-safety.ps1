$ErrorActionPreference='Stop'
$testRoot=Split-Path -Parent $PSScriptRoot
$testDir=Join-Path $testRoot ('.tools/test-database-safety-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testDir|Out-Null
$global:testDatabaseCalls=[Collections.Generic.List[object]]::new()
$global:testDatabaseActive=$false
function global:docker {
 $a=@($args);$global:testDatabaseCalls.Add($a);$global:LASTEXITCODE=0
 if($a[0] -eq 'inspect'){
  @{Name='/goepay-globalaccess179-fixture';State=@{Running=$true};HostConfig=@{PortBindings=@{'5432/tcp'=@(@{HostIp='127.0.0.1';HostPort='56069'})}}}|ConvertTo-Json -Depth 6
 }elseif(($a -join ' ') -match 'pg_stat_activity'){
  if($global:testDatabaseActive){'1'}else{'0'}
 }else{'OK'}
}
function Assert-True($value,[string]$message){if(!$value){throw $message}}
function Assert-Throws([scriptblock]$body,[string]$pattern){
 $caught=$false
 try{& $body}catch{$caught=$true;if($_.Exception.Message -notmatch $pattern){throw}}
 if(!$caught){throw ('Expected failure: '+$pattern)}
}
try{
 Import-Module (Join-Path $PSScriptRoot 'TestDatabase.psm1') -Force
 $metaPath=Join-Path $testDir 'pool.json'
 $metadata=@{container='goepay-globalaccess179-fixture';port=56069;user='goepay';password='synthetic-fixture-only';synthetic_only=$true;loopback_only=$true}
 $metadata|ConvertTo-Json|Set-Content -LiteralPath $metaPath
 $oldUrl=$env:GOEPAY_TEST_DATABASE_URL;$oldExpected=$env:GOEPAY_EXPECTED_TEST_DATABASE
 $env:GOEPAY_TEST_DATABASE_URL='sentinel';$env:GOEPAY_EXPECTED_TEST_DATABASE='sentinel'
 $first=Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label safety -Work {param($database) $database}
 Assert-True ($first -match '^goepay_test_safety_[a-f0-9]{32}$') 'Run database identity was lost'
 Assert-True ($env:GOEPAY_TEST_DATABASE_URL -ceq 'sentinel') 'Connection URL leaked across runs'
 Assert-True ($env:GOEPAY_EXPECTED_TEST_DATABASE -ceq 'sentinel') 'Expected database leaked'
 $second=Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label safety -Work {param($database) $database}
 Assert-True ($first -cne $second) 'Repeated runs reused a database'
 Assert-Throws {Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label safety -Work {throw 'synthetic test failure'}} 'synthetic test failure'
 $drops=@($global:testDatabaseCalls|Where-Object {($_ -join ' ') -match 'DROP DATABASE'})
 Assert-True ($drops.Count -eq 3) 'A failed test did not clean up its own database'
 $global:testDatabaseActive=$true
 Assert-Throws {Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label safety -Work {}} 'active connections'
 Assert-True (@($global:testDatabaseCalls|Where-Object {($_ -join ' ') -match 'DROP DATABASE'}).Count -eq 3) 'Active database was dropped'
 $global:testDatabaseActive=$false
 $metadata.synthetic_only=$false;$metadata|ConvertTo-Json|Set-Content $metaPath
 Assert-Throws {Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label safety -Work {}} 'synthetic'
 $metadata.synthetic_only=$true;$metadata.container='goepay-ux103-local-preview-postgres-1';$metadata|ConvertTo-Json|Set-Content $metaPath
 Assert-Throws {Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label safety -Work {}} 'pool'
 Assert-Throws {Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label 'unsafe;sql' -Work {}} 'Label'
 Assert-Throws {Invoke-GoepayTestDatabase -MetadataPath $metaPath -Label ('a'*19) -Work {}} 'Label'
 Assert-True (@($global:testDatabaseCalls|Where-Object {$_[0] -in @('run','create','start','stop','rm','network','volume')}).Count -eq 0) 'Container lifecycle mutation occurred'
 Write-Output 'PASS: unique databases, failure cleanup, active-session protection, unsafe target rejection, environment restoration; no container creation'
}finally{
 $env:GOEPAY_TEST_DATABASE_URL=$oldUrl;$env:GOEPAY_EXPECTED_TEST_DATABASE=$oldExpected
 Remove-Item Function:\docker -ErrorAction SilentlyContinue
}

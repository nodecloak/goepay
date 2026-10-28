param(
 [Parameter(Mandatory)][string]$Workspace,
 [Parameter(Mandatory)][string]$Prepared,
 [Parameter(Mandatory)][string]$ReferenceRoot,
 [switch]$AckRunnerIdle
)
$ErrorActionPreference='Stop'
if(!$AckRunnerIdle){throw 'Wait for full Linux verification to finish, then explicitly pass -AckRunnerIdle. No runner action was performed.'}
$workspacePath=(Resolve-Path -LiteralPath $Workspace).Path
$preparedPath=(Resolve-Path -LiteralPath $Prepared).Path
$ownedParent=[IO.Path]::GetFullPath((Join-Path $workspacePath '.tools'))
if((Split-Path -Parent $preparedPath) -cne $ownedParent -or (Split-Path -Leaf $preparedPath) -notmatch '^phpinterop-prepared-[a-f0-9]{32}$'){throw 'Exact owned preparation directory required'}
$prepareReceipt=Get-Content -Raw -LiteralPath (Join-Path $preparedPath 'PREPARED.json')|ConvertFrom-Json
$manifestFile=Join-Path $preparedPath 'runtime.manifest.json'
$manifest=Get-Content -Raw -LiteralPath $manifestFile|ConvertFrom-Json
$phpImage='sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50'
$runner='goepay-full195-linux-runner-52150d9d'
$runnerImage='sha256:0af3d9e13c9038504022e9631de8251bfb5c4ef468e43138bbf2b432befc1f46'
function Hash-File([string]$File){(Get-FileHash -LiteralPath $File -Algorithm SHA256).Hash.ToLowerInvariant()}
function Check-NoReparse([string]$File){
 $current=[IO.Path]::GetFullPath($File)
 while($current){
  $item=Get-Item -Force -LiteralPath $current
  if(($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw ('Reparse path is not an owned ordinary path: '+$current)}
  $next=Split-Path -Parent $current
  if(!$next -or $next -eq $current){break}
  $current=$next
 }
}
function Check-References {
 $root=(Resolve-Path -LiteralPath $ReferenceRoot).Path
 Check-NoReparse $root
 foreach($entry in $manifest.references.PSObject.Properties){
  $candidate=[IO.Path]::GetFullPath((Join-Path $root $entry.Name))
  Check-NoReparse $candidate
  if(!$candidate.StartsWith($root+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase) -or (Hash-File $candidate) -cne $entry.Value){throw ('Original PHP reference mismatch: '+$entry.Name)}
 }
}
Check-NoReparse $preparedPath
if($prepareReceipt.schema -ne 1 -or $prepareReceipt.status -cne 'prepared-not-executed' -or $prepareReceipt.image -cne $phpImage -or $prepareReceipt.runtime_id -cne $manifest.runtime_id -or $manifest.runtime_id -cnotmatch '^[a-f0-9]{32}$' -or $prepareReceipt.containers_created -ne 0 -or $prepareReceipt.runner_entered -or $prepareReceipt.php_executed -or $manifest.schema -ne 1 -or $manifest.architecture -cne 'linux-amd64' -or @($manifest.references.PSObject.Properties).Count -ne 15 -or (Hash-File $manifestFile) -cne $prepareReceipt.runtime_manifest_sha256 -or (Hash-File (Join-Path $preparedPath 'runtime.tar')) -cne $prepareReceipt.runtime_tar_sha256){throw 'Prepared archive/manifest integrity mismatch'}
if((Hash-File (Join-Path $PSScriptRoot 'install.mjs')) -cne $manifest.files.'install.mjs'.sha256){throw 'Installer source changed since preparation'}
if(Test-Path -LiteralPath (Join-Path $preparedPath 'INSTALL_ATTEMPT.json')){throw 'Preserve the prior attempt; use a fresh preparation ID'}
Check-References
$identity=(& docker inspect --format '{{.Id}} {{.Image}} {{.State.Running}}' $runner)
if($LASTEXITCODE -ne 0 -or "$identity" -notmatch ('^([a-f0-9]{64}) '+[regex]::Escape($runnerImage)+' true$')){throw 'Exact existing running test runner required; no container will be created'}
$runnerID=$Matches[1]
$before=@(& docker ps -aq --no-trunc|Sort-Object)
if($LASTEXITCODE -ne 0){throw 'Container inventory failed'}
$prefix='/tmp/goepay-phpinterop-'+$manifest.runtime_id
@{runner=$runner;runner_id=$runnerID;prefix=$prefix;containers_before=$before;at=[DateTime]::UtcNow.ToString('o')}|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $preparedPath 'INSTALL_ATTEMPT.json')
try {
 $create='const fs=require("fs");const p=process.argv[1];if(!/^\/tmp\/goepay-phpinterop-[a-f0-9]{32}$/.test(p)||fs.existsSync(p))throw Error("fresh owned prefix required");fs.mkdirSync(p,{mode:448})'
 & docker exec $runner node -e $create $prefix
 if($LASTEXITCODE -ne 0){throw 'Owned runner directory preparation failed'}
 & docker cp (Join-Path $preparedPath 'runtime.tar') ($runner+':'+$prefix+'/bundle.tar')
 if($LASTEXITCODE -ne 0){throw 'Copy into existing owned runner failed'}
 & docker cp (Join-Path $PSScriptRoot 'install.mjs') ($runner+':'+$prefix+'/bootstrap-install.mjs')
 if($LASTEXITCODE -ne 0){throw 'Installer copy failed'}
 $bootstrap='const fs=require("fs"),c=require("crypto"),u=require("url");const [file,want,...args]=process.argv.slice(1);if(!/^\/tmp\/goepay-phpinterop-[a-f0-9]{32}\/bootstrap-install\.mjs$/.test(file)||!fs.lstatSync(file).isFile()||fs.realpathSync(file)!==file||c.createHash("sha256").update(fs.readFileSync(file)).digest("hex")!==want)throw Error("copied installer integrity");process.argv=[process.execPath,file,...args];import(u.pathToFileURL(file).href).catch(e=>{console.error(String(e));process.exitCode=125})'
 $output=@(& docker exec $runner node -e $bootstrap ($prefix+'/bootstrap-install.mjs') $manifest.files.'install.mjs'.sha256 --prefix $prefix --tar-sha256 $prepareReceipt.runtime_tar_sha256 --manifest-sha256 $prepareReceipt.runtime_manifest_sha256 --ack-idle yes)
 $installExit=$LASTEXITCODE
 $output|Set-Content -LiteralPath (Join-Path $preparedPath 'INSTALL_OUTPUT.json')
 if($installExit -ne 0){throw 'Actual PHP sandbox preflight unavailable; skip interop, preserve receipt; no fallback'}
 $receipt=($output -join "`n")|ConvertFrom-Json
 if($receipt.status -cne 'ready' -or !$receipt.seccomp_network_denied -or !$receipt.seccomp_exec_denied -or $receipt.namespace_isolated -or $receipt.uid -ne 65534 -or $receipt.containers_created -ne 0){throw 'Sandbox proof did not pass'}
 $receipt|Add-Member -NotePropertyName runner -NotePropertyValue $runner
 $receipt|Add-Member -NotePropertyName runner_id -NotePropertyValue $runnerID
 $receipt|Add-Member -NotePropertyName runner_image -NotePropertyValue $runnerImage
 $receipt|Add-Member -NotePropertyName references -NotePropertyValue $manifest.references
 Check-References
 $after=@(& docker ps -aq --no-trunc|Sort-Object)
 if($LASTEXITCODE -ne 0 -or @(Compare-Object $before $after).Count -ne 0){throw 'Container inventory changed during preparation'}
 $ready=Join-Path $preparedPath 'READY.host.json'
 [IO.File]::WriteAllText($ready,($receipt|ConvertTo-Json -Depth 6)+"`n",[Text.UTF8Encoding]::new($false))
 @{status='ready';receipt=$ready;receipt_sha256=(Hash-File $ready);containers_created=0;namespace_isolated=$false;network_restriction='child seccomp syscall denial';runner=$runner}|ConvertTo-Json
}catch{
 @{status='unavailable';error=$_.Exception.Message;containers_created=0;namespace_isolated=$false;runner=$runner}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $preparedPath 'INSTALL_UNAVAILABLE.json')
 throw
}

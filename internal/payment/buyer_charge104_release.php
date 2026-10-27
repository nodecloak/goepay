<?php
// Execute the exact pinned readable Release branch, not a rewritten formula.
$source = str_replace("\r\n", "\n", file_get_contents('/reference/Pay.php'));
$start = strpos($source, "if(\$userrow['mode']==1){ //订单加费模式");
$end = strpos($source, "\n        }else{\n            \$submitData = \\lib\\Channel::info", $start);
if ($start === false || $end === false) throw new RuntimeException('original fee branch not found');
$branch = substr($source, $start, $end - $start);
$inputs = json_decode(file_get_contents('php://stdin'), true, 512, JSON_THROW_ON_ERROR);
$results = [];
foreach ($inputs as $input) {
    $money = $input['original'] / 100;
    $submitData = ['rate' => (10000 - $input['fee_bps']) / 100];
    $userrow = ['mode' => $input['mode']];
    $conf = ['payfee_lessthan' => $input['threshold'] / 100, 'payfee_mincost' => $input['cost'] / 100];
    $realmoney = null;
    $getmoney = null;
    eval($branch);
    $results[] = ['charge' => (int)round($realmoney * 100), 'net' => (int)round($getmoney * 100)];
}
echo json_encode(['synthetic_only'=>true, 'network'=>'none', 'original_branch_sha256'=>hash('sha256', $branch), 'results'=>$results], JSON_THROW_ON_ERROR), "\n";

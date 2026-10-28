<?php
// Trusted test bootstrap: no original PHP may run if the preload guard failed.
if (getenv('GOEPAY_PHP_GUARD_STATE') !== 'guarded') {
    fwrite(STDERR, "PHP_GUARD_UNAVAILABLE: constructor did not seal process\n");
    exit(125);
}
$argv = array_slice($argv, 1);
$argc = count($argv);
$_SERVER['argv'] = $argv;
$_SERVER['argc'] = $argc;
if ($argc === 2 && $argv[0] === '-r') {
    eval($argv[1]);
} elseif ($argc >= 1 && $argv[0][0] === '/') {
    $_SERVER['SCRIPT_FILENAME'] = $argv[0];
    require $argv[0];
} else {
    fwrite(STDERR, "PHP_GUARD_UNAVAILABLE: exact test script required\n");
    exit(125);
}

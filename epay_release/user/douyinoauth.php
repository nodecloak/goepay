<?php
$nosession=true;
include("../includes/common.php");
if(isset($_GET['code']) && isset($_GET['state'])){
    $code = $_GET['code'];
    $state = base64_decode($_GET['state']);
    if(str_starts_with($state, '/')){
        if (str_contains($state, '?')) {
            $redirect = $state . '&code=' . $code;
        } else {
            $redirect = $state . '?code=' . $code;
        }
    }else{
        $redirect = '/';
    }
    header('Location: '.$redirect);
    exit;
}
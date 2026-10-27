<?php
// 京东扫码支付页面

if(!defined('IN_PLUGIN'))exit();
?>
<!DOCTYPE html>
<html>
<head>
<meta name="viewport" content="initial-scale=1, maximum-scale=1, user-scalable=no, width=device-width">
<meta http-equiv="Content-Type" content="text/html; charset=UTF-8">
<meta http-equiv="Content-Language" content="zh-cn">
<meta name="renderer" content="webkit">
<title>京东扫码支付</title>
<link href="/assets/css/jd_pay.css" rel="stylesheet" media="screen">
</head>
<body>
<div class="body">
<h1 class="mod-title">
<span class="ico-wechat"></span><span class="text">京东扫码支付</span>
</h1>
<div class="mod-ct">
<div class="order">
</div>
<div class="amount">¥<?php echo $order['realmoney']?></div>
<div class="qr-container">
<div class="qr-image" id="qrcode">
</div>
<div class="qr-expired-overlay" id="qrExpiredOverlay">
    <div class="qr-expired-icon">×</div>
    <div class="qr-expired-text">二维码已失效</div>
    <div class="qr-expired-text">请返回重新发起支付</div>
</div>
</div>
<div class="countdown-container">
    <div class="countdown-title">支付剩余时间</div>
    <div class="countdown-timer">
        <div class="countdown-box">
            <span id="hours">00</span>
            <span class="countdown-label">时</span>
        </div>
        <div class="countdown-box">
            <span id="minutes">00</span>
            <span class="countdown-label">分</span>
        </div>
        <div class="countdown-box">
            <span id="seconds">00</span>
            <span class="countdown-label">秒</span>
        </div>
    </div>
</div>
 
<div class="detail" id="orderDetail">
<dl class="detail-ct" style="display: none;">
<dt>商家</dt>
<dd id="storeName"><?php echo $sitename?></dd>
<dt>购买物品</dt>
<dd id="productName"><?php echo $order['name']?></dd>
<dt>商户订单号</dt>
<dd id="billId"><?php echo $order['trade_no']?></dd>
<dt>创建时间</dt>
<dd id="createTime"><?php echo $order['addtime']?></dd>
</dl>
<a href="javascript:void(0)" class="arrow"><i class="ico-arrow"></i></a>
</div>
<div class="tip">
<span class="dec dec-left"></span>
<span class="dec dec-right"></span>
<div class="ico-scan"></div>
<div class="tip-text">
<p>请使用京东APP扫一扫</p>
<p>扫描二维码完成支付</p>
</div>
</div>
<div class="tip-text">
</div>
</div>
</div>
<script src="<?php echo $cdnpublic?>jquery/1.12.4/jquery.min.js"></script>
<script src="<?php echo $cdnpublic?>layer/3.1.1/layer.js"></script>
<script src="<?php echo $cdnpublic?>jquery.qrcode/1.0/jquery.qrcode.min.js"></script>
<script>
	var code_url = '<?php echo $code_url?>';
    var code_type = code_url.indexOf('data:image/')>-1?1:0;
    if(code_type == 0){
        $('#qrcode').qrcode({
            text: code_url,
            width: 230,
            height: 230,
            foreground: "#000000",
            background: "#ffffff",
            typeNumber: -1
        });
    }else{
        $('#qrcode').html('<img src="'+code_url+'"/>');
    }
    // 订单详情
    $('#orderDetail .arrow').click(function (event) {
        if ($('#orderDetail').hasClass('detail-open')) {
            $('#orderDetail .detail-ct').slideUp(500, function () {
                $('#orderDetail').removeClass('detail-open');
            });
        } else {
            $('#orderDetail .detail-ct').slideDown(500, function () {
                $('#orderDetail').addClass('detail-open');
            });
        }
    });
    function loadmsg() {
        $.ajax({
            type: "GET",
            dataType: "json",
            url: "/getshop.php",
            data: {type: "jdpay", trade_no: "<?php echo $order['trade_no']?>"},
            success: function (data) {
                if (data.code == 1) {
					layer.msg('支付成功，正在跳转中...', {icon: 16,shade: 0.1,time: 15000});
					setTimeout(window.location.href=data.backurl, 1000);
                }else{
                    setTimeout("loadmsg()", 2000);
                }
            },
            error: function () {
                setTimeout("loadmsg()", 2000);
            }
        });
    }
	window.onload = function(){
		setTimeout("loadmsg()", 2000);
        startCountdown('<?php echo $expires_in?>');
	}

    // 倒计时功能
    function startCountdown(duration) {
        if (duration === '') return;
        $(".countdown-container").show();
        var timer = parseInt(duration);
        if(duration <= 0){
            $("#qrExpiredOverlay").show();
            return;
        }

        var countdown = function() {
            var hours = Math.floor(timer / 3600);
            var minutes = Math.floor((timer % 3600) / 60);
            var seconds = timer % 60;
            hours = hours < 10 ? "0" + hours : hours;
            minutes = minutes < 10 ? "0" + minutes : minutes;
            seconds = seconds < 10 ? "0" + seconds : seconds;
            $("#hours").text(hours);
            $("#minutes").text(minutes);
            $("#seconds").text(seconds);
            if (--timer < 0) {
                clearInterval(window.countdownInterval);
                $("#qrExpiredOverlay").show();
            }
        }
        countdown();
        window.countdownInterval = setInterval(countdown, 1000);
    }
</script>
</body>
</html>
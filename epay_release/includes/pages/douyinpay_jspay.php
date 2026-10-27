<?php
// 抖音支付JSAPI页面

if(!defined('IN_PLUGIN'))exit();
?>
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="initial-scale=1, maximum-scale=1, user-scalable=no, width=device-width">
    <title>抖音支付</title>
    <link href="/assets/pay/css/weui.css" rel="stylesheet" />
</head>
<body>
    <div class="container js_container">
        <div class="page msg">
            <div class="weui_msg">
                <div class="weui_icon_area"><i class="weui_icon_info weui_icon_msg"></i></div>
                <div class="weui_text_area">
                    <h2 class="weui_msg_title">正在跳转支付...</h2>
                </div>
            </div>
        </div>
    </div>
<script src="<?php echo $cdnpublic?>jquery/1.12.4/jquery.min.js"></script>
<script src="<?php echo $cdnpublic?>layer/3.1.1/layer.js"></script>
<script src="https://unpkg.byted-static.com/bridge/douyin_open/1.0.20/lib/douyin_open.umd.js"></script>
<script>
document.body.addEventListener('touchmove', function (event) {
	event.preventDefault();
},{ passive: false });

const sdk = window.DouyinOpenJSBridge;

sdk.config({
  params: <?php echo $sdkConfig; ?>
});
sdk.ready(() => {
  onBridgeReady()
});
sdk.error((res) => {
  if (res) {
    layer.alert(res.status_msg, {icon: 2});
  }
});
function onBridgeReady() {
  sdk.ttcjpay.dypay({
    sdk_info: '<?php echo $jsApiParameters; ?>',
    success: res => {
      if (res && res.code === '0') {
        loadmsg();
      }
    },
    fail: res => {
      if (res) {
        if (res.code === -2) {
          layer.msg('请升级抖音APP');
        } else if (res.code === -3) {
          layer.msg('支付失败，参数错误');
        } else if (res.code === -1 || res.code === -100) {
          layer.msg('支付失败，JSB安全域名不匹配');
        } else {
          layer.msg('支付失败（' + res.code + '）');
        }
      }
    }
  });
}
function loadmsg() {
	$.ajax({
		type: "GET",
		dataType: "json",
		url: "/getshop.php",
		data: {type: "douyinpay", trade_no: "<?php echo TRADE_NO?>"},
		success: function (data) {
			if (data.code == 1) {
				layer.msg('支付成功，正在跳转中...', {icon: 16,shade: 0.01,time: 15000});
				window.location.href=<?php echo $redirect_url?>;
			}else{
				setTimeout("loadmsg()", 2000);
			}
		},
		error: function () {
			setTimeout("loadmsg()", 2000);
		}
	});
}
</script>
</body>
</html>
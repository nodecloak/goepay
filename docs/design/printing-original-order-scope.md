# 原订单自动打印范围复核

2026-10-24本地明文证据：`epay_release/includes/functions.php:604–709`中tid2余额充值独立处理；707开始明确只在tid0普通订单或tid3聚合收款调用`MsgNotice::send('order',...)`。`epay_release/includes/lib/MsgNotice.php:30`和较新`epay_pro/app/lib/MsgNotice.php:61`均要求scene=order，且print_order=2打印该类订单、1只打印tid3。

因此不能把充值自动打印当作已经证实的原系统缺口，当前Go的typed recharge排除保持。`epay_pro/app/service/OrderProcessService.php`使用Swoole Loader加密，无法从现有明文核实其全部新调用链；不以release证据冒称较新加密Pro完全相同，也不据猜测扩大外发打印。

新增`internal/printing/recharge_scope_test.go`验证：实际已认证的合成回调、明确充值来源、fresh/captured/平台启用/全部订单模式已满足，其首次及重复自动准备与重复回调仍无打印意图；充值余额只入账一次，普通已付订单正例仍自动创建打印任务。仅本地合成测试，不发送真实打印，不修改资金规则、已冻结迁移或既有打印来源。

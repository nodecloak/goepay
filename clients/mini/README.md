# 微信、支付宝小程序支付页

这是现有Go小程序支付接口的客户端配套工程。保留Pro的 `pages/pay/pay?money=...&url=...` 启动字段，以JSON POST交换一次性SDK code，然后调起原订单支付。客户端不含支付密钥，不把SDK返回的成功状态记为订单已支付。

从项目根目录执行：

```powershell
node --test clients/mini/tests/*.test.cjs
node clients/mini/build.mjs --output E:\GOEPAY\test-results\mini-client81-package
```

输出目录必须为新的绝对路径；打包器拒绝覆盖已有目录。输出包含 `wxpay` 和 `alipay` 两个独立工程及 `MANIFEST.json` 文件哈希。导入各自开发者工具即可查看源码；默认 `enabled:false`，打开页面或导入工程不会发起身份兑换或支付。

接入前在各工程 `config.js` 填入公开配置：`origin` 为平台固定HTTPS源（无末尾斜杠），`appId` 为原支付应用，`channelIDs` 为允许的规范字符串渠道ID。微信工程的 `project.config.json` AppID也须匹配。示例值 `UNCONFIGURED`、`touristappid`、`pay.example.invalid` 不代表有效支付应用。服务端必须已有匹配应用和渠道、独立身份兑换配置及原有开关；不得把AppSecret、商户API密钥或私钥放进客户端。

页面在用户点击“确认支付”后，微信调用 `wx.login`，支付宝调用 `my.getAuthCode` 的 `auth_base`；随后仅向固定completion URL发送 `{code}`，展示金额不提交。首次兑换后不重新登录或重发原code；重复点击、并发打开同state、超时、卸载后晚回调均不能产生新的客户端付款调用。当前进程最多在内存保留200个一次性state记录；达到上限后关闭小程序重新从原订单进入，服务端仍负责持久的一次性验证。

支付取消、网络异常、SDK成功或支付宝9000后均提示返回原订单查单；不自动跳外链，不提供重复付款按钮。受保护收银台URL只经过同源原订单校验后交给控制器调用方，原生示例页面不将其写入页面data或存储。

本机后端互操作测试：

```powershell
$env:GOEPAY_MINI_CLIENT_INTEROP='1'
# GOEPAY_TEST_DATABASE_URL必须指向本地隔离测试数据库。
go test ./tests/contract -run '^TestMiniClient81NodeHTTPInterop$' -count=1
```

该测试实际运行同一JS控制器，经仅回环HTTP桥访问Go Handler与随机PostgreSQL schema；上游兑换/下单使用固定本地TLS替身。测试桥只接受 `127.0.0.1`，不会打包到原生工程。微信/支付宝SDK由替身执行，没有真实设备编译、发布、业务域名登记或实际资金验证。SDK底层对重定向的处理不由该控制器接管，平台固定HTTPS completion不得配置外部重定向。

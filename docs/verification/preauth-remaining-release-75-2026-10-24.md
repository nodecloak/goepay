# 预授权剩余解除75专项证据

2026-10-24，E:\GOEPAY。仅本机合成PostgreSQL、临时TLS与浏览器mock；未访问生产、未使用导入真实凭证、未发真实资金。本文件记录开发专项，不是固定候选全量或部署验收。

## 实现与原语义

依据本地明文`epay_pro/app/lib/Payment.php::alipayUnfreeze`解除原授权明确`rest_amount`。Go保留原父amount与永久UA，独立来源修订绑定原FREEZE观察、账户/身份/金额、原会话、期限、事务和HMAC。观察本身不授予资金操作；管理员即使全局可选保护关闭仍需新动作独立资金确认，商户需同一本人会话和显式确认，拒绝代登录。

未发送时新观察/新确认CAS更新来源；旧修订不能认领新额度。原全额发送函数拒绝非0来源。source与claim/审计缺失、损坏、历史降级或孤立审计阻断。首次发送前提交唯一claim；结果未知永不重发，仅原UA查询。全额支付依原PaidReceipt，部分原支付不虚构全额入账；在途捕获不能用NOT_FOUND解围，只有已验签TRADE_CLOSED才可首次解冻。查询精确UNFREEZE/SUCCESS、原身份/金额及明确余款0后才确认，不产生商户退款或余额。

## 实际运行

证据目录均位于项目`.tools`；每个Go专项有原日志与`.result.json`实际起止时间和退出码。

| 范围 | 证据 | 实际结果 |
|---|---|---|
| 纯条款 | settlement-auto73-next-preauth75-policy-red/green.log | 真实编译RED后1.498秒通过 |
| 旧父身份强校验及旧全额/手动扣款 | settlement-auto73-next-preauth75-legacy-source-green-and-original.log | 126.977秒通过；此前真实3例损坏被接受的RED保留 |
| 旧全额解除、68观察和新来源/异常关联 | settlement-auto73-next-preauth75-integrity-original-green1.log | 258.057秒通过 |
| 新来源与完整发送分支 | settlement-auto73-next-preauth75-final-core-first.log | 92.840秒通过 |
| 最后审计计数、孤立审计、claim等待过期及所有75专项 | settlement-auto73-next-preauth75-final-core-audit.log | 129.726秒通过，2026-10-24T06:08:40Z至06:10:54Z；等待阶段由非事务序列证明，过期claim及其审计/父发送更新回滚 |
| 真74表形状正授权升级及71/73旧升级关联 | settlement-auto73-next-preauth75-upgrade-first.log | 18.792秒通过；旧列/UA/原总额一致，无来源回填，旧首次发送及检查器保持 |
| 本地真实签名TLS、admin mandatory资金确认/merchant本人、严格JSON/CSRF、错额查询/撤销后恢复及旧HTTP关联 | settlement-auto73-next-preauth75-http-original-final.log | server1.458秒、contract24.434秒通过；后加严格Cookie后cookie-final.log重验server1.344秒、contract12.601秒通过 |
| 前端全量开发区 | 2026-10-24T06:10运行输出、web/playwright-report/runs/2026-10-24T06-10-42-371Z/index.html | 65文件292单元通过；type/build0；144mock通过、9实际部署opt-in跳过、0失败，38.8秒 |
| 手机/桌面专项 | web/test-results/runs/2026-10-24T06-06-55-700Z | 9项通过6.3秒；375/1440截图已查看，长标识溢出修复后2项复测3.0秒，新增横向溢出断言 |
| 全仓库静态检查 | static75-895fce12e8f64e6fa99a3e2d99201915/RESULT.json | vet/build均0，明确为开发区非固定全量证据 |

API生成器已生成160路径/188操作。新增`release-remaining-authorization`严格接收三个字符串字段；订单详情按服务端能力展示。检查器新增`invalid_authorization_releases`，CLI结构计数由20增至21。前端564.41kB构建块告警保留；此处不宣称性能优于PHP。

## 仍须完成的集成验收

固定源文件清单及哈希、Windows完整33包、Linux完整race/vet/build、固定前端/PHP/源扫描；无重置本地升级、旧列/迁移校验和、两个模拟通道、9项实际浏览器及完整备份恢复。当前仍运行85/schema73，75未部署。86的Windows审计夹具失败保留，不能以专项替代全量；87仅为单项夹具修订准备，未执行完整套件。

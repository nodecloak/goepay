# 渠道分账专用双后台专项验证

本记录对应 `E:\GOEPAY` 的开发工作区专项，不是全部迁移阶段或生产兼容验收。50至55核心迁移由其它工作并行推进，完整固定快照、Linux race、Docker升级与恢复验证由后续整体验收补充；此处不把临时TLS协议测试写成真实支付机构资金联调。

四个原路径 `/admin/profitsharing/{receiver|order}`、`/merchant/profitsharing/{receiver|order}` 已接专用页面。接收方加密草稿、绑定/解绑、不可变规则版本、原支付声明、PS、原成功接收方PR、可验证微信PF使用专用API；站内分账审批保留独立路径。原82菜单当前为79 partial、1 pending、2 deferred；partial表示已接对应业务的一部分，仍不能视为完整一一对应验收。

## 已验证边界

- 管理员显式选择商户，商户只能使用会话身份；商户传任何merchant_id（包括空值/本人）均403。跨商户接收方/PS/PR/PF读取404，POST重复JSON键、无关字段与null拒绝；非规范游标/分页拒绝。
- 当前角色、管理员类别、CSRF先于业务执行。管理员首次bind/rule/PS/PR/PF动作在事务内消费具体资金授权，缺授权428且不外呼；原意图重放不重新消费、不重发。资金摘要使用同授权事务的AEAD/HMAC脱敏接收方及原订单、原声明、通道账户与规则版本快照。
- challenge绑定核对事实，不保证最终执行资格。公开页面先读取可信eligibility，核心资金事务在消费授权前再次验证；契约验证PS仍pending时即使持有PF challenge，也拒绝执行且0外呼、0授权消费。
- 完整账号/姓名仅用于草稿本次写入，提交即清空；页面只选择安全字段渲染，切换商户会取消旧请求、清除旧结果。模拟或未启用分享的通道明确不可用。支付宝持久接收方仅规范userId，不开放loginName/openId猜测。
- PS未知响应保留原号、锁定页面原业务号，通过本地GET恢复；只允许查原上游请求。逐接收方结果、执行结束与余款释放分别展示。
- PR只使用原成功receiver与原额度，资金成功回到原商户，不表示买家退款；unknown只查询原PR，页面禁止创建另一个活跃回退。
- PF先预览原gross应解冻金额与原merchant_net可释放金额。受理响应即使写FINISHED仍pending，不释放；可信query匹配原交易、原商户、金额才释放。source=sharing只显示原自动完结结果，无新PF。可信失败的新意图恢复必须重新预览、显式新号与新授权；unknown无该入口。

## 执行证据

2026-10-23本地执行，日志位于 `.tools/verification/sharing-console-2026-10-23/`：

| 检查 | 结果 |
|---|---|
| `go test -p 2 ./internal/server ./tests/contract -run TestChannelSharing -count=1` | exit0：server1.133s、contract12.055s（包含原分账余额契约、真实HTTP/TLS/PG闭环、PF失败显式恢复及challenge不绕过资格） |
| `go vet ./internal/server` | exit0 |
| `npm test` | 29文件、81测试全部通过 |
| `npm run build` | vue-tsc与Vite构建通过 |
| mock浏览器全组 `--grep-invert 'live\|local Docker'` | 78测试全部通过17.2s；新增8项覆盖四菜单、草稿脱敏/规则、PS查询、管理员PR资金密码、PF受理/核实 |
| `node docs/tools/build-openapi.mjs` | 离线结构检查通过：93路径、108操作；同步生成`api/console.openapi.json` |

真实HTTP契约使用随机独立PostgreSQL schema、实际WechatV3适配器、临时RSA验证密钥和本地TLS服务：签名AES-GCM回调使原100分支付入账；PS20分成功、80分可信释放；PR授权前0外呼，503结果未知，原号恢复/查询后10分回到原商户，余额90；第二150分支付仅PF受理时余额仍90/预留150，可信原商户解冻明细后余额240/预留0。第三70分原支付的PF收到签名PARAM_ERROR确定拒绝，旧号重放仍读failed；重新预览失败证据、新号和新授权后申请并可信查询，最终余额310/预留0。最后规则禁用和远端解绑也分别验证未授权428/0外呼、授权成功。所有账户/凭证/渠道预留、PR及PF检查均零异常。该流程未访问生产，也未发起真实资金交易。

API金额和身份约定详见 `api/README.md` 与 `docs/design/api-conventions.md`。支付宝独立PF、其它未验证产品、未知接收方关系恢复仍保持明确限制。

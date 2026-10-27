# risk177 最小累积集成契约

独立源：`work-riskcheck177-39cdd806-b87f-4ff1-b6c1-f3d886c89eaa`，原 175/1935 文件基线。自有 `.tools/integration184-v2` 逐 SHA 复制 184/2108 源后，按 175→177 变更进行三方按行合并。初始复制报告为 `.tools/integration184-sources-initial-v2.json`；失败的初始复制树保留但不是交付源。

需要显式处理的四个冲突：OpenAPI 用 schema/route 键合并保留双方；merchantnotice 模板追加 core_risk 并保留 171 场景；Queue 非 provider kind 保留 merchant.payment-disabled-notice 和 core-risk.notice；SettingsPage 同时保留 platformBusy 与 riskBusy 的全部鼠标/键盘/保存/刷新门，实际测试曾抓出漏合并风险锁。

171 只加 registry/execute 最小分支和 `core_risk_scheduled_check.go`，不重写原 Notify/paid_at 文件。helper 在 core risk 业务同 Tx 中验证原 run/lease/current generation，错误返回零提交计数。来源 SHA/最终变化清单在交付清单中逐个记录。

98 子通道的新增集成路径：

- `core_risk_child.go` 通过原 98 可读方法验证 route/quote/old account/fee birth；按父通道缓存已验证原子通道 heads，以原 enabled/archived 决定检查分支；不恢复账户或调用网络。
- `core_risk_check.go` 原通道规则分父/子命名空间、沿原最新 trade_no 次序；配置 check_channel_ids 仍筛父通道。
- `SubchannelCandidatesTx` 过滤独立风险阻断，实际发送最后的 `checkChildFirstSendTx` 使用相同 risk key 锁，再由原最终风险日界/会员时钟 gate 收尾。旧 attempt 恢复分支保持在所有新准入之前。
- `ProjectSubchannels` 增加 core_risk、parent_core_risk，只读 enabled 不改；UI 类型解析、风险原因、禁止支付测试入口都消费真实 DTO。
- migration102 增加 subchannel 处置 kind；通知依旧使用同一个冻结 AEAD/job/once 领取链及管理员既有工厂。

与 180 已直接协调：180 的 `parentChannelLock`、`parentAttemptAccount`、`parentAttemptFinal` 和动态 paymentWritesEnabled 必须保留；其 103 账户 head、96 fee、99 limits、98 child capability 不得被风险层替代。root 最终 firstsend 合并应依次保存原意图/各证明、执行所有 parent/child 最终门，最后执行 core risk 的数据库时钟门后提交，不能被某个 bound 分支提前返回。该 184 验证树不含未冻结 180/185 代码，不能称完整累计部署已通过。

所有测试只使用专属合成 PG、进程内或 loopback TLS 测试通道，真实发送次数指测试 provider/sink 的记录。桌面可见旧预览服务和主目录未被替换；没有 Git 暂存、提交、推送、SSH、生产访问、真实支付或出款。


## v2 独立复审与性能

独立复审实际抓到并已永久回归的缺陷：仅配置组、随后所有102表/marker/登记丢失时，旧 schema 探针漏组独立审计；现探针识别 policy、groups、origins、success、actions、check、notices 七域锚。读安全状态把原动作链与独立审计合成固定批读，所有 revision 仍逐 MAC 验证；原子通道列表的 2/20 项实际 SQL 数从 63/189 降至 56/56，动作 2/20 revision 均为2次 SQL。

IP 准入选当前业务日成功、当前间隔和仍未决的原来源，并从独立 admit/confirm 审计反查缺源。随后同 Tx 一次联读原 attempt/order/receipt/success，逐一认证原 MAC 和绑定；不是缓存权限或只信可写的索引列。原 admit/confirm 审计分别冻结固定 UTC admitted_at 与业务 day，新增索引支持选集。跨日的已验证支付即便 attempt 仍 pending，也不再占未付款槽。永久实际测试对 2/100001 个非财务 terminal-failed 来源均3次 SQL（100001时约561ms），2/20 个 pending 来源均4次 SQL，删未决来源拒绝；实际 markPaid+receipt 的当前日占槽、跨日释放及删除当前成功来源拒绝另有测试。大历史 fixture 只写本地合成失败记录，使用 DB 出生钟和测试未来观察钟，不伪造历史财务成功。

买家复用同一个批读保持旧跨日 OpenID/UID 认证别名关系；目前单个买家超过10万原相关来源仍明确拒绝，未以丢历史别名来冒充优化。104 的 C 实际收费接入由独立104补丁完成：177当前 Amount 为M，不应宣称104累计测试已通过；既有成功 C 阈值不改成加上新订单的预算上限。

## 公开地理资源交接

本机原材料没有 xdb。仅从官方 lionsoul2014/ip2region 固定 commit `70d862dbc7ee26c7de6e1adf4c25e63250de2606` 取得公开 IPv4/IPv6 数据与许可证（Apache-2.0 OR MIT）。资源只存独立 `.tools/riskcheck177-official-geo-70d862dbc7ee`，不在发布源码 allowlist；SHA与原始官方URL见独立 acquisition 收据。真实数据离线查询以及实际 HTTP 国内准入/海外拒绝/扫码错误有界处理已通过，不能把该测试理解为地理准确性认证。部署只读挂载目录，配置 `GOEPAY_RISK_GEO_IPV4_XDB`、`GOEPAY_RISK_GEO_IPV6_XDB`，仍保持风险功能默认关闭；不要把专有参考、运行数据或环境密钥放进源码。

v1 源码索引保留不可变；v2 分别输出实际开发源清单与官方 source-manifest 发布 allowlist，旧 SOURCE_CHECKPOINT 仅是取源锚，不是新发布内容。新 `installCoreRisk177API` 为纯增量安装器，由新父生成器导入后生成 API，不能整份覆盖旧184 API到186。

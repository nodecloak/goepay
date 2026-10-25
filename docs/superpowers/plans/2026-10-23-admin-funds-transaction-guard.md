# 管理员资金事务授权接线

**Goal:** 管理员对明确金融动作逐笔确认，授权消费、业务意图及资金预留同一 PostgreSQL 事务提交；商户签名接口及只读核实继续按原合同工作。

**Architecture:** identity41持有凭据、会话绑定、一次性challenge/grant；payment只接受请求context中的事务guard。Server将严格canonical原请求与数据库原资源/资金政策生成域隔离HMAC，预览及guard内调用同一builder。除摘要外不存储原收款信息。所有历史同号意图先执行原幂等比较；首次新意图才消费授权。跨午夜、事务失败、审计失败均不可留下半个授权或资金变更。

**Tech Stack:** Go、pgx、PostgreSQL、Vue3；在E:\GOEPAY实施，禁止生产外呼、真实资金交易和读取.secrets。

## 接口与顺序

- payment.WithFundsActionGuard(context.Context, FundsActionGuard)注入请求局部回调，requireFundsAction(ctx,tx,kind,key)在真实业务锁后、首条资金/意图写入前调用。无回调保持签名商户及worker既有行为；admin middleware必须对所有受保护POST设置回调，不能因服务nil跳过已开启政策。
- 首批覆盖退款预留、单笔代付、内部资金申请/审批、押金申请/审批、订单资金冻结/解冻、授权扣款/解除、关闭订单、规则分账、批次准备/取消/执行。退款的授权归属原reserve意图，后续既有provider operation不重复消费。
- 批次执行必须独立持久execution intent固定原批次和最多limit个unsent IDs；首claim同事务消费授权，后续每项只能在该intent集合内各claim一次。不能把已消费grant当作全会话通行证。
- 同号重放在已有记录完整tuple比较后返回原状态；未知结果保留资金，只查原号，不能新号或自动重发。

## 测试任务

- [ ] 先真实PG红测：拒绝guard后退款/代付余额及意图均不变；允许guard写标记与业务共commit；业务后续故障回滚标记；同号重放不再次调用guard。
- [ ] 对每个首意图入口加明确kind+business key，按真实路由证明覆盖，不在查询/回调加授权。
- [ ] Server preview拒任意路径、重复JSON键、跨动作及会话；费率/收款/通道配置改变须重新确认。授权失败返回428，错误不伪装202。
- [ ] 真实PG + 临时TLS证明12并发至多单发，取消/审批/发送竞争及密码轮换/logout/TTL/审计故障均失败关闭。
- [ ] 前端设置与动作确认分别测试，密码/grant不进storage/URL/日志；用户明确执行后只发送一次原body，未知结果只提示查询原业务。

所有步骤结束后更新接口、动作矩阵与真实测试记录，整体部署本地Docker并备份恢复验证。不能将凭据接口单独计作资金授权已完成。

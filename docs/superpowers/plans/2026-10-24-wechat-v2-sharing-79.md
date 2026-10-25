# Wechat V2 Sharing79 Implementation Plan

**Goal:** 在独立V2产品中实现本地原SDK已提供的直连单次分账，并复用原不可变资金来源。

**Architecture:** 独立XML/HMAC适配器复用现有ChannelSharingProvider接口；原Go分账状态机继续控制声明、预留、claim和原号查询。79只新增必要的产品约束兼容，旧MAC与1..78迁移不变。

**Tech Stack:** Go1.27.3、PostgreSQL18、Vue3、隔离PG及本地TLS/mTLS。

**Spec:** `docs/design/wechat-v2-sharing-79.md`

## Constraints

- 只在E:\GOEPAY主区开发；1223文件固定92不变。
- 禁止生产、真实密钥、实际支付机构调用及真实资金交易；其他业务插件仍暂缓。
- 原支付与撤销继续MD5；V2分账独立HMAC-SHA256，不隐含网络重试。
- 每个外部金融写先提交原claim/审计；未知只查原号，未认证失败不释放预留。

## Deliverables

- [x] 接收方与传输：新建`internal/adapters/payment/wechat_v2_sharing_transport.go`、`wechat_v2_sharing_receiver.go`及对应测试；`WechatV2Config.EnableChannelSharing`默认false。先以真实本地TLS的HMAC原请求、回显错配/签名及默认关闭测试失败，再实现`ChannelSharingCapabilities`、`ValidateChannelSharingReceiver`、`BindChannelSharingReceiver`、`UnbindChannelSharingReceiver`。不改原exchange的MD5行为。
- [x] 单次/查询/余款：新增`wechat_v2_sharing.go`及测试，实现现有`ValidateChannelSharing`、`SubmitChannelSharing`、`QueryChannelSharing`和`ChannelSharingRemaining`接口。先断言FINISHED+部分失败/缺接收方/金额改变不成为整体成功；仅支持明确unfreeze_unsplit=true的单次语义。原请求/返回单号须完全匹配，实际mTLS发送一次。
- [x] 回退/完结：新增`wechat_v2_sharing_completion.go`及测试；复用现有请求/结果类型，先测个人接收方回退零IO拒绝、原单预算、完结金额与余款证据。禁止以通信SUCCESS代替最终成功。
- [x] 持久与初次声明：新建`db/migrations/000079_wechat_v2_single_sharing.up.sql`，扩展`channel_sharing_receiver.go`、`channel_sharing_observation.go`、`channel_sharing_return.go`、`channel_sharing_finish.go`中的明确产品分支。先构建真实78→79数据保持与V2 TLS/PG资金测试；旧行、旧MAC及历史无声明单保持。再接工厂与普通/扫码首次声明；false摘要完全保留78语义，true单独绑定。
- [ ] 接口/界面与全量：复用原后台字段与资金确认，补产品显示、原始SDK签名契约和实际两后台测试；更新OpenAPI及差异说明。固定新源码，完整两平台PG/race、前端、PHP与依赖扫描，通过后再无重置本地升级、9项实际浏览器、双通道模拟和全表备份恢复。不能把79专项拼接成完整验收。

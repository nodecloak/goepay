# 原父通道微信应用关联

原版依据为本地 Pro `index-DNfqr2Hp.js` 的 `appwxmp`、`appwxa` 选择与 `channel-DoFrrF6Z.js` 的 `batchBindWeixin({type,weixin,checkbox})`，以及本地 Release `plugins/wxpay/wxpay_plugin.php` 156–177 行：公众号/小程序的 AppID 实际进入 JSAPI 支付，不能只换 OAuth 资料。仅参考字段和行为，不发布原 PHP/前端包。

## 界面与接口

父通道编辑保留原公众号与小程序下拉、0 取消关联、当前 AppID 展示、同 ID 重新关联。多选提供原子批量关联公众号/小程序，确认原通道集合后提交。选项来自可信加密导入中的微信 IdentityProfileSpec，遵守其父授权来源及商户列表，通用应用元信息不能冒充可执行凭据。跨来源没有共同应用时只允许取消关联。

新增只读 `GET /console/v1/admin/payment-channels/{id}/weixin-options`，以及 `POST /console/v1/admin/payment-channels/bind-weixin`。单条/创建/复制通过原 parent account mutation 使用 `appwxmp`、`appwxa`、`expected_bindings_fingerprint`。ID 一律完整十进制字符串；JSON 字段精确匹配，拒绝重复、大小写别名、null 和浮点 ID。所有目录和配置路径仅当前本人持久 super 可用，禁止 operator、失效会话及停用管理员，响应 no-store，目录不返回 secret。

## 执行与恢复

账户密文增加可选 weixin_bindings，封存真实可信 profile 内容及商户范围。未使用关联的旧 `{version,channel}` 序列化不变。WebSet/MiniSet 区分省略与明确0，首次只改一侧保留另一侧 legacy identity；明确0只清所选侧。

ParentChannelExecution 的 method selection 只投影该账户版本的整套 SDK。公众号、微信小程序与普通 native/App 使用各自真实 AppID；Provider.Query/Refund 不依赖临时 Create 状态。原103支付来源增加可选 PaymentVariant 和 ExecutionAccountHash，旧 nil 编码及原96/99费用/账户锚不变。旧恢复由已认证 source.Method/variant、原账户密文与独立账户 hash 重构，回调、查单、退款均固定原 SDK。缺源、篡改密文及同值大小写别名拒绝并且不发请求。

新 route 同时检查 sealed profile 与当前可信目录一致。目录更新后不会静默替换原账户 SDK，也不会让新订单继续使用过期资料；界面明确提示“应用资料已更新”，同 ID 可通过“重新关联所选应用”保存新账户版本。已经封存的原订单仍按原 SDK 恢复，当前 enabled/商户范围继续限制新的资金写入。

绑定本身不启用未选择的 JSAPI 产品。V2/V3 小程序启动均复用原可信 app/page/completion URL 验证；已有 direct mini_appid 必须等于同账户 payment appid，跨 AppID 的旧 direct 配置不会被推断成已支持。新的可信 mini 关联通过自己的 AppID/path 构造 SDK。子通道 SDK 不由父关联替换；98/102 与103的累计集成仍须由主任务单独验收。

## 验证

已保留 input/unknown-ID/catalog/batch/UI、单侧 legacy 保留、未启用产品、V3 mini metadata、目录轮换新单限制的真实失败及修复记录。实际独立 PostgreSQL 和本地 TLS 验证了 Web/mini 不同 AppID、账户和 profile A→B 后原 A 查单/退款/HTTP 回调；真实 Handler 的 app→applet→原 checkout→本地 TLS OAuth 一次 code→原 V2 mTLS / V3 RSA 支付完成，重复 code 不再发请求。

实际 HTTP DTO 已导出为无秘密测试样本，TypeScript 严格解析，并在375与1440宽度测试编辑、批量确认、未知结果读取原目标和 operator 不可见。测试支付行入口只打开确认窗口，不自动提交。最终通过数量和源码 SHA 以独立审查清单、完整回归收据为准；本文件不声称整个项目或所有原插件已经完成。

没有访问生产环境、真实通道或发起真实资金交易。

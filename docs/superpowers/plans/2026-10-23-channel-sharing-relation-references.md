# 渠道接收关系和本地引用

48/49 已冻结，本次只新增 51。接收方草稿保留各 owner 的原始 AEAD 资料和幂等号，但不占用远端关系。绑定时才在物理关系及鉴权关系的全局锁内注册关系；全局未知或已发送待结果不能由其他别名重发。

全局 registry 保存原物理 hash、鉴权 hash、实际源 receiver、最后远端 operation 及可信远端 bind proof。鉴权 hash 仅对活跃关系唯一；已明确失败且未产生关系的操作不永久占坑。同一物理关系只有一个 registry。各本地绑定以独立租约记录，引用操作必须明确 source=reference，并引用原成功 remote bind。引用只允许与原 source 的 provider/app/product/账户 scope 和完整接收方资料相同，不宣称再次向上游验证姓名。

锁顺序：排序后的物理/鉴权 advisory locks → registry → receiver → operation/lease。规则创建仍锁 receiver；它不取得 registry，因此解绑必须在 receiver 锁后再次检查当前规则和原支付引用。

有其他活跃租约时，解绑只释放当前本地租约，不发送远端 delete；最后一份租约且所有原规则/支付均无引用时才声明一次远端解绑。前置 claim、原 actor 和每步审计同事务提交。远端结果审计失败保持全局 claimed 屏障；引用结果审计失败整体回滚，没有外呼。

51 回填已有原成功绑定和当前活跃租约，不重写 48/49；仅新增明确 proof 的约束认可 reference 成功。未知、binding、unbinding 不会被回填为成功。多 App 与实际 PID 的归属证据仍不足：活跃关系不允许跨 App 引用或更改声明 Seller；该限制不能当成完整支付宝多应用兼容。

验证：草稿/明确失败不占坑、跨 owner/channel 相同资料引用、并发一次远端 bind/delete、规则和原支付保护、全局 unknown/claimed 屏障、审计失败回滚和直接 SQL 无 proof 拒绝。旧测试中把所有别名创建当 ErrConflict 的断言需根任务在新语义验证后更新。

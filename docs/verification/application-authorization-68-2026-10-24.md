# 独立支付宝应用授权集成开发验证

2026-10-24，E:\GOEPAY。当前正式本地Docker页面仍为schema62。本文记录66数据库迁移及68/69集成候选，不宣称已完成部署、真实授权或全部PHP兼容。

## 实现范围

独立加密ISV应用档案明确issuer AppID、预期授权AppID、获准商户、基础/指定应用及应用类型。默认关闭，启用要求OAuth总开关、独立开关、configured模式及HTTPS；付款写开关不授予应用授权，关闭付款写不阻断已明确启用的独立用途。配置加载/能力读取零厂商I/O，无自动通道绑定。

专用表、事件和code claim仅接受本人原始super会话，拒商户、operator及代登录。封存原用户授权版本、目标商户配置版本和完整档案指纹；商户停用再启用也不能恢复旧请求。原state/browser/authorization token分域；期限5分钟且不超过会话。先提交原app_auth_code消费与事件再执行一次交换；用户auth_code没有回退入口。结果独立AEAD存储，app_auth_token不在普通DTO中序列化，refresh不保留。只有原始会话的一次显式reveal在提交后返回凭证；未知交换/未知展示不能重发或重显。

基础链接、指定应用PC及钱包链接依据本地AlipayOauthService SDK。回调核对开发者app_id的依据也见[阿里官方第三方应用授权说明](https://developer.alibaba.com/docs/doc.htm?articleId=104110&docType=1&treeId=94)：app_auth_code与开发者AppID用于应用授权，enctraceid不参与业务判断。当前实现另要求原state与浏览器绑定；真实平台回传、商户签约和权限没有进行实际授权验收，不将本地SDK及合成测试视为线上许可或100%行为证明。

固定交换方法`alipay.open.auth.token.app`使用原签名客户端，验证精确响应节点、签名/证书和预期授权AppID。HTTPS固定目的地，不接受配置网关替换，拒绝重定向；清除GetBody以禁止HTTP内部请求体重放。错误不返回厂商原文或凭证。

前端仅原会话super可见独立第三页签；明确选择商户和档案并确认后创建，PC/钱包只选一个原入口。封存能力响应中的可信`authorization_origin`，允许与后台域名不同，同时核对路径、nonce和无userinfo/query/fragment。凭证只保存在当前内存的password字段；过期、上下文切换或卸载清除；没有自动复制、storage、厂商轮询、付款或自动绑定。

## 审查及测试证据

- 新适配器缺失API真实RED→合成TLS GREEN；证书SN/期限、错误签名/节点、错误AppID、重复JSON、HTTP异常、回调歧义、固定链接与refresh丢弃覆盖。
- 审查发现GetBody可被Go在未写入时内部重放；独立传输边界真实RED，清除后完整适配器包通过6.832秒。日志`.tools/app-auth-transport-replay-{red,green}.log`。
- 核心14顶层及8绑定子测试通过76.398秒，包含并发交换/展示、错误会话、降权/撤销、商户版本、配置变化、过期、审计故障、伪造processing/claim、AEAD篡改及跨请求密文交换。见`.tools/application-auth-core-final.log`及三文件SHA。
- HTTP真实合成TLS覆盖基础、指定PC、指定钱包、交换期间撤销、未知后不重发；另验证merchant/operator/delegated/重复cookie/CSRF/参数范围。最终通过23.696秒，所有资金表/任务无新增。见`.tools/application-auth-http-pc-wallet-final.log`。测试夹具曾误降级最后一个super、误把正确会话撤销401预期为403；原失败日志保留，改为保留合成backup并分别验证旧会话401/新operator403，没有放宽业务保护。
- 前端审查发现空userinfo被URL标准化隐藏，先保存8个失败断言，再补raw authority拒绝；最终145单元通过。原上下文、未知结果、独立域名与秘密清理审查无其它阻塞。
- 工作源码Go全项目vet与cmd构建通过。OpenAPI生成149路径、174操作，内部引用与路径参数检查通过。

## 固定候选和验收状态

- 68候选：`.tools/checkpoints/68-app-auth-integrated-4e75afb2-0418-49ca-88f0-086045288444`，885源码，63资金核心29文件SHA不变。最终历史65保存点修正、打印连接器和新应用授权均在内。Windows全量及Ubuntu Linux race使用独立PG实例运行，最终退出文件须完成后再认定通过。
- 68固定前端npm ci、145单元和构建通过，207模块，CSS `index-DmROCCpz.css`、JS `index-Dr4m3E7t.js`。
- 68第一次全量mock：100通过/3失败/9跳过；失败是新测试网络拦截规则写死5175，在5178阻止自身导航。保留该快照和日志。
- 69修正候选：`.tools/browser69-a1eab206/.tools/checkpoints/69-browser-port-fixed-268c19a9-39b9-41a3-8af0-bcf88744e138`，885源码，仅同一e2e文件改为读取配置baseURL；Go、产品前端、依赖、构建和所有运行源码与68逐字节一致，PARENT_COMPARISON记录差异闭集。完整mock重跑103通过27.8秒，9 live跳过。它复用68同字节运行代码的全量Go/构建验证，不假称已另跑一套不同后端。
- 新微信URL Link适配器是之后的独立开发，未包含在上述候选，不把新增文件存在计为功能已接通。

Docker新容器创建在WSL内核网络命名空间RTNL锁等待，既有PostgreSQL仍服务测试。未重启Docker/WSL、停止其它项目或修改8181。Ubuntu显式Go1.27.3+GCC、隔离Linux编译缓存和已验证模块图启动完整race；新WSL探针曾超时，但既有测试进程已输出通过的包，不能把探针失败混作测试失败或成功。独立native数据库/API/前端运行验收另行记录；恢复、实际运行和完整回归仍按最终结果更新。

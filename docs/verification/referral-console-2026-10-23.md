# 邀请推广专用控制台增量验证

2026-10-23，`E:\GOEPAY`。只使用本地假资料、httptest、随机schema PostgreSQL与mock浏览器；没有访问生产、读取真实密钥、调用资金provider或重部署容器。本文覆盖邀请控制台与注册预填增量，不代替56–59资金核心、全项目候选或加密PHP行为验收。核心退款回收闭环及独立审查通过前，不部署奖励配置。

## 交付行为与边界

商户原“邀请返现”菜单使用专用接口：`/console/v1/merchant/referral/overview`、`token`、`token/rotate`、`token/revoke`、`invitees`与`rewards`。只允许商户本人且拒绝代登录Issuer；管理员不能构造商户actor读取或签发。列表采用规范十进制after_id和1–100的limit，无商户选择参数。关系列表cursor为关系ID；满页之后可能需要一次空页确认结束。列表不暴露OTP、邮箱或证明材料。

令牌变更需CSRF、当前版本CAS，按账号+IP限速10次/15分钟。完整bearer只在签发响应返回，普通GET仅当前掩码和90天有效期；轮换或撤销后旧令牌失效，既有注册关系保留。页面使用密码输入框展示本次链接，只在当前组件内存保留；刷新、离开或重新读取清除，未知响应不自动重试，必须先重读版本。

链接使用fragment的promotion_token。应用在创建路由及发出任何API前提取并从query/fragment移除该参数；重复或非法参数清除并丢弃。路由ready后才挂载应用，避免初始错误area清除合法预填。注册表单可以手动粘贴、编辑或清空；仅明确OTP注册提交时把promotion_token写入JSON。准入invite_code独立；不会仅凭URL自动注册、绑定或发送验证码。令牌不写localStorage/sessionStorage、日志或常规截图；Nginx使用no-referrer且访问日志不带query，新链接避免初始HTTP请求携带令牌。旧query链接仍会抵达初始HTTP服务器，这是query本身的限制。

统计与奖励条目以整数分字符串传输、BigInt精确格式化，分别显示累计已发、累计已撤、退款暂存及UTC+08日统计，明确不是当前可提现余额。页面没有根据历史金额新增提现动作。异步操作在离开组件后不回填秘密或启动新请求。

超级管理员补充页`/system/referral-policies`使用`/console/v1/admin/referral-policies/{scope}/{scope_id}`专用GET/POST，仅super且无Issuer。global/0或group/正组ID；POST六字段必须齐全：expected字符串revision、enabled布尔或null、rate_bps整数0–10000或null、mode=immediate、basis=fee、refund_policy=any_successful_refund_voids_reward。组null继承，0显式覆盖；全局不能null。二次确认冻结原body，走既有admin.referral_policy动作授权，授权目标scope:id:expected；更改body不能使用原grant。未知结果需要重读版本。资金挑战摘要不保证最终执行资格，核心事务重新核实。

未开放日结、利润或会员推广返现，不推测加密Pro controller内部JSON。原精确入口GET `/user/invite/inviteInfo`、POST `/user/invite/inviteList`复用新安全DTO、Go会话及CSRF；响应`X-Goepay-Contract`明确safe-referral-v1/non-PHP-parity。旧current/size、offset/limit或PHP session不被伪称兼容。Cookie Path不扩宽，普通浏览器不会将当前Go商户Cookie自动送到旧/user路径；显式HTTP客户端可携带Go会话调用别名。Nginx只加这两个精确入口代理。

## 验证与证据

最初handler/路由、URL清理、界面、策略body测试在未实现时失败后转绿。真实浏览器还发现并修复初始路由未ready导致预填丢失；组件延迟POST测试先证明卸载后会额外GET，再以active生命周期门修复。资金策略浏览器最初因测试mock缺credential_epoch/totp_required被客户端正确阻断，补齐假数据而未放宽业务校验。实际HTTP授权检查使用admin_funds_grants已消费记录及真实动作名，未用假表证明授权。

| 最终检查 | 结果 |
| --- | --- |
| `go test -p 1 ./internal/server ./tests/contract -run 'TestReferralConsole\|TestTransferBatchImport' -count=1` | server 0.724s、contract 7.949s，exit0 |
| `go vet ./internal/server` | exit0，无诊断 |
| `npm test` | 32文件、89测试，1.84s，全部通过 |
| `npm run build` | vue-tsc与Vite 181模块构建通过，1.55s |
| 所有非live Playwright spec，workers=4 | 83/83通过，18.9s |
| `node docs/tools/build-openapi.mjs` | 103 paths / 119 operations，生成器检查通过 |
| `node docs/tools/build-acceptance.mjs` | 原82叶子：80 partial、0 pending、2 deferred；full_behavior_acceptance=false |
| 本地nginx:alpine，network none，配置只读挂载`nginx -t` | exit0，语法成功；未部署服务 |

真实PG HTTP合同包含：当前token首次只读、一次签发与掩码读取、旧版本409、跨角色/Issuer拒绝、规范cursor/未知query拒绝；真实本地OTP+独立准入码+显式promotion注册生成一条关系且不凭注册发奖励；别名方法/CSRF；撤销保留关系；不支持daily拒绝；真实资金口令设置、无grant428、挑战和确认、改body原grant拒绝、原body成功且仅一条grant消费、旧CAS不再变更。

浏览器新增3项覆盖super策略确认与operator拒绝；fragment在请求及Referer前清理、手动令牌和准入码独立提交；商户统计、两步签发、密码框、内存清理及不发泛CRUD请求。此spec明确关闭trace，避免将合成bearer写入trace。全量mock回归同时覆盖原注册、转账、结算、资金授权、渠道分账及批次页面。本次未执行live spec，真实PG合同不是生产接口测试。

独立只读审查已核对merchant API、注册预填和URL清理，未发现需阻塞的问题；这不替代其他agent正在进行的奖励/退款资金核心审查。

原始日志位于`.tools/verification/referral-console-2026-10-23/`，包括go-tests、go-vet、web-tests、web-build、browser-tests、nginx-config。source-hashes.json记录本增量文件在归档时的SHA256；共享入口或生成文件可能随后随主候选改变，应以主候选最终manifest为准。证据文件自身不参与自身hash。

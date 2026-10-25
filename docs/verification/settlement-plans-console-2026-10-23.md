# 60专用结算计划前端与接口文档验证

2026-10-23，`E:\GOEPAY`。本增量仅修改web、OpenAPI生成和接口/能力文档；没有修改payment核心、server、迁移或公共资金后端。未访问生产、真实密钥或真实资金接口，未部署。以下是前端单元/mock浏览器证据，不代替60核心真实PostgreSQL、HTTP资金合同及全项目固定候选验收。

## 交付范围

- admin补充入口`/settle/plans`只向R_SUPER提供，组件另要求本人super会话。专用列表、详情、默认关闭的新定义、原revision修改、独立授权、紧急撤销、当前due slot扫描及运行详情；不走通用资源CRUD。
- merchant补充入口`/account/settlement-schedule`只允许本人会话，默认关闭，明确参与与当前脱敏收款配置CAS。原已绑定recipient_config_id（初始0）和当前recipient.config_id分别展示；配置变化不能把旧同意自动应用到新账号，PUT必须明确确认当前ID。
- 原82个菜单计数不改变。普通cron资料仍为资料，启用不暗示资金执行。

计划仅`generate_only`：最多100个去重显式商户，hour/day/once，interval为字符串（1–168小时/1–366天/单次0），anchor_at必须带时区的RFC3339整秒，day显式时分秒、非day全0。按UTC+08业务日、固定D1资格。只生成待审核申请并冻结；原独立审批及出款授权仍需执行。无enabled PATCH，无threshold、百分比留存、分组daycut或自动付款UI。

授权冻结原body：expected_revision、expected_authorization_id、valid_from、expires_at、max_item_gross、max_run_gross、max_day_gross、max_day_items，金额明确标注人民币分字符串，ID/修订/数量也为字符串。资金动作前端白名单仅规范正ID的POST `/settlement-plans/{id}/authorizations`；不包含创建、修改、扫描或紧急撤销。App精准强制此动作使用资金密码流程，即使全局enforced=false；其他资金动作仍遵守原开关语义。未配置本人资金密码时0授权POST。

定义保存与授权、授权与扫描分别确认。扫描body只有expected_revision，检查202与原plan_id/run_id才显示已受理；不允许用户输入日期或任意slot，不自动重发。completed显示扫描完成，页面不称已审批/已付款。未知写结果需要原记录重读；紧急撤销仍能显式执行，空JSON且无需资金grant。切换计划中止原请求，延迟旧run不能覆盖新计划；卸载后的保存响应不启动新读取。

## 红绿测试

1. 精确授权allowlist、专用路由测试在原实现上失败，然后通过。
2. 定义、授权body helper首先在not-implemented stub上失败，然后通过；包括去重范围、显式schedule、固定mode、规范整数和授权时窗。
3. 新浏览器测试先因无专用页面行为失败，接入后通过。
4. 发现未知扫描把紧急revoke一并阻塞，先以真实组件测试复现再允许仅revoke绕过unknown写保护；没有放开新授权/新扫描。
5. 延迟run详情跨plan显示及HTTP200扫描伪受理均先以组件测试失败，再加入请求/plan身份检查与202检查后通过。

## 最终前端证据

| 命令 | 结果 |
| --- | --- |
| `npm test` | 34文件、97测试全部通过，2.09s |
| `npm run build` | vue-tsc通过；Vite186模块，2.47s，exit0 |
| 所有非live Playwright spec，workers=4 | 86/86通过，19.8s |
| `node docs/tools/build-openapi.mjs` | 110 paths / 130 operations，引用/operationId/路径参数检查通过 |
| `node docs/tools/build-acceptance.mjs` | 原82叶子仍80 partial、0 pending、2 deferred，full_behavior_acceptance=false |

新增3项浏览器验证分别覆盖：super计划详情/确认扫描202、未配置资金密码0授权POST/operator禁止访问；375px商户显式参与与当前脱敏收款配置CAS且不创建结算申请；global enforced=false但configured=true时仍实际challenge→摘要确认→密码confirm→精确原body+grant header执行，仅一次，浏览器存储无凭据。原全部注册、分账、转账、结算和资金授权mock回归同时通过。本次未执行live spec；后端真实HTTP/PG证据由主候选另行记录。

OpenAPI使用独立`docs/tools/settlement-plans-openapi.mjs`，写明admin或merchant确定路径、本人权限/CSRF、字段单位、默认关闭、强制grant、仅due slot扫描及原审批/付款边界。不是加密PHP后台完整兼容声明。

日志位于`.tools/verification/settlement-plans-console-2026-10-23/`的web-tests.txt、web-build.txt、browser-tests.txt；source-hashes.json固定本增量源码与生成文档归档SHA256。共享App/menu/生成器若随后合并主候选，以主候选最终manifest为准。

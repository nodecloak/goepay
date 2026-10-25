# 原商户pay与注册审核82阶段验证

本地2026-10-25记录（执行UTC2026-10-24），项目E:\GOEPAY。本阶段未作全项目或最终部署完成声明。

82固定1335文件，schema81，manifest SHA256 `3c79040b18215a7285b59564cce11fe4af3503db701d1ec37a2ee5d41d6b3fa7`，路径 `.tools/checkpoints/82-registration-stage-624c42b8-2ebf-4bdb-bc52-62c60963c92d`。固定快照逐文件校验不变；1..80迁移SHA与94一致。此后主区继续83，不使用82结果证明83新增代码。

- 注册/收款状态/后台专项：payment 8.452s、enrollment 36.209s、backoffice 8.346s通过。覆盖pay0/2拦截新订单及首次付款意图、原attempt仅恢复、原付款结果入账、审核后收款、profile同名值无权限、旧商户默认1、策略不回填、非法/重复user_review拒绝且不消费邮箱证明、审核不改余额/登录凭据及越权拒绝。
- 实际HTTP专项：待审核正常登录、本人只读查看、profile额外pay字段400、V1/V2下单拒绝且无新订单、管理员CSRF、审核后原会话保留。真实RED发现V2 pay_status错误来自普通profile，改为原列同MVCC读取后6.470s通过，仍使用原RSA响应。
- 固定快照完整运行backoffice/enrollment/server/tests/contract四包，UTC15:34:22→15:50:07，退出0，331个通过事件、4包全部pass。唯一跳过 `TestGenerateMerchantRSAPHPInterop`；对应PHP互操作在94独立执行，最终候选仍须再验证。两端Node客户端HTTP互操作在本次四包运行中实际启用并通过。
- 最新前端完整单元70文件/314项通过；类型检查/构建通过，584KB主chunk警告保留。只读静态快照5217上完整浏览器164通过、0失败/0flaky、9项部署验收opt-in跳过；跳过不算实际部署验收通过。
- 新增6项375/1280浏览器专项3.767s通过，覆盖管理员原pay字段和中文选择、独立账户状态、商户只读查看、注册待审核提示且不自动登录。首轮两个失败来自getByLabel读取选项文本；实际AX名称正确，改用combobox角色与准确字段名后通过，未修改正常表单标签。
- OpenAPI167路径/197操作，新增注册结果、公开审核能力、商户资料只读pay响应；注册及本人资料请求禁止提交pay。

机器证据：`.tools/payment-review82-affected-full-verified.json`、`.tools/payment-review82-final-all-unit.log`、`.tools/payment-review82-browser-all.json`、`.tools/payment-review82-browser-selector-green.json`。已查看375px截图：`web/test-results/runs/2026-10-24T15-28-59-609Z/payment-review82-merchant--24ae0-in-account-profile-at-375px/merchant-review-375.png`，待审核为只读文字，未增加付款权限输入。

没有重新完整运行internal/payment全包、完整仓库或Linux race；四包与定向测试不能替代。94全库只验证1310文件/schema80。最终集成须包含后续83的固定候选全库/race、原PHP、扫描、无重置部署、实际浏览器和全表恢复。收费注册、加密内部行为及未开放产品权限不因此宣称完全兼容。

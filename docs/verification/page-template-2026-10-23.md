# 61内置页面模板验证

2026-10-23，`E:\GOEPAY`。本期在冻结60之外新增schema61和专用模板包，不修改迁移1..60。测试只使用专属本地容器`goepay-template-test-61`、PostgreSQL端口55436与每测试随机schema，调用标准`db.Migrate`检查迁移；HTTP上游为本地测试服务，凭证均为测试生成。没有访问生产、读取真实渠道凭据、真实资金交易或部署服务。

## 已实现范围

三类内置模板`home/pay/qrpay`，各`default/compact`；只允许静态注册名称，无模板文件上传、路径拼接、任意HTML/JS、表达式eval或远程图片。原Pro模板controller加密，明文默认info.json没有非空配置字段；本期字段是新Go明确契约，不声称原加密JSON或全部第三方模板兼容。

配置包括品牌名称、说明、主题和背景颜色、本地标志、内容间距，pay另有显示订单名称开关。每类共享CAS修订，完整PUT拒未知/重复/null/错类型；切换保留各模板配置。写入仅本人super+CSRF，旧`template-configs`泛资源写由根任务封闭，操作员与代登录拒绝。

PNG/JPEG上传限制512KiB、边长2048、总像素1048576、并发2，先DecodeConfig再完整解码/重新编码，去除原metadata；规范输出限制2MiB，SHA256图片ID、固定MIME、数据库不可修改存储，不接受文件路径、SVG/GIF/WebP。私有编辑接口只对本人super可见；公共图片只在当前选中有效模板引用时公开，清除或切换后404，全部no-store。单独测试修正了嵌入bytes.Buffer意外继承WriteString绕过输出上限的实现问题，现使用私有buffer和唯一有界Write路径。

模板真实用于API源首页、provider checkout、mini launch以及聚合收款页面，显式每收款码配色仍优先。订单号/金额/商户、原HMAC/purpose/有效期、原收款应用验证、JSAPI/HTML/QR/app/preauth/scan支付材料分支、状态说明与支付行为仍由原服务控制。坏外观仅回退安全内置默认，并返回`X-Goepay-Template-Fallback`固定原因和结构化告警；原支付验证失败不能外观回退。预览仅固定`PREVIEW-ONLY`假资料，无订单创建或provider调用，脚本和表单禁止。

后台原`/admin/app/template`已使用专用页面：三类选择、静态字段、完整配置检查/确认、独立模板选择确认、私有图片草稿、保存后预览。保存/切换同类型CAS，目录与配置版本不一致时不显示可写表单；响应未知禁止重发，须显式重读。请求在离开页面时中止，不写storage或日志。

## 测试证据

原始输出目录：`.tools/verification/page-template-2026-10-23/`。

观察到的红→绿：新registry/parser、CAS/store、初始专用页面测试在stub阶段失败；HTTP首次返回旧硬编码checkout200而没有保存品牌，集成后绿。nil Pool公共页新增用例实际红`/: got 200, want 503`，显式503后绿。bounded writer用例实际证明继承WriteString可越界，改私有buffer后绿。浏览器首轮单一heading定位命中h1/h2两项，修正测试明确h2后绿（非业务缺陷）。HTTP canonical修订`01`原测试期望400，按既有服务契约修正为422后绿；不是放宽后端。另自查公开图片404缺失no-store可能负缓存，新增真实HTTP先红（`public-negative-cache-red.log`），再在模板公共匹配入口统一no-store后绿，成功、未发布、撤回和不可用响应均不能缓存。

- `go test -p 1 ./internal/pagetemplate ./internal/server -count=1 -v`：模板包完整7项通过10.257s；server全包通过4.699s，均未跳过数据库用例。见`go-packages.log`。
- 受影响原合同与本期合同目标回归16项：56.353s通过；包含collection、mini launch/OAuth、checkout回调/查单退款/close、原微信收款账户更换拒绝、轮询聚合、Alipay native、scan、preauth、web HTML/QR/app跳转。见`go-regression.log`。
- 最终模板真实HTTP两项补强后7.656s通过（同时server模板两项0.672s通过）。见`template-http.log`：保存品牌HTML转义、切换真实布局；checkout/collect无原签名403；显式每码配色仍优先；坏外观header可观测回退且非法签名仍403；商户/金额/名称不变、provider未重复提交、余额未增加。
- 其中专用API使用真实本地TLS server+PG：认证401、CSRF403、未知/重复query和JSON400、CAS409、非规范修订422；图片真实重编码私有→当前引用公开→切换404/no-store、metadata不回显；受限静态预览；orders/ledger_entries/jobs仍0，审计没有原品牌/上传metadata。
- `go vet ./internal/pagetemplate ./internal/server`：exit0，见空`go-vet.log`。
- `npm test`：38文件109项通过；`npm run build`：vue-tsc和Vite构建通过。见`frontend-unit.log`和`frontend-build.log`。
- 完整非live Playwright：92通过、8显式live跳过，22.0s。见`frontend-browser.log`；本期3项覆盖三类型字段、确认/CAS、私有图片草稿、受限iframe、未知响应重读、操作员拒绝、375px布局，不使用原泛CRUD。
- `npm audit --json`：0漏洞，见`npm-audit.json`。无新增运行库。
- OpenAPI独立`docs/tools/page-templates-openapi.mjs`由根任务接入；生成当时125路径/150操作。专用配置与文件上传不是资金API，不添加资金授权绕过路径。

## 固定与部署边界

本增量22个源文件及设计文件的SHA256见`source-sha256.json`，该清单文件SHA256为`c809657078d932b2989e6a925c1187b7655b52750e29f8fdac8f031211268ab7`。共享server/App/menu/Nginx/OpenAPI最终集成由根任务统一固定，本清单不替代根任务全项目候选。

独立审查先通过真实Nginx/Chromium复现全局XFO/CSP阻止preview的RED；根任务精确白名单location修正后，同一真实harness验证preview、内联样式和logo均可用，13条路径边界通过，其余console和相似路径仍DENY。iframe保留sandbox allow-same-origin：空sandbox负对照触发真实NotSameOrigin/CORP失败，故没有为预览放宽图片CORP或脚本权限。共享web/nginx.conf最终SHA256为`92beae17ec12042fbcfac3f399d937a3b8ea0be076d16d62ccbb60bbae0aadb9`，不在本方22文件清单内。独审另跑后端模板包13.359s/server1.147s/HTTP8.023s和5项前端单测均通过，报告当时未发现其它重要问题。完整62 Windows/Linux冻结候选的全库结果由根任务统一提供，本方增量结果不替代全库完成声明。

本期未将新代码部署到既有本地服务或生产；旧第三方主题、Pro加密模板返回、任意脚本/HTML和远程外观插件均不在本期声明范围。




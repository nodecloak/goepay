# Pro 后台对齐与剩余功能实施计划

> **For agentic workers:** 按下列责任边界连续实施、测试和审查。用户已经授权执行与本地部署，不再按单个任务请求确认。

**Goal:** 在完成剩余核心业务和本地验收的同时，使双后台的样式、交互及可验证字段尽量匹配本地epay_pro。

**Architecture:** 保留Go/PostgreSQL已有资金与权限边界；前端通过明确的页面字段映射适配现有API，复用Pro布局、视觉参数和交互信息。页面缺少的数据以专用、受权限保护的查询补齐，不把未知字段或内部资金证明直接展示给用户。

**Tech Stack:** Go、PostgreSQL18、Vue3、TypeScript、Vitest、Playwright、Docker Compose。

**Spec:** 用户2026-10-24新增指令；docs/remaining-core-work.md；本地epay_pro/public/static/assets与route源码。

## Global Constraints

- 项目目录E:\GOEPAY；不访问生产环境、不读取真实导入凭证、不发起真实资金交易；其他业务插件仍暂缓。
- 不修改已部署1..69迁移checksum；当前新增daily为70。冻结71..79均保持不变，最终总候选另行冻结。
- 页面金额单位、状态、权限和数据来源明确，所有ID/大整数继续使用十进制字符串；脱敏、CSRF、原会话与资金授权不能因样式对齐而削弱。
- 不执行原Pro bundle的外部请求代码；只读提取本地布局、CSS和字段证据，不引用外部CDN或字体。
- 新交互/业务先复现缺口测试，再实现并回归；CSS本身不写镜像式断言，使用真实浏览器截图、375px与桌面验收。
- 保留所有真实失败及环境中断，不以跳过、旧候选或模拟页面冒充最终全量验收。

### Task 1: 恢复隔离测试与建立最终验收基线

**Files:** .tools/DOCKER_MIGRATION_PAUSE.md；.tools/linux71-evidence-f8c39ab0f39e4eae97d48cc9888bbfd2/*；新专属Windows测试运行记录。

- [x] 用户明确迁移完成后解除暂停，检查本项目已有容器，不覆盖数据。
- [x] 等PostgreSQL恢复接受连接，检查schema与合成运行模式。
- [x] 冻结71用新GUID测试库重新运行Windows完整Go测试、vet和cmd构建；Linux独立detached容器运行完整race、vet/build并保留最终退出状态。
- [x] Go测试不因前端改动而丢失固定源码范围；最终前端另行冻结后跑完整单元、浏览器与实际联调。

### Task 2: Pro 视觉和导航交互

**Files:** web/src/style.css；web/src/App.vue（壳与展示计算）；web/src/components/MenuTree.vue；新增shell组件与专属交互测试；docs/design/pro-console-visual-baseline.md。

- [x] 从本地Pro admin/merchant布局与CSS中记录精准色值、导航尺寸、圆角、卡片、表格、表单、登录、页签与面包屑。
- [x] 使用真实折叠/导航/页签行为测试复现缺口，不改支付及权限逻辑。
- [x] 对齐管理后台深色导航与用户后台浅色导航、主色、密度和内容层次。
- [x] 375px和桌面真实浏览器复核，无水平溢出；菜单与原测试可访问语义保持稳定。

### Task 3: 字段、筛选与高频操作对齐

**Files:** web/src/components/{DataTable,OrderDetails,ListFilters,ResourcePage,ResourceFields,ReportPage}.vue；web/src/{list-filters,resources,reports}.ts；新增字段映射模块与对应测试；docs/design/pro-console-field-mapping.md。

- [x] 提取Pro订单、商户、资金记录和报表的字段名、列顺序、筛选项、状态与操作弹窗，逐项记源文件和Go对应字段。
- [x] 显式字段白名单替代无边界地把后端所有key追加成可见列；大整数和金额按映射格式化。
- [x] 已有后端字段直接正确映射；缺字段记录真实来源并补专用查询，禁止假数据、secret、收款明文或私有HMAC暴露。
- [x] 保留原资金确认、未知结果禁止重发和商户隔离，用单元与实际浏览器验证显示和操作语义。
- [x] App.vue列配置由root在壳改造完成后统一整合，避免跨agent覆盖。

### Task 4: 继续剩余业务并最终集成

**Files:** docs/remaining-core-work.md；docs/design/preauthorization-next-stage.md；docs/design/settlement-auto-payout-next.md；按明确协议新建相应Go/迁移/API/前端模块。

- [x] 根据Pro本地明文证据确定下一个可执行缺口，先完成原预授权查询/余款等有明确契约的操作；不从SDK枚举推导未知资金权限。
- [ ] 自动结算、阈值代付和返现扩展逐项建立原资金来源、有限授权、费用与幂等约束，再按专用计划连续实现。
- [ ] 最终固定候选执行全部Go、Linux race、前端、浏览器、API互操作、合成通道回调、无重置升级和真实备份恢复。
- [ ] 本地部署最终候选并验证双后台页面、配置模式默认关闭、账务对账和退出状态；只在所有已授权范围实际完成后汇总。

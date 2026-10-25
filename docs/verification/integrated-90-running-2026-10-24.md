# 90固定集成验证进行中

此页为开始时的历史记录；90现已完成完整验证及实际本地验收，当前结果以[90最终验收](integrated-90-2026-10-24.md)为准。

仅E:\GOEPAY合成本机测试。当前实际已验收部署89/schema75，8184/8185；90包含76通道停用/恢复/单次SMTP与恢复前在途响应边界，不含主区77机器人/Telegram/QQ运输。1..76固定源不得改动，不操作生产或真实资金。

固定源`.tools/checkpoints/90-channel-stop-integrated-aeb78c2b-4b19-42e8-8579-b003301d54fd`，1138文件，manifest SHA256 `23cbb93b14ffcc0b87ff3f05d4daa9ae319c50b2be709f08ca5a02ca03b44287`。原1..75迁移不变。第一次源清单遗漏89携带的两份75文档，注册前即被拒绝，目录和`.tools/rejected90-source-inventory.json`保留；现固定源明确携带已复核75/76文档，无原源删除。

完整后端仍运行，未发布：

- Windows：`.tools/goepay_windows90_b7623d834d2f4112aa5d51e121a4b2db`，07:30:36Z启动33包全量；专用GUID库、PG46回环55446。终态后才运行`.tools/verify-windows-full.mjs 90`。
- Linux：`.tools/linux90-full-a0979625`，模块与离线33包预检通过后07:33:22Z启动完整race/test/vet/build；专用internal网络和PG、无主机端口、源码只读。终态后运行同目录`verify-final.ps1`。
- `.tools/summarize90-progress.mjs`仅显示过程，不能当作完成证据。`.tools/verify-release90.mjs`已准备，尚未执行或授权部署；要求两个完整终态及下面全部证据。

已完成新执行：

- 前端`.tools/frozen90-frontend-0088c6a8caa84c97a093e00547870e7c/summary.json`：新安装67文件297单元/type/build通过，146mock/9实际部署opt-in跳过/0flake，37.514秒；全量和生产audit0，3产物/1138源SHA相同。静态5212无API代理；570KB提示保留。
- PHP：`.tools/php90-evidence-path.txt`指向的新RESULT，实际V1/V2、七SDK和商户RSA、9公开SDK、network-none且无DB、退出0，源一致。
- 源扫描：`.tools/security90/VERIFIED.json`，mod verify和扫描0，0导入/可达受影响符号；保留未导入OpenPGP的module-only GO-2026-5932和数据库时间2026-10-21。

90完整通过后，仍须从当前已验收89/schema75无重置升级到76，保留原178业务表、75迁移和正账，新增三表为空、无历史停用/通知回填。然后新执行两通道模拟、实际9项浏览器、182表/76校验和/22零计数的完整备份恢复。升级脚本尚未准备，不复用89已执行脚本改变现有部署。

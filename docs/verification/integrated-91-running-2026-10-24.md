> 本候选已完成验收部署，最终结果见[91完整验收](integrated-91-2026-10-24.md)。以下保留当时启动记录，不代表当前状态。

# 通知集成91完整验证进行中

当前实际已验收部署仍90/schema76，8184/8185；本候选包含77通知运输，尚未部署。固定源`E:\GOEPAY\.tools\checkpoints\91-admin-notice-integrated-c39749cf-3155-4271-ac83-925029d8d1cd`，1168文件，manifest SHA256 `587109f86e93ee7dd65609a2a77ddb529efc4be723916bf3821ee9ac69e62189`，相对90改43文件、无删除，1..76 SQL不变，77已固定不得修改。78和后续打印范围测试不在此候选内。

- Windows34包完整测试已在08:36:58 UTC开始，独立DB/evidence `.tools/goepay_windows91_7b47b37b14854eddacf44e10662eebd5`，后台PID23564。前轮90严格校验通过后启动；不是复用90结果。
- Linux34包完整race已在08:40:52 UTC开始，`.tools/linux91-full-f7c13d55`，容器`goepay-linux91-full-f7c13d55`，独立内部网`goepay-linux91-isolated-f7c13d55`与PG/volume，源只读、无宿主端口。预检实际退出0，模块字节相同、离线34包依赖可用；完整结果未完成。
- fresh前端`.tools/frozen91-frontend-71922c08c0a04372bbbb04f203de5e89/summary.json`：67文件299单元、typecheck/build成功，146mock/9live opt-in跳过/0失败/0flake（30.609秒），两audit为0，源码/三资产SHA不变；>500KB构建提示保留。静态5213仅固定资产，无API代理。首次finalizer因生产依赖audit尚未运行而缺文件；补跑production audit后才生成通过摘要，未重跑已通过测试。
- 实际PHP V1/V2+七SDK+商户RSA、9公开文件、network=none/无DB/退出0，见`.tools/php91-evidence-path.txt`所指目录RESULT。不是复用90。
- `.tools/security91/VERIFIED.json`：新源码扫描与模块校验0，可达/已导入包发现0；仍保留未导入OpenPGP模块级GO-2026-5932及2026-10-21库日期，不声称不存在所有安全问题。

`.tools/verify-release91.mjs`仅已准备且语法检查通过；需要两完整测试最终manifest/容器/日志结果都成功后才可执行。91部署/升级/live/restore工具尚未准备，必须以已验收90的76迁移、181旧业务表和当前18/116/232正数据为基线，77只增列不增表，最终仍182public表、22项检查。不得执行任何已接受90 live/restore/promotion脚本。

所有测试均合成、本地，真实通道/身份/通知/打印/自动付款/调度开关仍关闭。不访问生产或发起真实资金交易。完整测试进行中不是通过结论；不把专项合并冒称全部Pro兼容。

# 源码静态基线

来源：`E:\GOEPAY\epay_pro` 与 `E:\GOEPAY\epay_release`。生成命令：

```powershell
Set-Location -LiteralPath 'E:\GOEPAY'
node docs/tools/inventory.mjs
```

脚本只读取 PHP 包，写入本目录，不执行 PHP，不连接数据库或支付上游。重复运行会更新生成文件；正式冻结基线后先另存快照再重跑。

| 文件 | 作用 |
|---|---|
| summary.json | 统计摘要 |
| menus.csv | 双后台全部99个菜单节点，82个叶子页面，含隐藏、角色、原始路径 |
| feature-index.md | 自动生成的菜单叶子索引与控制器动作分组，便于阅读 |
| routes.csv | 4个路由文件共572条字面量静态路由，含行号与目标控制器 |
| tables.csv | Pro 38 / Release 29 张安装表及原字段定义 |
| plugins.csv | Release 60个支付插件入口与Pro 7个邮件/短信插件元数据 |
| encrypted-files.csv | 99个检测到Swoole Loader头的Pro PHP文件 |
| api-docs-extracted.txt | 24个内置文档页的正文提取；HTML实体可能保留，原HTML为准 |
| source-hashes.csv | 菜单、路由、SQL、文档与SDK压缩包的SHA-256版本指纹 |

统计边界：排除 vendor、node_modules、.git；路由统计包含闭包目标，不包括 miss、入口别名及动态 Addon 注册。`{admin_path}` 是可配置后台前缀，默认值见 route/admin.php。路由读取器针对当前源码格式做静态解析，运行时中间件、动态路由和框架匹配需要另外验证。

`tables.csv` 只表示 install.sql 的表定义；update2～5已经阅读并纳入版本指纹，但不把增量表重复累计为新安装表。不得将安装脚本与所有升级脚本盲目串行执行。

`plugins.csv` 的 methods 只表示静态函数声明，不证明能力正确、供应商仍支持或Pro行为一致。Loader检测只匹配已观察的头标记，未发现标记不等于所有代码都已审计。

文档/源码阅读不是运行验收。没有源数据库、完整Pro业务插件、原系统样本和配置时，不能依据这些清单宣称功能完整复刻。

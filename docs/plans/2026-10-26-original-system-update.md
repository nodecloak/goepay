# Go系统更新与原版UX合同

父为冻结176的1966发布源，隔离候选，不改变其它候选/主源码/冻结。原Pro public/static/assets/index-3p1ouazc.js 5810字节 SHA0fa7ae185f76b6318b061fae5323035ba2c1df57fb128f0de70e6f0f515e73d5：current_version/latest_version/latest_version_name/need_update/update_log；检测/重新检测、更新确认、失败重试、进度与结果。该原UI用Math.random累计到90的进度不是实际下载证据，Go实现不能沿用。原运行信息独立保留，不能冒充版本检查。

目标：系统更新专页匹配原双版本信息、更新记录、检查/重试、确认/取消/实际准备进度。当前版本/commit/source来自真实构建信息，开发未标记必须明确。更新源为部署指定的本地签名清单及本地包，测试只临时合成文件；默认未配置、没有外网更新/生产访问。受信Ed25519公钥由部署输入，不通过web更改。清单严格JSON、完整版本/目标平台/大小/SHA/签名/最小数据库要求；全部可显示信息与实际验证绑定，不把可编辑metadata叫发行版本。

原super持久会话/禁止代登录、CSRF、strict body/指纹CAS、最后数据库时钟进入实际准备流程。普通管理员可读非秘密版本信息，不能准备包。私有原路径/密钥不得GET或错误回显。准备操作固定读部署配置路径，不能请求任意URL/文件；只验证并写入本任务受控staging。验证大小/hash、ZIP路径/重复大小写/符号链接/压缩炸弹、声明的每个文件及既有迁移SHA保全；没有执行脚本、覆盖PHP/运行中Go二进制或给予Web Docker socket。相同清单/包为同一稳定intent，unknown先读状态，失败/取消/上下文变化不盲重建。

准备进度只取真实读取字节/校验阶段；已验证准备与已部署分开，只有重启后的真实构建版本/source吻合才显示运行升级完成。Go不可变容器使用经校验本地包及明确部署命令应用，不伪造随机进度/自动PHP覆盖。最终本任务会在用户已授权的本地Docker验证准备与新构建识别，不访问生产或真实支付。无新增迁移：使用原独立审计域记录非秘密不可变验证事实及本地受控artifact，不能新增未注册backoffice resource绕CHECK。

TDD：旧页面/实际API缺失RED；合法/最新/不可升级/签名错/大小或SHA错/历史迁移变更/平台不匹配/缺源/超时/取消/上下文变更/重放/并发/原会话到期与撤销，真实本地文件/PG/HTTP。前端真实HTTP fixture/1440和375、原字段/确认/取消/焦点/busy/unknown；完整单元/noEmit/browser与独立复审后整合。依赖178实际task-scheduler还必须加入Docker构建可执行文件，此部署组合缺口另在累计Docker变更中显式修正，不据Go编译通过假定镜像已含命令。

本阶段接口：GET /console/v1/admin/system-update；POST /prepare；GET /preparations/{intent}；POST /preparations/{intent}/cancel、/retry（均在同一system-update前缀）。prepare的intent与expected_fingerprint必须相等；重试同时绑定原intent和expected_state。API文档由纯installer生成，可在累计合并时复用，不覆盖其它已发布接口。原状态none/preparing/unknown/prepared/applied/failed/cancelled分别显示；未知提交只读原intent，prepared丢文件必须显式CAS修复；operator不接收写能力。

部署配置为GOEPAY_UPDATE_MANIFEST_FILE、GOEPAY_UPDATE_PACKAGE_FILE、GOEPAY_UPDATE_STAGING_DIR和GOEPAY_UPDATE_PUBLIC_KEY（Ed25519公钥64位hex），四项共同启用。manifest和package只读挂载，staging为私有可写目录；不能使用UNC/device网络路径。配置不通过Web编辑。发行构建用Docker build参数GOEPAY_VERSION、GOEPAY_SOURCE_SHA、GOEPAY_COMMIT链接真实构建；默认development与空source不提供可准备能力。api --build-info和cli build-info在连接DB前返回二进制信息（schema=0表示未查询DB），后台查询才包含实际迁移版本。cli verify-update使用实际数据库完整迁移名称/SHA、当前链接构建、同一部署信任及同一ZIP验证器，只验证临时文件后清理，输出verified=true/prepared=false/applied=false；不执行部署或变更数据库。

本地证据须分别报告：实际Go/PG导出清理源schema和会话后供UI消费；前端原字段/安全文本/确认/CAS取消重试/会话撤销/丢响应不重发；接受、prepared与取消的最后audit跨原会话到期全部回滚；两个并发相同intent只保留一个accepted和一个prepared。根测试、子测试及显式导出skip分别计数。完整累计版本仍需独立审查、组合迁移、镜像及部署验证，本候选不能凭这些分支测试宣称整套项目已完成。

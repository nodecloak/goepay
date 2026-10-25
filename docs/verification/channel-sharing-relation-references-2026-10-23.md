# 渠道接收关系引用专项证据

51 为候选迁移，未部署。48/49 不变，50 财务实现由根任务并行持有。此记录不是50/51全库或完整多应用归属兼容声明。

## 结果

草稿为 owner 本地 AEAD 快照，不注册或独占全局关系。仅 bind claim 才在排序的物理/鉴权锁内注册一次远端发送屏障。全局 registry 用原成功 remote bind operation 作为证明，各 owner/channel 以独立本地 lease 引用。reference 绑定要求原 provider/app/product/账户 scope 及完整解密资料完全一致，返回明确 source/reference_operation_id，不伪称重新调用上游验名。

有其他 lease 时，解绑只本地释放；最后 lease 且无活跃规则/未完成原支付引用才发送一次远端 delete。unknown 或外呼后结果审计失败留下全局屏障，不能换别名/新号重发。已明确失败的 bind 回到 unbound，不再永久占用鉴权身份。支付宝同 App 改声明 Seller 仍由活跃 relation_auth_hash 保护；跨 App 归属未证明时拒绝引用。

51 回填原8种已持久化状态及成功 proof，draft 无 operation 不注册；claimed/unknown 不会变成功。新增数据库 deferred 约束核查 registry、原 remote proof、lease/接收方状态、引用操作及原 actor 审计。不禁用任何触发器。

## 实际验证

- 首条真实红测：两个 owner 同资料草稿，旧版第二个 Create 返回 ErrConflict，3.582s/exit1。
- Windows 全部 `TestChannelSharingRelationReferences*`：25.503s，exit0。涵盖真实微信/支付宝 TLS、跨 owner/channel、12别名并发仅一次 bind/delete、姓名不匹配拒绝引用、失败不占坑、初始 bind 和最终 delete 未知屏障、外呼后丢审计、本地 bind/release 审计回滚、SQL 未审计引用拒绝，以及48/49已有状态升级。
- 既有 receiver 加密、实际TLS、未知、换 MchID、审计故障安全回归：16.188s，exit0。
- 51升级旧状态独立测试：3.328s，exit0。初版 schema reentrant/checksum：4.667s，exit0；后续51变更在每个专项新建 schema 时实际应用。
- `go vet ./internal/payment ./db`：exit0。
- 固定文件检查点 `.tools/checkpoints/51-relation-references-3eb292c9-1acd-4d2a-8ea5-ad994533c24a`，7文件，各自 SHA256 位于 manifest.json。Linux 基础镜像固定为 `sha256:a4aa08fb7b75f385dd8060595b16f91aa03f77efdfe8dd3aa1a95b8794a9047e`（根任务607文件49schema候选）；只读叠加该检查点的51迁移、receiver两文件及3个新测试。
- 该固定组合 `go test -race ./internal/payment -run TestChannelSharingRelationReferences -count=1`：24.014s，随后相同容器 `go vet ./internal/payment ./db`，最终exit0。没有50财务代码混入此 Linux 验证。

## 待根任务集成

`channel_sharing_receiver_test.go` 的两个旧测试仍期待所有 alias 草稿一律冲突，由根任务更新到安全引用语义；同外部号改资料、canonical UID 和类型限制不变。本专项未修改这些原测试。

待独立 review、50/51合并后的全库/race、恢复及本地部署验收。没有访问生产环境、导入凭据或真实支付服务。

## 独立 review P2 修复补证

独立 reviewer 在旧7文件候选发现纯数据库防线缺口：仅把原 claimed/unknown operation 改成 failed 并追加同 actor audit，而不解开 global/receiver 屏障，竟可提交。整合为独立 `channel_sharing_relation_state_guard_test.go` 后，bind/unbind 的 claimed 和 unknown 共4类状态真实红：9.875s/exit1，错误均为 detached remote result 被数据库接受。

51 deferred complete 现双向核查 global state 对应原 remote action/state，以及实际外呼 receiver 的原状态。bound/unbound 核查原终态；若 receiver 后来已有合法本地引用操作，不再误把它强制恢复为旧 remote 操作的状态。新回归同时拒绝“op和global改结果、receiver未改”的半套提交。

- 修复后 Windows 完整 relation references 专项：33.262s，exit0，包含全部旧成功引用、远端 resolve、未知和回填回归。
- 新8文件候选 `.tools/checkpoints/51-relation-references-p2-5e1b3199-7396-42ba-baf7-d876d296e737`，manifest含各文件SHA256；相同固定基础镜像，仅叠加该候选的7个代码/迁移/测试文件。
- 新候选 Linux `go test -race ./internal/payment -run TestChannelSharingRelationReferences -count=1`：32.267s；随后 `go vet ./internal/payment ./db`，最终exit0。

旧7文件候选及其24.014s通过结果**不包含P2修复**。后续API资金授权和50/51/52集成的变化另行验证，不能沿用此专项结果作为全库通过证明。

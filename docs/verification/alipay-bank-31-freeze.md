# 支付宝银行卡31复核冻结记录

仅本地临时证书/TLS假上游和真实PostgreSQL；没有生产或真实交易。本记录对应预计费用0.00审阅修复后的文件与未改动迁移，完整仓库最终交付校验仍由父任务统一执行。

| 文件 | SHA256 |
| --- | --- |
| internal/adapters/payment/alipay_bank_transfer.go | 2678041E01F750D98815F27C5BE7D017EA5D84043C4C50A3280D64D943DF8707 |
| internal/adapters/payment/alipay_bank_transfer_test.go | 0111E080BF0B8A74202CD88892E96EC482683D59B8789FF152C12C5028F1D85B |
| tests/contract/alipay_bank_http_test.go | 64E59C1E003CDACA22306145A0028DA3F3392252B86553F10CD21CB8FBE7A26F |
| db/migrations/000031_alipay_bank_transfer.up.sql | BDEA96AF9E34D1F5940D26D9D5776E4DA6903E35A696E0335C91308D84F16A78 |

修复后 `go test -count=1 ./internal/adapters/payment ./internal/channelconfig ./internal/payment ./internal/reconciliation ./tests/contract -run 'TestAlipayBank|TestWechatBank|TestBank'` 五包通过。`go vet` 覆盖上述包及backoffice/server/jobs共八包通过。独立审阅五包AlipayBank回归也通过，无剩余P1/P2。

预计order_fee允许零且不进实际费用账。缺原提交绑定、缺实际金额、成功后的认证矛盾以及普通FAIL不能伪装退票或付款成功。31仍不使用passback_params恢复旧意图；后续设计已记录，缺证据保留待核对。

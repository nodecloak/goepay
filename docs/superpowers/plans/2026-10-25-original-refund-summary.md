# 双后台退款统计

原Pro两个退款列表都有“退款统计”520px弹窗，读取当前筛选而非当前分页，原六字段为totalCount/totalMoney、successCount/successMoney、pendingCount/pendingMoney。本地静态模块index-CaPsjcRn.js和index-KNIUL4x0.js明确提供这些字段和筛选调用，当前Go列表尚无统计入口。

新增只读GET `/console/v1/{role}/refunds/summary`，复用原退款列表的固定SQL搜索和筛选构造器、独立认证租户谓词。只允许q/merchant_id/type/status/channel_id/from/to等现有筛选，拒绝重复、未知、分页/导出参数、非法路径/日期/规范ID；商户跨租户403。单条聚合保证同一快照，返回原六字段，计数与元金额均精确字符串。处理中包括pending与unknown，已失败记录保留在筛选总额但不计成功或处理中；明确文案，不把待核实当成功退款。超时与并发边界，不调用支付机构，不写审计或账务。

前端用点击时当前筛选快照打开原六指标弹窗，固定接口路径，重试仅GET；分页不影响总计。失败清空旧值，账号/CSRF/路由或筛选上下文变化清理弹窗与迟到响应，Escape/Tab/关闭回焦，桌面520px和375适配。本人和授权管理会话均沿用退款列表的范围，不放宽资金动作。

先真实HTTP RED证明旧路径误返回列表，覆盖双商户的成功/处理中/待核实/失败、日期/方式/搜索/状态、参数注入和无额外账务写入；前端精度/晚响应/错误及双后台桌面/手机浏览器验证。冻结后全前端、完整server及相关合同/race/vet/build，再更新独立模拟预览并实机观察。

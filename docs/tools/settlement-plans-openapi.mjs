export function installSettlementPlanAPI({
  add,
  paths,
  schemas,
  object,
  str,
  id,
  ref,
  content,
}) {
  const decimal = str("规范非负十进制整数字符串。", {
    pattern: "^(0|[1-9][0-9]*)$",
  });
  const positive = str("规范正十进制整数字符串；金额为人民币分。", {
    pattern: "^[1-9][0-9]*$",
  });
  const timestamp = str("RFC3339整秒，必须含时区；响应为UTC。", {
    format: "date-time",
  });
  schemas.SettlementPlanSchedule = object(
    {
      frequency: str(null, { enum: ["second", "minute", "hour", "day", "once"] }),
      interval: str("数字字符串；second 10..3600、minute 1..1440仅balance_threshold支持；hour 1..168、day 1..366、once 0。非按天时分秒必须为0。", {
        pattern: "^(0|[1-9][0-9]*)$",
      }),
      anchor_at: timestamp,
      execute_hour: { type: "integer", minimum: 0, maximum: 23 },
      execute_minute: { type: "integer", minimum: 0, maximum: 59 },
      execute_second: { type: "integer", minimum: 0, maximum: 59 },
    },
    [
      "frequency",
      "interval",
      "anchor_at",
      "execute_hour",
      "execute_minute",
      "execute_second",
    ],
  );
  schemas.SettlementPlanInput = object(
    {
      name: str(null, { minLength: 1, maxLength: 100 }),
      mode: str("明确业务模式；首次签发生成授权后不可变。", { enum: ["generate_only", "balance_threshold"] }),
      schedule: ref("SettlementPlanSchedule"),
      merchant_ids: {
        type: "array",
        minItems: 1,
        maxItems: 100,
        uniqueItems: true,
        items: id,
      },
    },
    ["name", "mode", "schedule", "merchant_ids"],
  );
  schemas.SettlementPlanUpdate = structuredClone(schemas.SettlementPlanInput);
  schemas.SettlementPlanUpdate.properties.expected_revision = positive;
  schemas.SettlementPlanUpdate.required.push("expected_revision");
  schemas.SettlementPlanAuthorize = object(
    {
      expected_revision: positive,
      expected_authorization_id: decimal,
      valid_from: timestamp,
      expires_at: timestamp,
      max_item_gross: positive,
      max_run_gross: positive,
      max_day_gross: positive,
      max_day_items: positive,
    },
    [
      "expected_revision",
      "expected_authorization_id",
      "valid_from",
      "expires_at",
      "max_item_gross",
      "max_run_gross",
      "max_day_gross",
      "max_day_items",
    ],
  );
  schemas.SettlementPlanScan = object({ expected_revision: positive }, [
    "expected_revision",
  ]);
  schemas.SettlementSchedulePreference = object(
    {
      expected_revision: decimal,
      enabled: { type: "boolean" },
      recipient_config_id: decimal,
    },
    ["expected_revision", "enabled", "recipient_config_id"],
  );
  schemas.SettlementPlanView = object(
    {
      id,
      revision: positive,
      name: str(),
      mode: str("明确业务模式；首次签发生成授权后不可变。", { enum: ["generate_only", "balance_threshold"] }),
      schedule: ref("SettlementPlanSchedule"),
      merchant_ids: { type: "array", items: id },
      enabled: { type: "boolean" },
      last_authorization_id: decimal,
      created_at: timestamp,
    },
    [
      "id",
      "revision",
      "name",
      "mode",
      "schedule",
      "merchant_ids",
      "enabled",
      "last_authorization_id",
    ],
  );
  schemas.SettlementPlanRunItem = object(
    {
      id,
      merchant_id: id,
      request_id: decimal,
      state: str(null, { enum: ["created", "skipped", "blocked"] }),
      reason: str("固定原因枚举，不含原收款资料或自由异常文本。"),
      ledger_available: decimal,
      eligible: decimal,
      retained: decimal,
      gross: decimal,
      fee: decimal,
      net: decimal,
      recipient_config_id: decimal,
    },
    [
      "id",
      "merchant_id",
      "request_id",
      "state",
      "reason",
      "ledger_available",
      "eligible",
      "retained",
      "gross",
      "fee",
      "net",
      "recipient_config_id",
    ],
  );
  schemas.SettlementPlanRunView = object(
    {
      id,
      plan_id: id,
      plan_revision: positive,
      authorization_id: id,
      slot_no: decimal,
      business_date: str("UTC+08业务日期。", { format: "date" }),
      scheduled_at: timestamp,
      state: str(null, {
        enum: [
          "pending",
          "running",
          "completed",
          "partial",
          "blocked",
          "revoked",
        ],
      }),
      created_count: decimal,
      skipped_count: decimal,
      blocked_count: decimal,
      error_count: decimal,
      created_at: timestamp,
      items: { type: "array", items: ref("SettlementPlanRunItem") },
    },
    [
      "id",
      "plan_id",
      "plan_revision",
      "authorization_id",
      "slot_no",
      "business_date",
      "scheduled_at",
      "state",
      "created_count",
      "skipped_count",
      "blocked_count",
      "error_count",
      "created_at",
    ],
  );
  function endpoint(role, route, method, summary, options = {}) {
    add(route, method, summary, {
      ...options,
      description: `仅${role === "admin" ? "本人super管理员" : "本人商户"}会话，拒代登录；写操作需CSRF。仅列表接受规范after_id与limit，其他接口禁止query。${options.description || ""}`,
    });
    const before = `/console/v1/{role}${route}`,
      after = `/console/v1/${role}${route}`;
    const op = paths[before][method];
    op.parameters = op.parameters.filter((p) => p.name !== "role");
    op.security = [
      { [role === "admin" ? "AdminSession" : "MerchantSession"]: [] },
    ];
    (paths[after] ??= {})[method] = op;
    delete paths[before][method];
    if (!Object.keys(paths[before]).length) delete paths[before];
    return op;
  }
  const pagination = (op) =>
    op.parameters.push(
      { name: "after_id", in: "query", schema: { ...decimal, default: "0" } },
      {
        name: "limit",
        in: "query",
        schema: { type: "integer", minimum: 1, maximum: 100, default: 20 },
      },
    );
  const planList = endpoint(
    "admin",
    "/settlement-plans",
    "get",
    "游标读取专用结算计划",
    { description: "返回items与next_cursor；计划不是普通cron资料。" },
  );
  pagination(planList);
  endpoint("admin", "/settlement-plans", "post", "创建默认关闭的结算计划定义", {
    body: "SettlementPlanInput",
    created: true,
    description:
      "明确generate_only或balance_threshold模式；scope为1..100个显式商户。非day时分秒必须为0；day按UTC+08锚点日期。只保存定义，不冻结资金或生成申请。balance_threshold读取原auto_settle_money（元）与transfer_alipay；严格大于阈值、UID倒序最多5个、关闭组不补位，整余额超过额度时跳过。组settle_rate覆盖全局比例，仅全局fee_max；不使用fee_min/留存/普通日次数。客户端不能传金额或选择集合；生成与独立自动出款授权分离。禁止auto_payout/threshold/百分比留存等别名字段。",
  });
  endpoint(
    "admin",
    "/settlement-plans/{id}",
    "get",
    "读取原计划定义及当前修订",
    {
      description:
        "包含last_authorization_id与enabled；执行资格仍由原授权快照和当前状态重新核对。",
    },
  );
  endpoint(
    "admin",
    "/settlement-plans/{id}",
    "patch",
    "CAS修改计划定义并停用旧授权",
    {
      body: "SettlementPlanUpdate",
      description:
        "不接受enabled。修改定义后关闭，必须另行授权；历史运行与申请不改写。丢响应先读取原修订，不自动重提。",
    },
  );
  const auth = endpoint(
    "admin",
    "/settlement-plans/{id}/authorizations",
    "post",
    "以强制资金密码确认授权原计划与精确上限",
    {
      body: "SettlementPlanAuthorize",
      created: true,
      description:
        "即使全局enforced=false也必须原动作资金授权。expected_revision与expected_authorization_id双CAS，时间窗/每项每run每日毛额分/每日数量均绑定原body；原scope、政策、参与和收款配置快照变化后旧授权失效，不能用新scope继续跑。资金挑战不保证最终执行资格，事务内重新核实。授权只允许生成待审核申请并冻结，不含审批或出款。",
    },
  );
  auth.parameters.push({
    name: "X-Payment-Authorization",
    in: "header",
    required: true,
    schema: str(null, { writeOnly: true, minLength: 43, maxLength: 43 }),
    description: "始终必需的原计划精确动作短时授权，不能由全局关闭开关绕过。",
  });
  endpoint(
    "admin",
    "/settlement-plan-authorizations/{id}/revoke",
    "post",
    "紧急撤销原授权并停止后续生成",
    {
      body: "Empty",
      description:
        "无需资金grant，但仍需本人super及CSRF。空JSON，不撤回已生成申请，不执行外部资金操作。",
    },
  );
  const runs = endpoint(
    "admin",
    "/settlement-plans/{id}/runs",
    "get",
    "游标读取原计划运行记录",
    {
      description:
        "返回items/next_cursor。completed表示生成扫描完成，不表示审批或付款。金额及数量均字符串。",
    },
  );
  pagination(runs);
  endpoint(
    "admin",
    "/settlement-plans/{id}/runs",
    "post",
    "扫描或恢复当前合法到期时段",
    {
      body: "SettlementPlanScan",
      uncertain: true,
      description:
        "只接受expected_revision，返回202原run；不接受任意日期、slot或新请求号。按UTC+08固定D1资格，仅合法当前due slot，不补造过期业务日申请。运行幂等恢复，未知先查原run。生成申请仍须后续独立审批与出款授权。",
    },
  );
  endpoint(
    "admin",
    "/settlement-plan-runs/{id}",
    "get",
    "读取原运行及逐项生成证据",
    {
      description:
        "条目只含固定原因、ID和分金额，无完整收款姓名账号。无provider调用。",
    },
  );
  endpoint(
    "merchant",
    "/account/settlement-schedule",
    "get",
    "读取本人计划参与与当前脱敏收款配置",
    {
      description:
        "默认关闭。返回merchant_id/revision/enabled/recipient_config_id/recipient，收款配置仅掩码，非普通账户余额。",
    },
  );
  endpoint(
    "merchant",
    "/account/settlement-schedule",
    "put",
    "以参与和收款配置CAS显式选择参与",
    {
      body: "SettlementSchedulePreference",
      description:
        "无merchant_id选择器，enabled必须明确。绑定当前收款配置；改收款配置后重新确认。保存参与不生成申请、不授权自动审批或出款，拒代登录。",
    },
  );
}

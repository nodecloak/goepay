export function installRuntimeParentChannelAPI({
  paths,
  schemas,
  object,
  str,
  id,
  ref,
  content,
}) {
  const bool = { type: "boolean" },
    fp = str("原完整状态指纹。", { pattern: "^[a-f0-9]{64}$" }),
    key = str("原一次性请求号；结果未知先读取同号，禁止换号重发。", {
      pattern: "^[a-f0-9]{32}$",
    }),
    zeroID = str("0取消关联；正数为完整int64字符串。", {
      pattern: "^(0|[1-9][0-9]*)$",
    }),
    family = { type: "string", enum: ["wxpay", "alipay"] };
  const counter = str("规范非负整数字符串。", { pattern: "^(0|[1-9][0-9]*)$" }),
    money = str("人民币元，两位小数的十进制字符串。", {
      pattern: "^(0|[1-9][0-9]*)\\.[0-9]{2}$",
    }),
    retained = str(
      "原商户保留比例0..100%；不是扣费比例，不接受浮点JSON或指数。",
      {
        pattern: "^(?:100(?:\\.0{1,2})?|(?:0|[1-9][0-9]?)(?:\\.[0-9]{1,2})?)$",
      },
    );
  const fieldProperties = {
    name: str(null, { maxLength: 180 }),
    namespace: { type: "string", enum: ["config", "certificates"] },
    secret: bool,
    pem: bool,
  };
  schemas.RuntimeParentField = object(
    fieldProperties,
    Object.keys(fieldProperties),
  );
  schemas.RuntimeParentFieldState = object(
    {
      ...fieldProperties,
      present: bool,
      value: str("仅非秘密字段可出现。PEM字段的内容是PEM而不是服务器路径。", {
        maxLength: 131072,
      }),
    },
    [...Object.keys(fieldProperties), "present"],
  );
  schemas.RuntimeParentFieldState.allOf = [
    {
      if: { properties: { secret: { const: true } }, required: ["secret"] },
      then: { not: { required: ["value"] } },
    },
  ];
  schemas.RuntimeParentChannelAccount = object(
    {
      plugin: family,
      app_types: {
        type: "array",
        maxItems: 16,
        uniqueItems: true,
        items: { type: "string" },
      },
      mode: { type: "integer", const: 0 },
      fields: { type: "array", items: ref("RuntimeParentFieldState") },
      appwxmp: id,
      appwxa: id,
      weixin_binding_state: ref("ParentWeixinBindingState"),
    },
    ["plugin", "app_types", "mode", "fields"],
  );
  const managed = {
    configured: { const: true },
    id,
    source_channel_id: id,
    name: str(null, { minLength: 1, maxLength: 120 }),
    pay_type: family,
    plugin: family,
    revision: id,
    account_version: id,
    enabled: bool,
    archived: bool,
    mode: { type: "integer", const: 0 },
    rate: { anyOf: [retained, { type: "null" }] },
    fee_rate_bps: { type: ["integer", "null"], minimum: 0, maximum: 10000 },
    account: ref("RuntimeParentChannelAccount"),
    created_at: str(null, { format: "date-time" }),
    updated_at: str(null, { format: "date-time" }),
  };
  schemas.RuntimeParentChannelManaged = object(managed, Object.keys(managed));
  schemas.RuntimeParentChannelRegistered = object(
    {
      id,
      name: str(),
      pay_type: family,
      plugin: family,
      configured: { const: false },
    },
    ["id", "name", "pay_type", "plugin", "configured"],
  );
  schemas.RuntimeParentChannel = {
    oneOf: [
      ref("RuntimeParentChannelManaged"),
      ref("RuntimeParentChannelRegistered"),
    ],
  };
  schemas.RuntimeParentValueEdit = {
    oneOf: [
      object({ operation: { type: "string", enum: ["keep", "clear"] } }, [
        "operation",
      ]),
      object(
        {
          operation: { const: "replace" },
          value: str("只写入请求；秘密值不回显。", {
            writeOnly: true,
            maxLength: 131072,
          }),
        },
        ["operation", "value"],
      ),
    ],
  };
  const configNames = [
    "appid",
    "appmchid",
    "costrate",
    "enable_scan_pay",
    "enable_web_oauth",
    "enable_mini_launch",
    "mini_appid",
    "mini_path",
    "mini_program_id",
    "enable_channel_sharing",
    "wechat_api_version",
    "appkey",
    "oauth_appsecret",
    "mini_secret",
    "enable_bank_transfer",
    "bank_public_key",
    "h5_site_url",
    "h5_site_name",
    "v3_api_key",
    "v3_merchant_serial",
    "v3_encryption_serial",
    "appsecret",
    "sign_type",
    "bank_transfer_scene",
    "bank_transfer_scene_reports",
    "preauth_voucher_product_code",
    "enable_channel_sharing_return",
    "sharing_return_evidence",
  ];
  const config = object(
    Object.fromEntries(
      configNames.map((k) => [k, ref("RuntimeParentValueEdit")]),
    ),
  );
  const certificates = object(
    Object.fromEntries(
      [
        "sslcert_path",
        "sslkey_path",
        "publickey_path",
        "v3_merchant_key",
        "app_cert_path",
        "alipay_cert_path",
        "root_cert_path",
      ].map((k) => [k, ref("RuntimeParentValueEdit")]),
    ),
  );
  certificates.patternProperties = {
    "^v3_verifier_[A-Za-z0-9_-]{1,128}$": ref("RuntimeParentValueEdit"),
  };
  schemas.RuntimeParentChannelAccountChange = object({
    app_types: {
      type: "array",
      minItems: 1,
      maxItems: 16,
      uniqueItems: true,
      items: { type: "string" },
    },
    config,
    certificates,
    appwxmp: zeroID,
    appwxa: zeroID,
    expected_bindings_fingerprint: fp,
  });
  const create = {
    source_channel_id: id,
    expected_source_fingerprint: fp,
    request_id: key,
    name: str(null, { minLength: 1, maxLength: 120 }),
    rate: retained,
    mode: { type: "integer", const: 0 },
    account: ref("RuntimeParentChannelAccountChange"),
  };
  schemas.RuntimeParentChannelCreate = {
    oneOf: [
      object(create, Object.keys(create)),
      object(
        { ...create, copy_from_channel_id: id, expected_copy_fingerprint: fp },
        [
          ...Object.keys(create),
          "copy_from_channel_id",
          "expected_copy_fingerprint",
        ],
      ),
    ],
  };
  schemas.RuntimeParentChannelPatch = object(
    {
      expected_fingerprint: fp,
      name: str(null, { minLength: 1, maxLength: 120 }),
      rate: retained,
      enabled: bool,
      archived: { const: true },
      account: ref("RuntimeParentChannelAccountChange"),
    },
    ["expected_fingerprint"],
  );
  schemas.RuntimeParentChannelPatch.minProperties = 2;
  schemas.RuntimeParentChannelBatch = object(
    {
      action: { type: "string", enum: ["open", "close", "delete"] },
      items: {
        type: "array",
        minItems: 1,
        maxItems: 100,
        items: object({ id, expected_fingerprint: fp }, [
          "id",
          "expected_fingerprint",
        ]),
      },
    },
    ["action", "items"],
  );
  schemas.RuntimeParentChannelTemplate = object(
    {
      source_channel_id: id,
      name: str(),
      pay_type: family,
      fields: { type: "array", items: ref("RuntimeParentField") },
      fingerprint: fp,
      merchant_count: { type: "integer", minimum: 0, maximum: 1000 },
    },
    [
      "source_channel_id",
      "name",
      "pay_type",
      "fields",
      "fingerprint",
      "merchant_count",
    ],
  );
  schemas.RuntimeParentStatisticsDay = object(
    {
      day: str(null, { format: "date" }),
      realmoney: money,
      refundmoney: money,
      profitmoney: { type: "null" },
      orderCount: counter,
      successCount: counter,
      successRate: { type: ["string", "null"] },
    },
    [
      "day",
      "realmoney",
      "refundmoney",
      "profitmoney",
      "orderCount",
      "successCount",
      "successRate",
    ],
  );
  schemas.RuntimeParentPaymentTest = object(
    {
      request_id: key,
      channel_id: id,
      merchant_id: id,
      out_trade_no: str(null, { pattern: "^PT[a-f0-9]{32}$" }),
      trade_no: str(),
      name: str(),
      money,
      state: {
        type: "string",
        enum: [
          "blocked",
          "prepared",
          "processing",
          "unknown",
          "pending",
          "succeeded",
          "failed",
          "paid",
          "closed",
        ],
      },
      checkout_url: str(
        "空串或当前原订单的内部签名/checkout/链接；不返回API密钥或原SDK材料。",
      ),
      material_type: {
        type: "string",
        enum: ["", "qrcode", "html", "app", "jsapi", "preauth", "jump"],
      },
      can_query: bool,
    },
    [
      "request_id",
      "channel_id",
      "merchant_id",
      "out_trade_no",
      "trade_no",
      "name",
      "money",
      "state",
      "checkout_url",
      "material_type",
      "can_query",
    ],
  );
  const security = [{ AdminSession: [] }],
    csrf = {
      name: "X-CSRF-Token",
      in: "header",
      required: true,
      schema: str(null, { pattern: "^[A-Za-z0-9_-]{43}$" }),
    },
    parameter = { name: "id", in: "path", required: true, schema: id },
    requestParameter = {
      name: "request_id",
      in: "path",
      required: true,
      schema: key,
    };
  const envelope = (schema) =>
    content(
      object(
        { code: { type: "integer", const: 0 }, message: str(), data: schema },
        ["code", "message", "data"],
      ),
    );
  const errors = {
    400: {
      description: "未知/重复/错误类型字段，非规范ID，额外查询参数或body。",
    },
    401: { description: "未登录或原会话失效。" },
    403: {
      description:
        "仅本人持久super管理员；拒代登录/operator，写操作要求同原CSRF；末写后再验原会话。",
    },
    404: { description: "原通道或本人的原请求不存在。" },
    409: {
      description:
        "原指纹变更，独立事实/审计丢失或不可确认；禁止猜测当前账户或重发。",
    },
    422: {
      description: "参数或当前可信商户/账户/产品/费率不可用；仅收款mode0。",
    },
    503: { description: "本地持久配置/凭据/运行门无法确认。" },
  };
  const op = (operationId, summary, data, options = {}) => ({
    operationId,
    summary,
    security,
    tags: ["parent-channels"],
    ...options,
    responses: {
      200: { description: "当前原目标，无秘密值。", content: envelope(data) },
      ...errors,
    },
  });
  const base = "/console/v1/admin/payment-channels";
  paths[base] = {
    get: op(
      "runtimeParentChannels",
      "分页读取真实父通道目录及原字段",
      object(
        {
          items: {
            type: "array",
            maxItems: 100,
            items: ref("RuntimeParentChannel"),
          },
          total: { type: "integer", minimum: 0 },
          page: { type: "integer", minimum: 1 },
          size: { type: "integer", minimum: 1, maximum: 100 },
        },
        ["items", "total", "page", "size"],
      ),
      {
        description:
          "与通用channels资料ID分开；GET不创建账户版本。未知历史费用为null。costrate仅估算，未形成真实利润；父账户版本不重写96/99历史。",
        parameters: [
          { name: "kw", in: "query", schema: str() },
          {
            name: "type",
            in: "query",
            schema: { type: "string", enum: ["", "alipay", "wxpay"] },
          },
          {
            name: "plugin",
            in: "query",
            schema: { type: "string", enum: ["", "alipay", "wxpay"] },
          },
          {
            name: "dstatus",
            in: "query",
            schema: { type: "string", enum: ["-1", "0", "1"] },
          },
          {
            name: "page",
            in: "query",
            schema: {
              type: "integer",
              minimum: 1,
              maximum: 1000000,
              default: 1,
            },
          },
          {
            name: "size",
            in: "query",
            schema: { type: "integer", minimum: 1, maximum: 100, default: 20 },
          },
        ],
      },
    ),
    post: op(
      "runtimeParentChannelCreate",
      "新增或复制真实账户，默认关闭",
      ref("RuntimeParentChannelManaged"),
      {
        parameters: [csrf],
        description:
          "仅可信原source及当前operator范围。新建不继承秘密，必须完整提供所需凭据；复制服务端解密原版本并重新AEAD绑定新ID，权限不扩出原copy grant与当前operator交集。原request_id防失联新建，未知先GET /requests/{request_id}。",
        requestBody: {
          required: true,
          content: content(ref("RuntimeParentChannelCreate")),
        },
      },
    ),
  };
  paths[base + "/templates"] = {
    get: op(
      "runtimeParentChannelTemplates",
      "读取可信原授权来源及闭字段目录",
      object(
        {
          items: { type: "array", items: ref("RuntimeParentChannelTemplate") },
          total: { type: "integer", minimum: 0 },
        },
        ["items", "total"],
      ),
    ),
  };
  paths[base + "/{id}"] = {
    get: op(
      "runtimeParentChannelRead",
      "读取原父通道配置",
      ref("RuntimeParentChannel"),
      { parameters: [parameter] },
    ),
    patch: op(
      "runtimeParentChannelChange",
      "CAS更新原账户/费率/启停/归档",
      ref("RuntimeParentChannelManaged"),
      {
        parameters: [parameter, csrf],
        description:
          "秘密必须keep/replace/clear；keep不发值。每次账户修改追加不可变版本，旧Query/Refund/callback仍恢复原版本。归档不删除秘密历史，不重发已有支付。无法证明pre180旧秘密时unknown禁止I/O。",
        requestBody: {
          required: true,
          content: content(ref("RuntimeParentChannelPatch")),
        },
      },
    ),
  };
  paths[base + "/batch"] = {
    post: op(
      "runtimeParentChannelBatch",
      "原通道启停及删除归档原子批量处理",
      object(
        {
          action: { type: "string", enum: ["open", "close", "delete"] },
          items: { type: "array", items: ref("RuntimeParentChannelManaged") },
          total: { type: "integer" },
        },
        ["action", "items", "total"],
      ),
      {
        parameters: [csrf],
        description:
          "最多100个唯一原ID；排序锁、全部CAS及末写会话复核。任一冲突全批回滚。",
        requestBody: {
          required: true,
          content: content(ref("RuntimeParentChannelBatch")),
        },
      },
    ),
  };
  paths[base + "/{id}/adopt"] = {
    post: op(
      "runtimeParentChannelAdopt",
      "显式载入本人原注册账户供配置",
      ref("RuntimeParentChannelManaged"),
      {
        parameters: [parameter, csrf],
        description:
          "不伪造历史支付源、费率或原秘密。restore-only过期原来源不能采用为可执行账户。",
        requestBody: {
          required: true,
          content: content(
            object({ expected_source_fingerprint: fp }, [
              "expected_source_fingerprint",
            ]),
          ),
        },
      },
    ),
  };
  paths[base + "/requests/{request_id}"] = {
    get: op(
      "runtimeParentChannelOriginalRequest",
      "只读核对本人原创建请求对应的通道",
      ref("RuntimeParentChannelManaged"),
      {
        parameters: [requestParameter],
        description: "等待原catalog事务后读取；无创建、版本写入或支付I/O。",
      },
    ),
  };
  paths[base + "/{id}/statistics"] = {
    get: op(
      "runtimeParentChannelStatistics",
      "读取原父通道近七日统计",
      object(
        {
          channel_id: id,
          items: {
            type: "array",
            minItems: 7,
            maxItems: 7,
            items: ref("RuntimeParentStatisticsDay"),
          },
          cost_basis: { const: "unknown" },
          money_basis: str(),
        },
        ["channel_id", "items", "cost_basis", "money_basis"],
      ),
      {
        parameters: [parameter],
        description:
          "仅实际父通道FK与已认证收款/退款记录；成本与真实利润未知，利润为null，不冒充账实利润。",
      },
    ),
  };
  paths[base + "/{id}/tests"] = {
    post: op(
      "runtimeParentPaymentTestCreate",
      "原测试支付：固定收款商户，经普通支付编排创建受保护订单",
      ref("RuntimeParentPaymentTest"),
      {
        parameters: [parameter, csrf],
        description:
          "test_pay_uid从支付设置固定选择，0/缺失关闭测试。拒客户端商户、回调、method、上游URL。原风控/费用/账户/99限制及末写会话门不绕过；内部HMAC用途marker使普通Submit不能绕过原测试会话。首次调用原providerCreate，重放同request只读原号；未提供真实支付凭据时不伪造通道测试成功。",
        requestBody: {
          required: true,
          content: content(
            object(
              {
                request_id: key,
                name: str(null, { minLength: 1, maxLength: 256 }),
                money: str("正数元金额，不接受JSON数字或指数。", {
                  pattern: "^(0|[1-9][0-9]*)(?:\\.[0-9]{1,2})?$",
                }),
                expected_fingerprint: fp,
              },
              ["request_id", "name", "money", "expected_fingerprint"],
            ),
          ),
        },
      },
    ),
  };
  paths[base + "/{id}/tests/{request_id}"] = {
    get: op(
      "runtimeParentPaymentTestRead",
      "读取本人原测试请求及原订单，不重发也不自动查上游",
      ref("RuntimeParentPaymentTest"),
      { parameters: [parameter, requestParameter] },
    ),
  };
  paths[base + "/{id}/tests/{request_id}/query"] = {
    post: op(
      "runtimeParentPaymentTestQuery",
      "显式查询本人原测试订单，使用原账户版本",
      ref("RuntimeParentPaymentTest"),
      {
        parameters: [parameter, requestParameter, csrf],
        description:
          "仅Query/核对原号。结果未知不创建新的支付请求，不跟随任意URL或探测上游。所有验证仅应在自有合成本地TLS环境执行；本实现测试没有真实交易。",
        requestBody: { required: true, content: content(object({})) },
      },
    ),
  };
  schemas.RuntimeParentSubject = object(
    {
      channel_id: id,
      ordername: str(null, { maxLength: 1024 }),
      ordername_rules: str(null, { maxLength: 20000 }),
      fingerprint: fp,
    },
    ["channel_id", "ordername", "ordername_rules", "fingerprint"],
  );
  paths[base + "/{id}/subject"] = {
    get: op(
      "runtimeParentSubjectRead",
      "读取原父通道订单名称高级设置",
      ref("RuntimeParentSubject"),
      {
        parameters: [parameter],
        description:
          "仅当前通道作用域；独立空值继承，[]清空金额规则。复用现有订单名称策略与首次发送快照。",
      },
    ),
    patch: op(
      "runtimeParentSubjectUpdate",
      "保存原ordername与ordername_rules字段",
      ref("RuntimeParentSubject"),
      {
        parameters: [parameter, csrf],
        description:
          "仅真实受管理父ID；原超级会话末写复核、完整原字段指纹CAS、审计与事务回滚。六变量与首次随机名称不追溯旧订单。",
        requestBody: {
          required: true,
          content: content(
            object(
              {
                ordername: str(null, { maxLength: 1024 }),
                ordername_rules: str(null, { maxLength: 20000 }),
                expected_fingerprint: fp,
              },
              ["ordername", "ordername_rules", "expected_fingerprint"],
            ),
          ),
        },
      },
    ),
  };
}

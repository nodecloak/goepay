// Pro-style channel sharing records: filtered offset list, row view and statistics.
export function channelSharingRecordSchemas() {
 return {
  ChannelSharingOrderListView: {
   "type": "object",
   "description": "Pro 分账记录 列表行：基础订单摘要加上来自原支付声明的 订单金额/支付方式/分账规则。",
   "properties": {
    "id": {
     "type": "string",
     "pattern": "^[1-9][0-9]*$"
    },
    "external_no": {
     "type": "string",
     "pattern": "^[A-Za-z0-9_-]{1,128}$"
    },
    "out_no": {
     "type": "string"
    },
    "state": {
     "type": "string"
    },
    "share_amount": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$",
     "description": "分账金额（人民币分，各接收方金额之和）。"
    },
    "order_amount": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$",
     "description": "订单金额（人民币分，原支付声明毛额）。"
    },
    "provider": {
     "type": "string",
     "description": "支付方式（通道 provider，如 wxpay/alipay）。"
    },
    "method": {
     "type": "string"
    },
    "rule_id": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$",
     "description": "分账规则 id；原支付未声明规则时为 0。"
    },
    "channel_id": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$"
    },
    "created_at": {
     "type": "string",
     "format": "date-time"
    }
   },
   "required": [
    "id",
    "external_no",
    "out_no",
    "state",
    "share_amount",
    "order_amount",
    "provider",
    "method",
    "rule_id",
    "channel_id",
    "created_at"
   ],
   "additionalProperties": false
  },
  ChannelSharingOrderStats: {
   "type": "object",
   "description": "Pro 分账记录 统计聚合（与列表相同过滤）。",
   "properties": {
    "count": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$"
    },
    "share_amount": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$"
    },
    "released_amount": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$"
    },
    "success_count": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$"
    },
    "fail_count": {
     "type": "string",
     "pattern": "^(0|[1-9][0-9]*)$"
    }
   },
   "required": [
    "count",
    "share_amount",
    "released_amount",
    "success_count",
    "fail_count"
   ],
   "additionalProperties": false
  },
 };
}
export function installChannelSharingRecordsAPI({paths}) {
 const op = paths['/console/v1/{role}/channel-sharing/orders'].get;
 op.summary = "读取所属商户渠道分账orders列表（Pro 分账记录筛选）";
 op.description = "管理员必须显式提供merchant_id；商户由当前会话决定身份，禁止提供merchant_id字段。Pro 分账记录筛选：field（external_no系统订单号/out_no接口订单号/share_amount分账金额）+keyword搜索内容，status分账状态（unsent待分账/submitted已提交/succeeded分账成功/failed分账失败/cancelled已取消），from/to日期（YYYY-MM-DD，北京时间，含当日）。offset规范非负整数默认0，limit规范整数1..100默认20。返回items（含订单金额order_amount、支付方式provider、分账规则rule_id）、total与limit。format=csv时返回带Pro表头的有界CSV下载。只读本地，不自动向上游查询。";
 op.parameters = [
  {
   "name": "role",
   "in": "path",
   "required": true,
   "schema": {
    "type": "string",
    "enum": [
     "admin",
     "merchant"
    ]
   }
  },
  {
   "name": "merchant_id",
   "in": "query",
   "schema": {
    "type": "string",
    "description": "十进制 ID 字符串。",
    "pattern": "^[1-9][0-9]*$"
   },
   "description": "仅管理员必填；商户不能提供。"
  },
  {
   "name": "field",
   "in": "query",
   "schema": {
    "type": "string",
    "enum": [
     "external_no",
     "out_no",
     "share_amount"
    ]
   },
   "description": "搜索字段。share_amount按元精确匹配分账金额。"
  },
  {
   "name": "keyword",
   "in": "query",
   "schema": {
    "type": "string",
    "maxLength": 128
   },
   "description": "搜索内容，配合 field。"
  },
  {
   "name": "status",
   "in": "query",
   "schema": {
    "type": "string",
    "enum": [
     "unsent",
     "submitted",
     "succeeded",
     "failed",
     "cancelled"
    ]
   },
   "description": "分账状态过滤。"
  },
  {
   "name": "rule_id",
   "in": "query",
   "schema": {
    "type": "string",
    "pattern": "^[1-9][0-9]*$"
   },
   "description": "分账规则ID过滤。"
  },
  {
   "name": "from",
   "in": "query",
   "schema": {
    "type": "string",
    "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"
   },
   "description": "起始日期（北京时间，含当日）。"
  },
  {
   "name": "to",
   "in": "query",
   "schema": {
    "type": "string",
    "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"
   },
   "description": "结束日期（北京时间，含当日）。"
  },
  {
   "name": "offset",
   "in": "query",
   "schema": {
    "type": "string",
    "pattern": "^(0|[1-9][0-9]*)$"
   }
  },
  {
   "name": "limit",
   "in": "query",
   "schema": {
    "type": "integer",
    "minimum": 1,
    "maximum": 100,
    "default": 20
   }
  },
  {
   "name": "format",
   "in": "query",
   "schema": {
    "type": "string",
    "enum": [
     "csv"
    ]
   },
   "description": "format=csv 返回有界CSV（最多50000行或8MiB）。"
  }
 ];
 op.responses['200'].content = {
  "application/json": {
   "schema": {
    "type": "object",
    "properties": {
     "code": {
      "type": "integer"
     },
     "message": {
      "type": "string"
     },
     "data": {
      "type": "object",
      "properties": {
       "items": {
        "type": "array",
        "items": {
         "$ref": "#/components/schemas/ChannelSharingOrderListView"
        }
       },
       "total": {
        "type": "string",
        "pattern": "^(0|[1-9][0-9]*)$",
        "description": "过滤后的总条数（offset 分页）。"
       },
       "limit": {
        "type": "integer"
       }
      },
      "required": [
       "items",
       "total",
       "limit"
      ],
      "additionalProperties": false
     }
    },
    "required": [
     "code",
     "message",
     "data"
    ],
    "additionalProperties": false
   }
  }
 };
 paths["/console/v1/{role}/channel-sharing/orders/statistics"] = {
  "get": {
   "operationId": "console_sharing_orders_statistics_get",
   "summary": "渠道分账记录统计聚合（Pro 统计）",
   "description": "按与列表相同的过滤（field/keyword/status/from/to、管理员 merchant_id）返回有界统计：count、share_amount、released_amount、success_count、fail_count。不分页。",
   "parameters": [
    {
     "name": "role",
     "in": "path",
     "required": true,
     "schema": {
      "type": "string",
      "enum": [
       "admin",
       "merchant"
      ]
     }
    },
    {
     "name": "merchant_id",
     "in": "query",
     "schema": {
      "type": "string",
      "pattern": "^[1-9][0-9]*$"
     },
     "description": "仅管理员必填；商户不能提供。"
    },
    {
     "name": "field",
     "in": "query",
     "schema": {
      "type": "string",
      "enum": [
       "external_no",
       "out_no",
       "share_amount"
      ]
     }
    },
    {
     "name": "keyword",
     "in": "query",
     "schema": {
      "type": "string",
      "maxLength": 128
     }
    },
    {
     "name": "status",
     "in": "query",
     "schema": {
      "type": "string",
      "enum": [
       "unsent",
       "submitted",
       "succeeded",
       "failed",
       "cancelled"
      ]
     }
    },
    {
     "name": "rule_id",
     "in": "query",
     "schema": {
      "type": "string",
      "pattern": "^[1-9][0-9]*$"
     }
    },
    {
     "name": "from",
     "in": "query",
     "schema": {
      "type": "string",
      "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"
     }
    },
    {
     "name": "to",
     "in": "query",
     "schema": {
      "type": "string",
      "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"
     }
    }
   ],
   "responses": {
    "200": {
     "description": "统计已返回。",
     "content": {
      "application/json": {
       "schema": {
        "type": "object",
        "properties": {
         "code": {
          "type": "integer"
         },
         "message": {
          "type": "string"
         },
         "data": {
          "$ref": "#/components/schemas/ChannelSharingOrderStats"
         }
        },
        "required": [
         "code",
         "message",
         "data"
        ],
        "additionalProperties": false
       }
      }
     }
    },
    "400": {
     "description": "参数错误。",
     "content": {
      "application/json": {
       "schema": {
        "$ref": "#/components/schemas/Envelope"
       }
      }
     }
    },
    "401": {
     "description": "未认证。",
     "content": {
      "application/json": {
       "schema": {
        "$ref": "#/components/schemas/Envelope"
       }
      }
     }
    },
    "403": {
     "description": "权限拒绝。",
     "content": {
      "application/json": {
       "schema": {
        "$ref": "#/components/schemas/Envelope"
       }
      }
     }
    }
   }
  }
 };
}

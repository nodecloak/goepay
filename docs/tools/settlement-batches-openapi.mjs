// Preserve the implemented batch contract in every full regeneration.
export function installSettlementBatchesAPI({paths}) {
  const definitions = {
  "/console/v1/admin/settlement-batches": {
    "get": {
      "operationId": "settlementBatches84List",
      "tags": [
        "settlement-batches"
      ],
      "summary": "本人super/operator读取独立原结算批次",
      "security": [
        {
          "AdminSession": []
        }
      ],
      "responses": {
        "200": {
          "description": "已核验的原批次字段：id、batch、external_no、allmoney（元字符串净额）、count、time、status、all_paid、items；分页含total、limit、next_before_id。items的gross_amount_cents/fee_amount_cents/net_amount_cents均为分字符串。"
        },
        "400": {
          "description": "参数、JSON字段或查询不明确"
        },
        "401": {
          "description": "会话失效"
        },
        "403": {
          "description": "非本人管理员、错误角色或缺少CSRF"
        },
        "404": {
          "description": "批次不存在"
        },
        "409": {
          "description": "原来源或审计冲突、重复归组或尚未确认全部付款"
        }
      },
      "parameters": [
        {
          "name": "external_no",
          "in": "query",
          "schema": {
            "type": "string",
            "maxLength": 128
          }
        },
        {
          "name": "before_id",
          "in": "query",
          "schema": {
            "type": "string",
            "pattern": "^(0|[1-9][0-9]*)$",
            "default": "0"
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
          "name": "batch",
          "in": "query",
          "schema": {
            "type": "string",
            "pattern": "^[0-9]{20,27}$"
          }
        },
        {
          "name": "status",
          "in": "query",
          "schema": {
            "type": "string",
            "enum": [
              "active",
              "archived",
              "completed"
            ]
          }
        }
      ]
    },
    "post": {
      "operationId": "settlementBatches84Create",
      "tags": [
        "settlement-batches"
      ],
      "summary": "原结算申请按净额归组，幂等、不审批、不再次预留或付款",
      "security": [
        {
          "AdminSession": []
        }
      ],
      "responses": {
        "200": {
          "description": "已核验的原批次字段：id、batch、external_no、allmoney（元字符串净额）、count、time、status、all_paid、items；分页含total、limit、next_before_id。items的gross_amount_cents/fee_amount_cents/net_amount_cents均为分字符串。"
        },
        "400": {
          "description": "参数、JSON字段或查询不明确"
        },
        "401": {
          "description": "会话失效"
        },
        "403": {
          "description": "非本人管理员、错误角色或缺少CSRF"
        },
        "404": {
          "description": "批次不存在"
        },
        "409": {
          "description": "原来源或审计冲突、重复归组或尚未确认全部付款"
        }
      },
      "parameters": [
        {
          "name": "X-CSRF-Token",
          "in": "header",
          "required": true,
          "schema": {
            "type": "string"
          }
        }
      ],
      "requestBody": {
        "required": true,
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "additionalProperties": false,
              "properties": {
                "external_no": {
                  "type": "string",
                  "minLength": 1,
                  "maxLength": 128
                },
                "request_ids": {
                  "type": "array",
                  "minItems": 1,
                  "maxItems": 100,
                  "uniqueItems": true,
                  "items": {
                    "type": "string",
                    "pattern": "^[1-9][0-9]*$"
                  }
                }
              },
              "required": [
                "external_no",
                "request_ids"
              ]
            }
          }
        }
      }
    }
  },
  "/console/v1/admin/settlement-batches/{id}": {
    "get": {
      "operationId": "settlementBatches84Detail",
      "tags": [
        "settlement-batches"
      ],
      "summary": "核验原资金、收款快照及独立审计后读取历史批次",
      "security": [
        {
          "AdminSession": []
        }
      ],
      "responses": {
        "200": {
          "description": "已核验的原批次字段：id、batch、external_no、allmoney（元字符串净额）、count、time、status、all_paid、items；分页含total、limit、next_before_id。items的gross_amount_cents/fee_amount_cents/net_amount_cents均为分字符串。"
        },
        "400": {
          "description": "参数、JSON字段或查询不明确"
        },
        "401": {
          "description": "会话失效"
        },
        "403": {
          "description": "非本人管理员、错误角色或缺少CSRF"
        },
        "404": {
          "description": "批次不存在"
        },
        "409": {
          "description": "原来源或审计冲突、重复归组或尚未确认全部付款"
        }
      },
      "parameters": [
        {
          "name": "id",
          "in": "path",
          "required": true,
          "schema": {
            "type": "string",
            "pattern": "^[1-9][0-9]*$"
          }
        }
      ]
    }
  },
  "/console/v1/admin/settlement-batches/{id}/archive": {
    "post": {
      "operationId": "settlementBatches84archive",
      "tags": [
        "settlement-batches"
      ],
      "summary": "归档批次并保留所有原申请及出款记录",
      "security": [
        {
          "AdminSession": []
        }
      ],
      "responses": {
        "200": {
          "description": "已核验的原批次字段：id、batch、external_no、allmoney（元字符串净额）、count、time、status、all_paid、items；分页含total、limit、next_before_id。items的gross_amount_cents/fee_amount_cents/net_amount_cents均为分字符串。"
        },
        "400": {
          "description": "参数、JSON字段或查询不明确"
        },
        "401": {
          "description": "会话失效"
        },
        "403": {
          "description": "非本人管理员、错误角色或缺少CSRF"
        },
        "404": {
          "description": "批次不存在"
        },
        "409": {
          "description": "原来源或审计冲突、重复归组或尚未确认全部付款"
        }
      },
      "parameters": [
        {
          "name": "id",
          "in": "path",
          "required": true,
          "schema": {
            "type": "string",
            "pattern": "^[1-9][0-9]*$"
          }
        },
        {
          "name": "X-CSRF-Token",
          "in": "header",
          "required": true,
          "schema": {
            "type": "string"
          }
        }
      ],
      "requestBody": {
        "required": true,
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "additionalProperties": false,
              "properties": {},
              "required": []
            }
          }
        }
      }
    }
  },
  "/console/v1/admin/settlement-batches/{id}/complete": {
    "post": {
      "operationId": "settlementBatches84complete",
      "tags": [
        "settlement-batches"
      ],
      "summary": "仅全部原出款已可信确认付款后完成批次",
      "security": [
        {
          "AdminSession": []
        }
      ],
      "responses": {
        "200": {
          "description": "已核验的原批次字段：id、batch、external_no、allmoney（元字符串净额）、count、time、status、all_paid、items；分页含total、limit、next_before_id。items的gross_amount_cents/fee_amount_cents/net_amount_cents均为分字符串。"
        },
        "400": {
          "description": "参数、JSON字段或查询不明确"
        },
        "401": {
          "description": "会话失效"
        },
        "403": {
          "description": "非本人管理员、错误角色或缺少CSRF"
        },
        "404": {
          "description": "批次不存在"
        },
        "409": {
          "description": "原来源或审计冲突、重复归组或尚未确认全部付款"
        }
      },
      "parameters": [
        {
          "name": "id",
          "in": "path",
          "required": true,
          "schema": {
            "type": "string",
            "pattern": "^[1-9][0-9]*$"
          }
        },
        {
          "name": "X-CSRF-Token",
          "in": "header",
          "required": true,
          "schema": {
            "type": "string"
          }
        }
      ],
      "requestBody": {
        "required": true,
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "additionalProperties": false,
              "properties": {},
              "required": []
            }
          }
        }
      }
    }
  },
  "/console/v1/admin/settlement-batches/{id}/export": {
    "get": {
      "operationId": "settlementBatches84Export",
      "tags": [
        "settlement-batches"
      ],
      "summary": "仅本人super导出原冻结收款快照及净额；审计不记录收款人",
      "security": [
        {
          "AdminSession": []
        }
      ],
      "responses": {
        "200": {
          "description": "pay_<format>_<batch>.csv；Cache-Control:no-store；Content-Disposition:attachment；严格text/csv;charset=utf-8或gbk",
          "content": {
            "text/csv": {
              "schema": {
                "type": "string",
                "format": "binary"
              }
            }
          }
        },
        "400": {
          "description": "参数、JSON字段或查询不明确"
        },
        "401": {
          "description": "会话失效"
        },
        "403": {
          "description": "非本人管理员、错误角色或缺少CSRF"
        },
        "404": {
          "description": "批次不存在"
        },
        "409": {
          "description": "原来源或审计冲突、重复归组或尚未确认全部付款"
        }
      },
      "parameters": [
        {
          "name": "id",
          "in": "path",
          "required": true,
          "schema": {
            "type": "string",
            "pattern": "^[1-9][0-9]*$"
          }
        },
        {
          "name": "format",
          "in": "query",
          "required": true,
          "schema": {
            "type": "string",
            "enum": [
              "common",
              "mybank",
              "alipay",
              "wxpay"
            ]
          }
        }
      ],
      "description": "common、wxpay使用UTF-8 BOM，alipay、mybank使用GBK无BOM，均CRLF。微信按原AppID筛选且拒绝跨App混合；缺原收款快照、危险表格公式或不可编码字符直接失败，不改写收款人。仅返回附件，不创建付款。"
    }
  }
};
  for (const [route, methods] of Object.entries(definitions)) { if (paths[route]) throw Error("Duplicate settlement batch route"); paths[route] = methods; }
}

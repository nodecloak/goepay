// Transaction complaints: original records, statistics, channel sync tasks and provider replies.
export function installComplaintsAPI({paths}) {
 paths["/console/v1/{role}/complaints133"] = {
  "get": {
   "operationId": "complaints133_list_get",
   "summary": "交易投诉列表（provider 同步，complaints133）",
   "description": "列出 provider 同步到 complaints133 的投诉（非本地工单资源）。merchant 仅见本商户 owner 的投诉；admin 全量。受 complain_open 门控。filter: field(关联订单 trade_no/第三方单号 third_id/问题类型 category/原因 title/详情 content/电话 phone/渠道子商户号 third_mch_id/商户号 merchant_id)+q, status(open|processing|done), pay_type(wxpay|alipay)。offset 分页 page/limit，返回 items+total。",
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
     "name": "field",
     "in": "query",
     "schema": {
      "type": "string"
     }
    },
    {
     "name": "q",
     "in": "query",
     "schema": {
      "type": "string"
     }
    },
    {
     "name": "status",
     "in": "query",
     "schema": {
      "type": "string",
      "enum": [
       "open",
       "processing",
       "done"
      ]
     }
    },
    {
     "name": "pay_type",
     "in": "query",
     "schema": {
      "type": "string",
      "enum": [
       "wxpay",
       "alipay"
      ]
     }
    },
    {
     "name": "page",
     "in": "query",
     "schema": {
      "type": "integer",
      "minimum": 1
     }
    },
    {
     "name": "limit",
     "in": "query",
     "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100
     }
    }
   ],
   "responses": {
    "200": {
     "description": "items[] + total；金额为分字符串或空（未知），状态为 open|processing|done"
    },
    "403": {
     "description": "complain_open 关闭"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints133/{id}"] = {
  "get": {
   "operationId": "complaints133_detail_get",
   "summary": "交易投诉详情（含图片与协商历史）",
   "description": "一条 complaints133 投诉的详情，含从已验签 provider 明文体中提取的 images 与 negotiation 历史。merchant 仅限本商户 owner。受 complain_open 门控。",
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
     "name": "id",
     "in": "path",
     "required": true,
     "schema": {
      "type": "integer"
     }
    }
   ],
   "responses": {
    "200": {
     "description": "投诉详情 + images[] + negotiation[]"
    },
    "403": {
     "description": "complain_open 关闭"
    },
    "404": {
     "description": "投诉不存在或越权"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints-summary"] = {
  "get": {
   "operationId": "complaints_summary_get",
   "summary": "交易投诉 今日/昨日 汇总",
   "description": "complaints133 的今日/昨日投诉总数及支付宝/微信分项（按 first_seen_at，UTC+8 业务日）。merchant 按本商户 owner 统计。受 complain_open 门控。",
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
    }
   ],
   "responses": {
    "200": {
     "description": "summary{today_count,today_alipay,today_wxpay,yesterday_*}"
    },
    "403": {
     "description": "complain_open 关闭"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints-charts"] = {
  "get": {
   "operationId": "complaints_charts_get",
   "summary": "投诉统计（汇总 + 商户排行榜）",
   "description": "complaints133 的 今日/昨日 汇总与按商户的投诉排行榜（今日降序，再按累计降序）。merchant 按本商户 owner。受 complain_open 门控。",
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
    }
   ],
   "responses": {
    "200": {
     "description": "summary + items[](排行榜) + columns[]"
    },
    "403": {
     "description": "complain_open 关闭"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaint-sync-tasks"] = {
  "get": {
   "operationId": "complaint_sync_tasks_get",
   "summary": "交易投诉 同步任务列表（admin）",
   "description": "complaint_tasks133 每渠道同步任务，join 渠道名称/支付平台：通道ID、通道名称、状态(enabled)、上次同步时间、同步状态(last_status/consecutive_failures/last_reason)、下次同步时间、获取频率(frequency 分钟 / num 条)。仅管理员；受 complain_open 门控。",
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
    }
   ],
   "responses": {
    "200": {
     "description": "items[] + total"
    },
    "403": {
     "description": "complain_open 关闭或非管理员"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaint-sync-tasks/{channel_id}"] = {
  "put": {
   "operationId": "complaint_sync_task_put",
   "summary": "新建/更新某渠道的投诉同步任务（admin）",
   "description": "按 channel_id upsert 同步任务。渠道的支付平台必须支持投诉（WeChat Pay V3 / Alipay），否则 422 拒绝并给出说明。bounds: frequency 1-1440 分钟，num 1-50 条。停用保留行与失败历史；重新启用重置连续失败计数并立即安排下次。审计记录。仅管理员；受 complain_open 门控。",
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
     "name": "channel_id",
     "in": "path",
     "required": true,
     "schema": {
      "type": "integer"
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
       "required": [
        "enabled",
        "frequency",
        "num"
       ],
       "properties": {
        "enabled": {
         "type": "boolean"
        },
        "frequency": {
         "type": "integer",
         "minimum": 1,
         "maximum": 1440,
         "description": "同步间隔（分钟）"
        },
        "num": {
         "type": "integer",
         "minimum": 1,
         "maximum": 50,
         "description": "每轮同步条数"
        }
       }
      }
     }
    }
   },
   "responses": {
    "200": {
     "description": "保存后的任务视图"
    },
    "400": {
     "description": "无效渠道或请求体"
    },
    "403": {
     "description": "complain_open 关闭或非管理员"
    },
    "404": {
     "description": "渠道不存在"
    },
    "422": {
     "description": "渠道不支持投诉或 frequency/num 越界"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaint-channels"] = {
  "get": {
   "operationId": "complaint_channels_get",
   "summary": "可投诉的支付通道（admin，自动获取选择器）",
   "description": "支付平台为 wxpay/alipay 的渠道，供投诉记录页的 自动获取 选择器使用；每项带 enabled（是否已开启同步任务）。仅管理员；受 complain_open 门控。",
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
    }
   ],
   "responses": {
    "200": {
     "description": "items[]{channel_id,name,provider,enabled}"
    },
    "403": {
     "description": "complain_open 关闭或非管理员"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints-sync"] = {
  "post": {
   "operationId": "complaints_sync_post",
   "summary": "立即同步某渠道的交易投诉（管理员）",
   "description": "按渠道凭证实时拉取 WeChat 消费者投诉2.0 / Alipay 交易投诉一页并幂等 upsert 到本地投诉表（按 provider 投诉ID去重，身份/订单/金额仅追加，状态只前进）。记录成功/失败到同步任务，连续失败达阈值(10)自动停用并通知管理员。仅管理员；受 complain_open 开关门控（关闭返回403）。",
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
    }
   ],
   "requestBody": {
    "required": true,
    "content": {
     "application/json": {
      "schema": {
       "type": "object",
       "additionalProperties": false,
       "required": [
        "channel_id"
       ],
       "properties": {
        "channel_id": {
         "type": "integer",
         "description": "支付通道ID"
        },
        "num": {
         "type": "integer",
         "minimum": 1,
         "maximum": 50,
         "description": "本轮同步条数，默认20"
        }
       }
      }
     }
    }
   },
   "responses": {
    "200": {
     "description": "已同步；返回 channel_id 与本轮 synced 条数"
    },
    "400": {
     "description": "渠道无效"
    },
    "403": {
     "description": "complain_open 关闭或非管理员"
    },
    "502": {
     "description": "渠道投诉结果未能确认（待重试）"
    },
    "503": {
     "description": "provider 不可用或功能未就绪"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints-image"] = {
  "post": {
   "operationId": "complaints_image_post",
   "summary": "上传投诉图片（微信V3媒体上传）",
   "description": "multipart 上传一张投诉图片到微信V3媒体接口，返回 media_id 供后续回复/处理引用。签名按微信规则对 meta(JSON: filename+sha256) 计算，非对 multipart 负载。需 ?channel_id=，受 complain_open 门控。",
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
     "name": "channel_id",
     "in": "query",
     "required": true,
     "schema": {
      "type": "integer"
     }
    }
   ],
   "requestBody": {
    "required": true,
    "content": {
     "multipart/form-data": {
      "schema": {
       "type": "object",
       "properties": {
        "file": {
         "type": "string",
         "format": "binary",
         "description": "jpg/jpeg/png/bmp，≤2MB"
        }
       },
       "required": [
        "file"
       ]
      }
     }
    }
   },
   "responses": {
    "200": {
     "description": "返回 media_id"
    },
    "400": {
     "description": "渠道无效或图片过大/格式不符"
    },
    "403": {
     "description": "complain_open 关闭"
    },
    "502": {
     "description": "媒体上传结果未能确认"
    },
    "503": {
     "description": "provider 不可用"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints/{id}/provider-reply"] = {
  "post": {
   "operationId": "complaints_provider_reply_post",
   "summary": "通过渠道回复投诉人",
   "description": "微信=回复用户(Response)，支付宝=留言回复(ReplySubmit)。按投诉记录所属渠道解析凭证并调用。merchant 仅限本商户投诉；受 complain_open 门控。images 为渠道媒体ID列表（可选）。",
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
     "name": "id",
     "in": "path",
     "required": true,
     "schema": {
      "type": "integer"
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
       "required": [
        "content"
       ],
       "properties": {
        "content": {
         "type": "string"
        },
        "images": {
         "type": "array",
         "items": {
          "type": "string"
         },
         "maxItems": 10
        }
       }
      }
     }
    }
   },
   "responses": {
    "200": {
     "description": "已提交"
    },
    "403": {
     "description": "complain_open 关闭或越权"
    },
    "404": {
     "description": "投诉不存在"
    },
    "409": {
     "description": "渠道不匹配或忙"
    },
    "422": {
     "description": "入参不合法"
    },
    "502": {
     "description": "渠道结果未能确认"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints/{id}/provider-complete"] = {
  "post": {
   "operationId": "complaints_provider_complete_post",
   "summary": "通过渠道完成/处理投诉",
   "description": "微信=反馈处理完成(Complete)，支付宝=商家处理(FeedbackSubmit，需 feedback_code+content)。按投诉所属渠道解析凭证。merchant 仅限本商户；受 complain_open 门控。",
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
     "name": "id",
     "in": "path",
     "required": true,
     "schema": {
      "type": "integer"
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
        "feedback_code": {
         "type": "string",
         "description": "支付宝处理分类码"
        },
        "content": {
         "type": "string"
        },
        "images": {
         "type": "array",
         "items": {
          "type": "string"
         },
         "maxItems": 10
        }
       }
      }
     }
    }
   },
   "responses": {
    "200": {
     "description": "已提交"
    },
    "403": {
     "description": "complain_open 关闭或越权"
    },
    "404": {
     "description": "投诉不存在"
    },
    "409": {
     "description": "渠道不匹配或忙"
    },
    "422": {
     "description": "入参不合法"
    },
    "502": {
     "description": "渠道结果未能确认"
    }
   }
  }
 };
 paths["/console/v1/{role}/complaints/{id}/provider-supplement"] = {
  "post": {
   "operationId": "complaints_provider_supplement_post",
   "summary": "补充投诉凭证（仅支付宝）",
   "description": "支付宝商家补充凭证(SupplementSubmit)。微信无此动作，返回不支持。merchant 仅限本商户；受 complain_open 门控。",
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
     "name": "id",
     "in": "path",
     "required": true,
     "schema": {
      "type": "integer"
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
       "required": [
        "content"
       ],
       "properties": {
        "content": {
         "type": "string"
        },
        "images": {
         "type": "array",
         "items": {
          "type": "string"
         },
         "maxItems": 10
        }
       }
      }
     }
    }
   },
   "responses": {
    "200": {
     "description": "已提交"
    },
    "403": {
     "description": "complain_open 关闭或越权"
    },
    "404": {
     "description": "投诉不存在"
    },
    "409": {
     "description": "该渠道不支持补充凭证"
    },
    "422": {
     "description": "入参不合法"
    },
    "502": {
     "description": "渠道结果未能确认"
    }
   }
  }
 };
}

// Pure additive installer: no filesystem, process, network or parent API replacement.
const owned = {
  "schemas": {
    "CoreRiskSettings": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "pay_iplimit",
        "pay_ip_interval",
        "pay_iplimit_white",
        "pay_userlimit",
        "pay_daymoney",
        "pay_region_block",
        "pay_block_oversea",
        "check_pay_regoin",
        "auto_check_payip",
        "check_payip_second",
        "check_payip_count",
        "auto_check_payspeed",
        "check_payspeed_second",
        "check_payspeed_count",
        "check_payspeed_days",
        "auto_check_sucrate",
        "check_sucrate_second",
        "check_sucrate_count",
        "check_sucrate_value",
        "check_sucrate_notice",
        "auto_check_channel",
        "check_channel_second",
        "check_channel_failcount",
        "check_channel_ids",
        "check_channel_notice"
      ],
      "properties": {
        "pay_iplimit": {
          "type": "string",
          "maxLength": 8192
        },
        "pay_ip_interval": {
          "type": "string",
          "maxLength": 8192
        },
        "pay_iplimit_white": {
          "type": "string",
          "maxLength": 8192
        },
        "pay_userlimit": {
          "type": "string",
          "maxLength": 8192
        },
        "pay_daymoney": {
          "type": "string",
          "maxLength": 8192
        },
        "pay_region_block": {
          "type": "string",
          "maxLength": 8192
        },
        "pay_block_oversea": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "check_pay_regoin": {
          "type": "string",
          "enum": [
            "0",
            "1",
            "2"
          ]
        },
        "auto_check_payip": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "check_payip_second": {
          "type": "string",
          "maxLength": 8192
        },
        "check_payip_count": {
          "type": "string",
          "maxLength": 8192
        },
        "auto_check_payspeed": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "check_payspeed_second": {
          "type": "string",
          "maxLength": 8192
        },
        "check_payspeed_count": {
          "type": "string",
          "maxLength": 8192
        },
        "check_payspeed_days": {
          "type": "string",
          "maxLength": 8192
        },
        "auto_check_sucrate": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "check_sucrate_second": {
          "type": "string",
          "maxLength": 8192
        },
        "check_sucrate_count": {
          "type": "string",
          "maxLength": 8192
        },
        "check_sucrate_value": {
          "type": "string",
          "maxLength": 8192
        },
        "check_sucrate_notice": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "auto_check_channel": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "check_channel_second": {
          "type": "string",
          "maxLength": 8192
        },
        "check_channel_failcount": {
          "type": "string",
          "maxLength": 8192
        },
        "check_channel_ids": {
          "type": "string",
          "maxLength": 8192
        },
        "check_channel_notice": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        }
      }
    },
    "CoreRiskPolicy": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "configured",
        "revision",
        "fingerprint",
        "geo_configured",
        "settings"
      ],
      "properties": {
        "configured": {
          "type": "boolean"
        },
        "revision": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        },
        "geo_configured": {
          "type": "boolean"
        },
        "settings": {
          "$ref": "#/components/schemas/CoreRiskSettings"
        }
      }
    },
    "CoreRiskPolicyPatch": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "settings",
        "expected_fingerprint"
      ],
      "properties": {
        "settings": {
          "$ref": "#/components/schemas/CoreRiskSettings"
        },
        "expected_fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        }
      }
    },
    "CoreRiskGroupConfig": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "pay_block_oversea",
        "check_pay_regoin"
      ],
      "properties": {
        "pay_block_oversea": {
          "type": "string",
          "enum": [
            "",
            "0",
            "1"
          ]
        },
        "check_pay_regoin": {
          "type": "string",
          "enum": [
            "",
            "0",
            "1",
            "2"
          ]
        }
      }
    },
    "CoreRiskGroup": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "group_id",
        "group_name",
        "configured",
        "revision",
        "fingerprint",
        "config",
        "effective_oversea",
        "effective_scan_region",
        "geo_configured"
      ],
      "properties": {
        "group_id": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "group_name": {
          "type": "string"
        },
        "configured": {
          "type": "boolean"
        },
        "revision": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        },
        "config": {
          "$ref": "#/components/schemas/CoreRiskGroupConfig"
        },
        "effective_oversea": {
          "type": "string",
          "enum": [
            "0",
            "1"
          ]
        },
        "effective_scan_region": {
          "type": "string",
          "enum": [
            "0",
            "1",
            "2"
          ]
        },
        "geo_configured": {
          "type": "boolean"
        }
      }
    },
    "CoreRiskGroupPatch": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "config",
        "expected_fingerprint"
      ],
      "properties": {
        "config": {
          "$ref": "#/components/schemas/CoreRiskGroupConfigInput"
        },
        "expected_fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        }
      }
    },
    "CoreRiskCheckRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "external_no",
        "expected_fingerprint"
      ],
      "properties": {
        "external_no": {
          "type": "string",
          "minLength": 8,
          "maxLength": 96,
          "pattern": "^[A-Za-z0-9:_-]+$"
        },
        "expected_fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        }
      }
    },
    "CoreRiskCheckResult": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "external_no",
        "policy_revision",
        "scanned",
        "actions",
        "at"
      ],
      "properties": {
        "external_no": {
          "type": "string"
        },
        "policy_revision": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "scanned": {
          "type": "integer",
          "minimum": 0,
          "maximum": 100000
        },
        "actions": {
          "type": "integer",
          "minimum": 0
        },
        "at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "CoreRiskAction": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "id",
        "target_id",
        "revision",
        "kind",
        "rule",
        "active",
        "until",
        "changed_at",
        "source_count",
        "source_order_ids",
        "fingerprint"
      ],
      "properties": {
        "id": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "target_id": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "revision": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "kind": {
          "type": "string",
          "enum": [
            "ip",
            "buyer",
            "merchant",
            "channel",
            "subchannel"
          ]
        },
        "rule": {
          "type": "string",
          "enum": [
            "auto_check_payip",
            "auto_check_payspeed",
            "auto_check_sucrate",
            "auto_check_channel"
          ]
        },
        "active": {
          "type": "boolean"
        },
        "until": {
          "type": [
            "string",
            "null"
          ],
          "format": "date-time"
        },
        "changed_at": {
          "type": "string",
          "format": "date-time"
        },
        "source_count": {
          "type": "integer",
          "minimum": 0,
          "maximum": 100000
        },
        "source_order_ids": {
          "type": "array",
          "maxItems": 20,
          "items": {
            "type": "string",
            "pattern": "^(0|[1-9][0-9]{0,18})$"
          },
          "uniqueItems": true
        },
        "fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        }
      }
    },
    "CoreRiskActions": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "items",
        "total"
      ],
      "properties": {
        "items": {
          "type": "array",
          "maxItems": 10000,
          "items": {
            "$ref": "#/components/schemas/CoreRiskAction"
          }
        },
        "total": {
          "type": "integer",
          "minimum": 0
        }
      }
    },
    "CoreRiskRelease": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "expected_fingerprint",
        "expected_policy_fingerprint"
      ],
      "properties": {
        "expected_fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        },
        "expected_policy_fingerprint": {
          "type": "string",
          "pattern": "^[0-9a-f]{64}$"
        }
      }
    },
    "CoreRiskGroupConfigInput": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "pay_block_oversea",
        "check_pay_regoin"
      ],
      "properties": {
        "pay_block_oversea": {
          "type": "string",
          "enum": [
            "",
            "_",
            "0",
            "1"
          ]
        },
        "check_pay_regoin": {
          "type": "string",
          "enum": [
            "",
            "_",
            "0",
            "1",
            "2"
          ]
        }
      }
    },
    "CoreRiskStatus": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "blocked",
        "reason",
        "until",
        "action_id",
        "revision"
      ],
      "properties": {
        "blocked": {
          "type": "boolean"
        },
        "reason": {
          "type": "string",
          "maxLength": 128
        },
        "until": {
          "type": [
            "string",
            "null"
          ],
          "format": "date-time"
        },
        "action_id": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        },
        "revision": {
          "type": "string",
          "pattern": "^(0|[1-9][0-9]{0,18})$"
        }
      }
    }
  },
  "paths": {
    "/console/v1/admin/payment-risk": {
      "get": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskPolicy",
        "parameters": [],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskPolicy"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        }
      },
      "patch": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskPolicyPatch",
        "parameters": [
          {
            "name": "X-CSRF-Token",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 43,
              "maxLength": 43
            }
          }
        ],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskPolicy"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/CoreRiskPolicyPatch"
              }
            }
          }
        }
      }
    },
    "/console/v1/admin/payment-risk/groups/{group_id}": {
      "get": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskGroup",
        "parameters": [
          {
            "name": "group_id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^(0|[1-9][0-9]{0,18})$"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskGroup"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        }
      },
      "patch": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskGroupPatch",
        "parameters": [
          {
            "name": "group_id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^(0|[1-9][0-9]{0,18})$"
            }
          },
          {
            "name": "X-CSRF-Token",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 43,
              "maxLength": 43
            }
          }
        ],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskGroup"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/CoreRiskGroupPatch"
              }
            }
          }
        }
      }
    },
    "/console/v1/admin/payment-risk/check": {
      "post": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskCheck",
        "parameters": [
          {
            "name": "X-CSRF-Token",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 43,
              "maxLength": 43
            }
          }
        ],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskCheckResult"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/CoreRiskCheckRequest"
              }
            }
          }
        }
      }
    },
    "/console/v1/admin/payment-risk/check-runs/{external_no}": {
      "get": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskCheckResult",
        "parameters": [
          {
            "name": "external_no",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 8,
              "maxLength": 96
            }
          }
        ],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskCheckResult"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        }
      }
    },
    "/console/v1/admin/payment-risk/actions": {
      "get": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskActions",
        "parameters": [],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskActions"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        }
      }
    },
    "/console/v1/admin/payment-risk/actions/{action_id}/release": {
      "post": {
        "tags": [
          "payment-risk"
        ],
        "security": [
          {
            "AdminSession": []
          }
        ],
        "x-required-admin-class": "super",
        "description": "仅本人持久 super 会话；无 query 参数；no-store。修改另需原 CSRF。拒绝重复、缺失、未知、null 和尾随 JSON。仅影响新首次支付，保留原订单查询/回调/未决意图恢复。",
        "operationId": "adminCoreRiskRelease",
        "parameters": [
          {
            "name": "action_id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^(0|[1-9][0-9]{0,18})$"
            }
          },
          {
            "name": "X-CSRF-Token",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 43,
              "maxLength": 43
            }
          }
        ],
        "responses": {
          "200": {
            "description": "可核实的原状态/提交结果",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": false,
                  "required": [
                    "code",
                    "message",
                    "data"
                  ],
                  "properties": {
                    "code": {
                      "type": "integer",
                      "const": 0
                    },
                    "message": {
                      "type": "string"
                    },
                    "data": {
                      "$ref": "#/components/schemas/CoreRiskAction"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "无效结构、目标或参数"
          },
          "401": {
            "description": "会话失效"
          },
          "403": {
            "description": "原本人超级管理员/CSRF 校验失败"
          },
          "409": {
            "description": "CAS 或原状态来源冲突，刷新后重新确认"
          },
          "422": {
            "description": "设置无效或必要本地地址库不可用"
          },
          "503": {
            "description": "来源/数据库不可核实，不重复提交"
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/CoreRiskRelease"
              }
            }
          }
        }
      }
    }
  }
};
export function installCoreRisk177API(schemas,paths){
 if(!schemas||!paths||typeof schemas!=='object'||typeof paths!=='object')throw new TypeError('API schema/path objects required');
 Object.assign(schemas,structuredClone(owned.schemas));Object.assign(paths,structuredClone(owned.paths));
 const add=(node,props)=>{if(!node||typeof node!=='object')return;if(node.type==='object'&&node.properties)Object.assign(node.properties,structuredClone(props));for(const k of ['oneOf','allOf','anyOf'])for(const child of node[k]||[])add(child,props)};
 add(schemas.RuntimeChannelPolicy,{core_risk:{$ref:'#/components/schemas/CoreRiskStatus'}});
 add(schemas.PaymentSubchannel,{core_risk:{$ref:'#/components/schemas/CoreRiskStatus'},parent_core_risk:{$ref:'#/components/schemas/CoreRiskStatus'}});
}

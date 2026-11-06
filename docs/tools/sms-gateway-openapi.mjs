// Original sms_api gateway settings: provider keys, active gateway and a test send.
export function installSmsGatewayAPI({paths}) {
 paths["/console/v1/admin/settings/sms-channels"] = {
  "get": {
   "tags": [
    "settings"
   ],
   "security": [
    {
     "AdminSession": []
    }
   ],
   "parameters": [],
   "responses": {
    "200": {
     "description": "SMS网关状态：providers(aliyun/qcloud/smsbao 各 configured/template/sign/principal)、active、status、CAS fingerprint。密钥从不返回。"
    },
    "403": {
     "description": "本人超级管理员或CSRF校验失败。"
    },
    "503": {
     "description": "配置来源或密文无法确认。"
    }
   },
   "operationId": "smsGateway134Read",
   "summary": "原短信网关(sms_api)配置"
  },
  "patch": {
   "tags": [
    "settings"
   ],
   "security": [
    {
     "AdminSession": []
    }
   ],
   "responses": {
    "200": {
     "description": "保存后返回最新状态（不含密钥）。"
    },
    "400": {
     "description": "拒绝query、缺少字段或无效确认。"
    },
    "403": {
     "description": "本人超级管理员或CSRF校验失败。"
    },
    "409": {
     "description": "网关版本变化，重新读取。"
    },
    "422": {
     "description": "字段无效，或开启网关但无已配置的活动服务商。"
    },
    "503": {
     "description": "配置来源或密文无法确认。"
    }
   },
   "operationId": "smsGateway134Save",
   "summary": "保存短信服务商密钥/选择活动网关"
  }
 };
 paths["/console/v1/admin/settings/sms-channels/test"] = {
  "post": {
   "tags": [
    "settings"
   ],
   "security": [
    {
     "AdminSession": []
    }
   ],
   "responses": {
    "200": {
     "description": "测试发送受理结果：request_id/status(accepted|failed)/reason。先落库再上行，不重发。"
    },
    "400": {
     "description": "测试手机号或当前确认无效。"
    },
    "403": {
     "description": "本人超级管理员或CSRF校验失败。"
    },
    "409": {
     "description": "网关版本变化，重新读取。"
    },
    "422": {
     "description": "网关未开启或无活动服务商。"
    },
    "503": {
     "description": "测试请求无法记录或网关不可用。"
    }
   },
   "operationId": "smsGateway134Test",
   "summary": "短信网关测试发送"
  }
 };
}

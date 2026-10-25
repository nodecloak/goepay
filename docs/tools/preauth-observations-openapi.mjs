// Independent, explicit read of the original Alipay freeze operation.
export function installPreauthObservationsAPI({paths,schemas,object,str,id,ref,content}) {
 const nonce=str('本地64位小写十六进制查询意图；不作为支付宝操作号。',{pattern:'^[0-9a-f]{64}$'});
 const minor=str('人民币分的非负十进制整数字符串。',{pattern:'^(0|[1-9][0-9]*)$'});
 schemas.PreauthorizationObservationCreate=object({client_request_no:nonce},['client_request_no']);
 schemas.PreauthorizationObservation=object({
  request_id:nonce,client_request_no:nonce,merchant_id:id,order_id:id,attempt_id:id,channel_id:id,trade_no:str(),app_id:str(),auth_no:str(),buyer_id:str(),amount:minor,
  status:str(null,{enum:['processing','verified','unknown']}),rest_known:{type:'boolean'},rest_amount:minor,
  provider_status:str(),order_status:str(),operation_type:str(),observed_at:str(null,{format:'date-time'}),
  source_verified:{type:'boolean',description:'仅经真实厂商签名、原冻结身份/金额/物理账户及本地来源/结果/审计证明核验的历史观测为true；不代表当前可执行金额。'}
 },['request_id','client_request_no','merchant_id','order_id','attempt_id','channel_id','trade_no','app_id','auth_no','buyer_id','amount','status','rest_known','source_verified']);
 const envelope=content(object({code:{type:'integer'},message:str(),data:ref('PreauthorizationObservation')},['code','message','data']));
 const params=[{name:'role',in:'path',required:true,schema:str(null,{enum:['admin','merchant']})},{name:'trade_no',in:'path',required:true,schema:str(null,{minLength:1})}];
 const security=[{AdminSession:[]},{MerchantSession:[]}];
 const responses={200:{description:'verified历史观测；金额和ID均字符串，rest_known=true时rest_amount可为字符串0。',content:envelope},202:{description:'原意图processing或unknown；不是查询成功，rest_amount省略，不得伪造0。',content:envelope},400:{description:'非规范JSON/重复或未知字段/查询参数/非规范nonce。'},401:{description:'当前本人会话失效。'},403:{description:'CSRF、代登录或当前权限拒绝。'},404:{description:'订单、观测不存在或归属/路径不匹配。'},409:{description:'原账户绑定、会话或业务来源变化；无可信余额返回。'},503:{description:'独立读取开关关闭、非configured HTTPS、sandbox或通道能力不可用。'}};
 const base='/console/v1/{role}/orders/{trade_no}/authorization-observations';
 paths[base]={post:{operationId:'preauthorizationObservationCreate',summary:'显式观测原预授权余款',description:'默认关闭的GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION独立只读能力，不依赖付款写开关，也不授予资金操作。只允许原始本人商户/管理员会话，拒绝代登录；CSRF必需。先提交本地查询意图和审计，再调用一次原冻结alipay.fund.auth.operation.detail.query，厂商out_request_no始终原trade_no。相同client_request_no永久复用原意图，unknown/processing不重查；明确重新观测可显式新号。资金表、扣款/解冻、账本和作业均不变。不接受query参数。',parameters:[...params,{name:'X-CSRF-Token',in:'header',required:true,schema:str()}],security,requestBody:{required:true,content:content(ref('PreauthorizationObservationCreate'))},responses}};
 paths[base+'/{request_id}']={get:{operationId:'preauthorizationObservationRead',summary:'本地读取原余款观测',description:'零厂商I/O。当前有效本人商户或管理员会话重新核验订单权限、通道允许范围和原物理账户；历史观测不要求创建会话仍登录，代登录拒绝。严格匹配merchant/order/trade路径和原request_id。返回no-store；无HMAC、密钥或原始厂商报文。observed_at为历史时间，unknown不能用于判断余额。',parameters:[...params,{name:'request_id',in:'path',required:true,schema:nonce}],security,responses}};
}

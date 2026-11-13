// Concrete role routes keep super-only actions out of the merchant contract.
// Public checkout capabilities explicitly override the document's cookie auth.
export function installCryptoCustodyAPI({paths,schemas,object,str,ref,content}) {
  const decimalPattern='^[0-9]+(?:\\.[0-9]{1,6})?$';
  schemas.CryptoAmount=str('原生USDT数量的正十进制字符串，最多6位小数，最大9000000000 USDT；不能传JSON number、科学计数法或人民币金额。',{pattern:decimalPattern,maxLength:24,not:{pattern:'^0+(?:\\.0+)?$'},examples:['10','0.000001','12.345678']});
  schemas.CryptoNonnegativeAmount=str('USDT数量的非负十进制字符串，最多6位小数；0合法。独立于人民币账本。',{pattern:decimalPattern,examples:['0','9.9','0.000001']});
  schemas.CryptoNetwork=str('必须明确原USDT网络；TRON=TRC20，Ethereum=ERC20。不同网络不混账，不自动换链。',{enum:['tron','ethereum']});
  schemas.CryptoMerchantID=str('完整正十进制商户ID字符串；JSON number不接受。商户省略时取当前本人会话，显式提供也只能为本人；管理员写入时必须明确商户。',{pattern:'^[1-9][0-9]*$'});
  schemas.CryptoRecordID=str('原USDT记录的48位小写十六进制ID；不是旧人民币订单号。',{pattern:'^[0-9a-f]{48}$'});
  schemas.CryptoRequestID=str('商户内稳定原业务请求号；同号只允许相同网络、金额、受款地址和原收款单。结果未知先查询原号，不能换号重复出款。',{pattern:'^[!-~]+$',minLength:1,maxLength:128});
  schemas.CryptoAddress=str('对应网络的公开地址。TRON读取为Base58Check，Ethereum读取为规范小写hex；写入校验网络及校验和。不接受私钥。',{minLength:1,maxLength:42});
  const password=str('当前原始登录账号本人的登录密码，仅本次确认使用；不是商户API密钥或管理员独立资金密码。不得放入URL、日志、审计或浏览器持久存储。',{format:'password',writeOnly:true,minLength:1,maxLength:1024});
  const time=str(null,{format:'date-time'});
  const invoiceState=str(null,{enum:['pending','underpaid','paid','overpaid','expired','late']});
  schemas.CryptoInvoiceInput=object({merchant_id:ref('CryptoMerchantID'),out_trade_no:ref('CryptoRequestID'),network:ref('CryptoNetwork'),amount:ref('CryptoAmount'),notify_url:str('可省略或空；非空须HTTPS。回调按原记录签名并独立重试。',{maxLength:2048}),return_url:str('可省略或空；非空须HTTPS。',{maxLength:2048})},['out_trade_no','network','amount']);
  schemas.CryptoWithdrawalInput=object({merchant_id:ref('CryptoMerchantID'),request_id:ref('CryptoRequestID'),network:ref('CryptoNetwork'),address:ref('CryptoAddress'),amount:ref('CryptoAmount'),current_password:password},['request_id','network','address','amount','current_password']);
  schemas.CryptoRefundInput=object({merchant_id:ref('CryptoMerchantID'),request_id:ref('CryptoRequestID'),invoice_id:ref('CryptoRecordID'),address:ref('CryptoAddress'),amount:ref('CryptoAmount'),current_password:password},['request_id','invoice_id','address','amount','current_password']);
  schemas.CryptoRecipientInput=object({merchant_id:ref('CryptoMerchantID'),network:ref('CryptoNetwork'),address:ref('CryptoAddress'),current_password:password},['network','address','current_password']);
  schemas.CryptoOwnerPasswordInput=object({merchant_id:ref('CryptoMerchantID'),current_password:password},['current_password']);
  schemas.CryptoPasswordInput=object({current_password:password},['current_password']);
  schemas.CryptoAddressInput=object({network:ref('CryptoNetwork'),address:ref('CryptoAddress'),role:str('仅公开地址用途元数据，不修改隔离签名服务白名单或私钥配置。',{enum:['deposit','treasury','hot']}),current_password:password},['network','address','role','current_password']);
  schemas.CryptoSweepRetryInput=object({network:ref('CryptoNetwork'),address:ref('CryptoAddress'),current_password:password},['network','address','current_password']);
  schemas.CryptoRequestNotRecorded=object({request_not_recorded:{type:'boolean',const:true},kind:{type:'string',const:'crypto'},merchant_id:ref('CryptoMerchantID'),external_no:ref('CryptoRequestID'),reason:{type:'string',const:'conflict'}},['request_not_recorded','kind','merchant_id','external_no','reason']);
  const invoiceProperties={id:ref('CryptoRecordID'),merchant_id:ref('CryptoMerchantID'),out_trade_no:ref('CryptoRequestID'),network:ref('CryptoNetwork'),address:ref('CryptoAddress'),amount:ref('CryptoAmount'),received:ref('CryptoNonnegativeAmount'),fee:ref('CryptoNonnegativeAmount'),state:invoiceState,expires_at:time,notify_url:str(),return_url:str(),cny_amount:str('兼容订单原人民币分数值的整数字符串；原生USDT收款单为0，不是USDT金额。',{pattern:'^(0|[1-9][0-9]*)$'}),name:str(),param:str(),version:str(null,{enum:['native','v1','v2']})};
  schemas.CryptoChannelID=str('统一支付通道的完整正int64字符串。不能使用目录资料ID或客户端自定义网络代替。',{pattern:'^[1-9][0-9]*$',maxLength:19});
  const channelCounter=str('完整非负int64字符串；0表示历史订单没有统一通道快照。',{pattern:'^(0|[1-9][0-9]*)$',maxLength:19});
  schemas.CryptoInvoiceInput.properties.channel_id=ref('CryptoChannelID');
  schemas.CryptoInvoiceInput.required=['out_trade_no','amount'];
  Object.assign(invoiceProperties,{channel_id:channelCounter,channel_revision:channelCounter,fee_bps:{type:'integer',minimum:0,maximum:10000},fee_source:str('已冻结的费用来源；legacy_network表示接入统一通道之前的历史记录。',{enum:['legacy_network','channel','group']}),channel_snapshot:{type:'object',additionalProperties:true,description:'创建时冻结的公开通道与组规则快照。历史记录为空对象；不包含私钥或凭据。'}});
  schemas.CryptoInvoiceView=object(invoiceProperties,Object.keys(invoiceProperties));
  schemas.CryptoInvoiceCheckoutView=object({...invoiceProperties,checkout_url:str('本次有权调用者获得的相对公开收银台URL，包含敏感token能力；不写日志或浏览器持久存储。')},[...Object.keys(invoiceProperties),'checkout_url']);
  const withdrawalProperties={id:ref('CryptoRecordID'),merchant_id:ref('CryptoMerchantID'),kind:str(null,{enum:['settlement','refund','exception_refund','sweep']}),request_id:ref('CryptoRequestID'),network:ref('CryptoNetwork'),address:ref('CryptoAddress'),amount:ref('CryptoAmount'),state:str('签名、广播受理和unknown均不代表链上完成；confirmed需原交易双源最终执行和USDT转账事件证明。',{enum:['requested','approved','preparing','prepared','signing','signed','broadcasting','unknown','confirmed','failed','cancelled']}),tx_id:str('未签名前可为空。已签名后始终核对原交易，不因超时换号重发。'),invoice_id:str('关联原收款单ID；结算申请可为空。'),approved_by:str('完整审批人ID字符串；尚未审批为0。',{pattern:'^(0|[1-9][0-9]*)$'}),cancellation_requested:{type:'boolean',description:'仅表示取消请求已登记。worker完成隔离签名器撤销确认并持久记账前，预留资金不会释放；必须继续查原request_id。'}};
  schemas.CryptoWithdrawalView=object(withdrawalProperties,Object.keys(withdrawalProperties));
  schemas.CryptoRecipientView=object({id:ref('CryptoRecordID'),merchant_id:ref('CryptoMerchantID'),network:ref('CryptoNetwork'),address:ref('CryptoAddress'),state:str(null,{enum:['requested','approved','revoked']}),usable_at:time},['id','merchant_id','network','address','state','usable_at']);
  schemas.CryptoBalanceView=object({network:ref('CryptoNetwork'),available:ref('CryptoNonnegativeAmount'),reserved:ref('CryptoNonnegativeAmount')},['network','available','reserved']);
  schemas.CryptoConfigView=object({configured:{type:'boolean',description:'USDT服务是否已配置。false时资金及历史查询返回503，channels返回空数组；管理员仍可管理通道配置；true且enabled=false时仍可按原权限读取历史记录。'},enabled:{type:'boolean',description:'至少一个USDT网络已启用；false时不应发起新的收款或资金申请，不代表历史记录为空。'},networks:{type:'array',maxItems:2,items:object({network:ref('CryptoNetwork'),enabled:{type:'boolean'},fee_bps:{type:'integer',minimum:0,maximum:10000}},['network','enabled','fee_bps'])},simulation:{type:'boolean',description:'true表示模拟环境，请勿转入真实资产；不是签名或真实广播已开启的声明。'}},['configured','enabled','networks','simulation']);
  schemas.CryptoReconciliationView=object({imbalanced_journals:{type:'integer',minimum:0},balance_mismatches:{type:'integer',minimum:0},negative_balances:{type:'integer',minimum:0}},['imbalanced_journals','balance_mismatches','negative_balances']);
  schemas.CryptoCheckoutStatusView=object({state:invoiceState,amount:ref('CryptoAmount'),received:ref('CryptoNonnegativeAmount'),expires_at:time,return_url:str('仅paid/overpaid且原商户返回地址可签名时返回的完整签名URL；未付款不返回。')},['state','amount','received','expires_at']);

  const csrf={name:'X-CSRF-Token',in:'header',required:true,schema:{type:'string',minLength:1},description:'当前本角色会话返回的csrf_token。所有console写入必需。'};
  const envelope=data=>object({code:{type:'integer',const:0},message:str(),data},['code','message','data']);
  const errors={
    '400':{description:'JSON格式、字段类型或未知/重复字段无效；merchant_id必须为字符串。',content:content(ref('Envelope'))},
    '401':{description:'本角色登录会话无效。',content:content(ref('Envelope'))},
    '403':{description:'CSRF、角色、商户归属或原始本人会话校验失败；admin仅本人super，写入拒绝委派/代登录/撤销/禁用/过期会话。',content:content(ref('Envelope'))},
    '404':{description:'记录不存在、已无权查看或该角色没有此路由。',content:content(ref('Envelope'))},
    '409':{description:'原请求参数或状态冲突、收款地址未批准/冷静期未结束、已有活跃归集或已签/未知交易不可直接取消。',content:content(ref('Envelope'))},
    '422':{description:'金额、网络、地址、当前密码无效/未设置，或USDT可用资金不足。',content:content(ref('Envelope'))},
    '429':{description:'本人密码校验来源/账号预算或并发预算耗尽。',headers:{'Retry-After':{schema:{type:'integer'}}},content:content(ref('Envelope'))},
    '503':{description:'USDT服务尚未配置时明确提示尚未启用，当前无法处理此操作；其它服务不可用或结果无法确认时，保留原request_id查询，不得以新号重复资金请求。该状态本身不证明旧请求未记录。',content:content(ref('Envelope'))},
  };
  const scope='USDT账本与人民币账本独立。读取使用对应角色Cookie并校验归属；所有写入要求原始本人持久会话、CSRF及数据库事务内重新核验。admin仅super。标为current_password必填的动作始终确认本人当前登录密码，不受人民币资金确认开关替代。';
  function operation(role,route,method,summary,{body,data,description='',parameters=[]}={}){
    const full=`/console/v1/${role}/crypto${route}`;
    const pathParameters=[...full.matchAll(/\{([^}]+)\}/g)].map(([,name])=>({name,in:'path',required:true,schema:ref('CryptoRecordID')}));
    const bodySchema=body?(role==='admin'&&schemas[body].properties.merchant_id?{allOf:[ref(body),{required:['merchant_id']}]}:ref(body)):null;
    (paths[full]??={})[method]={operationId:`crypto_${role}_${method}_${route.replace(/[^a-zA-Z0-9]+/g,'_').replace(/^_|_$/g,'')}`,tags:['crypto-custody'],summary,description:`${scope} ${description}`,security:[{[role==='admin'?'AdminSession':'MerchantSession']:[]}],parameters:[...pathParameters,...parameters,...(method==='post'?[csrf]:[])],...(bodySchema?{requestBody:{required:true,content:content(bodySchema)}}:{}),responses:{'200':{description:'no-store；本地申请/审批/取消登记成功不等于链上资金已发送、已取消或已到账。',content:content(envelope(data??{type:'null'}))},...structuredClone(errors)}};
    if(body==='CryptoWithdrawalInput'||body==='CryptoRefundInput')paths[full][method].responses['409']={description:'原请求或当前状态冲突。只有data.request_not_recorded=true且merchant_id/external_no匹配本次原请求，才证明本次意图未记录；其它409不能据此推断未发送或自行换号。',content:content(object({code:{type:'integer',const:-1},message:str(),data:{anyOf:[{type:'null'},ref('CryptoRequestNotRecorded')]}},['code','message','data']))};
  }
  const merchantQuery=(role,{required=false,balance=false}={})=>({name:'merchant_id',in:'query',required:role==='admin'&&required,schema:ref('CryptoMerchantID'),description:role==='merchant'?'省略取当前本人；显式提供只能为本人完整字符串ID。':balance?'省略按网络汇总全部商户的available/reserved，不含平台fees/suspense，不是链上钱包余额；显式ID只查询该商户。':required?'必须明确原请求所属商户的完整字符串ID。':'省略可读取全部商户记录；显式ID只读取该商户。'});
  const channelChoice={id:ref('CryptoChannelID'),name:str(null,{minLength:1,maxLength:120}),network:ref('CryptoNetwork'),fee_bps:{type:'integer',minimum:0,maximum:10000},min_amount:ref('CryptoNonnegativeAmount'),max_amount:ref('CryptoNonnegativeAmount'),day_limit:ref('CryptoNonnegativeAmount')};
  schemas.CryptoChannelChoice=object(channelChoice,Object.keys(channelChoice));
  const channelView={...channelChoice,revision:ref('CryptoChannelID'),enabled:{type:'boolean'},archived:{type:'boolean'},merchant_ids:{type:'array',maxItems:1000,uniqueItems:true,items:ref('CryptoMerchantID')},created_at:time,updated_at:time,pay_type:{type:'string',const:'usdt'},plugin:{type:'string',enum:['usdt_tron','usdt_ethereum']},configured:{type:'boolean',const:true},asset:{type:'string',const:'USDT'},runtime_enabled:{type:'boolean',description:'部署侧当前网络是否启用。保存通道配置不能修改该值，也不能开启签名或广播。'}};
  schemas.CryptoChannelView=object(channelView,Object.keys(channelView));
  const {id:_channelID,...editableChannel}=channelChoice;
  const channelInput={...editableChannel,enabled:{type:'boolean'},archived:{type:'boolean'},merchant_ids:channelView.merchant_ids,expected_revision:ref('CryptoChannelID')};
  schemas.CryptoChannelPatch=object(channelInput,Object.keys(channelInput));
  schemas.CryptoChannelCreate=object({...channelInput,expected_revision:{type:'string',const:'0'},request_id:str('本管理员稳定创建请求号，丢失响应使用同号同参数恢复；不能自动换号重试。',{pattern:'^[0-9a-f]{32}$'})},[...Object.keys(channelInput),'request_id']);
  const channelsBase='/console/v1/admin/payment-channels/crypto';
  const channelOp=(method,detail,body)=>({operationId:`crypto_channel_${method}_${detail?'detail':'catalog'}`,tags:['crypto-custody'],summary:method==='get'?'读取统一USDT通道配置':detail?'修改USDT通道配置与商户授权':'创建统一USDT支付通道',description:'仅本人super；与普通支付通道共享编号。网络创建后不可修改，限额单位为USDT，0表示不设该限额；fee_bps为扣费比例。修改必须带原expected_revision和完整字段。关闭或归档只影响新收款，不重写历史订单。部署未启用USDT时仍可管理配置，但不会启用主网、签名或广播。',security:[{AdminSession:[]}],parameters:[...(detail?[{name:'channel_id',in:'path',required:true,schema:ref('CryptoChannelID')}]:[]),...(method!=='get'?[csrf]:[])],...(body?{requestBody:{required:true,content:content(ref(body))}}:{}),responses:{'200':{description:'配置读取或写入成功；不表示主网运行已启用。',content:content(envelope(method==='get'&&!detail?object({items:{type:'array',items:ref('CryptoChannelView')}},['items']):ref('CryptoChannelView')))},...structuredClone(errors)}});
  paths[channelsBase]={get:channelOp('get',false),post:channelOp('post',false,'CryptoChannelCreate')};
  paths[channelsBase+'/{channel_id}']={get:channelOp('get',true),patch:channelOp('patch',true,'CryptoChannelPatch')};
  // Extend the shared list/detail union, leaving CNY create/mutation contracts intact.
  if(schemas.RuntimeParentChannel?.oneOf&&!schemas.RuntimeParentChannel.oneOf.some(v=>v.$ref===ref('CryptoChannelView').$ref))schemas.RuntimeParentChannel.oneOf.push(ref('CryptoChannelView'));
  const commonChannels=paths['/console/v1/admin/payment-channels']?.get;
  if(commonChannels){
    commonChannels.summary='分页读取支付通道（人民币与USDT）';
    for(const parameter of commonChannels.parameters??[]){
      const added=parameter.name==='type'?['usdt']:parameter.name==='plugin'?['usdt_tron','usdt_ethereum']:[];
      if(parameter.schema?.enum)parameter.schema.enum=[...new Set([...parameter.schema.enum,...added])];
    }
  }
  for(const role of ['admin','merchant']){
    operation(role,'/channels','get','读取商户当前获准使用的USDT通道',{data:object({items:{type:'array',items:ref('CryptoChannelChoice')}},['items']),parameters:[merchantQuery(role)],description:'只列可用于新收款的通道；fee_bps已应用该商户有效用户组费率。部署或支付方式关闭时返回空列表。管理员未指定商户时也返回空列表，不展示所有商户授权。响应不含merchant_ids、私钥或RPC。'});
    operation(role,'/config','get','读取USDT配置、网络启用与模拟标记',{data:ref('CryptoConfigView'),description:'只读能力摘要。没有私钥、RPC凭据、热钱包或签名白名单写入接口；configured=false表示服务尚未配置，资金及历史查询返回503、channels返回空数组；管理员通道配置仍可管理。configured=true且enabled=false表示网络均关闭，仍可按原权限读取历史记录；关闭态不证明历史记录为空，也不授予新的资金操作权限。'});
    operation(role,'/balances','get','读取独立USDT可用及预留余额',{data:{type:'array',items:ref('CryptoBalanceView')},parameters:[merchantQuery(role,{balance:true})],description:'available/reserved均为原生USDT数量字符串，最多6位小数；不转换为人民币，不代表对应热钱包可立即出款。'});
    operation(role,'/invoices','get','读取USDT收款单列表',{data:{type:'array',maxItems:200,items:ref('CryptoInvoiceView')},parameters:[merchantQuery(role),{name:'channel_id',in:'query',required:false,schema:ref('CryptoChannelID')}],description:'最多最新200条；可按原通道筛选。列表不返回checkout_url或token。状态来自最终链上收款证据；少付、超付和迟到款不会被自动改成可用余额。'});
    operation(role,'/invoices/{invoice_id}','get','读取原USDT收款单及收银台入口',{data:ref('CryptoInvoiceCheckoutView'),parameters:[merchantQuery(role)],description:'仅有权会话可获得该收款单checkout_url；其token能力仅用于专用公开收银台路径，不授权任何console操作。'});
    operation(role,'/invoices','post','创建原生USDT收款单',{body:'CryptoInvoiceInput',data:ref('CryptoInvoiceCheckoutView'),description:'amount是USDT数量，最多6位小数；固定15分钟有效期。可明确channel_id及network，网络只能限制选择，不能覆盖通道固定网络；省略时按商户授权和有效组策略选择。所有新单检查支付方式、通道启停、授权、组规则及USDT限额。固定通道、网络、报价和费用快照。此动作无需current_password。相同商户out_trade_no和相同原请求返回原单，修改原通道/网络/金额/URL冲突；与人民币订单共享外部单号命名空间。写入不代表已付款。'});
    operation(role,'/withdrawals','get','读取原币结算、退款及归集申请',{data:{type:'array',maxItems:200,items:ref('CryptoWithdrawalView')},parameters:[merchantQuery(role)],description:'最多最新200条；读取本地状态，不发起签名、广播或替换未知交易。'});
    operation(role,'/withdrawals/by-request','get','按原商户业务号恢复USDT出款申请',{data:ref('CryptoWithdrawalView'),parameters:[merchantQuery(role,{required:true}),{name:'request_id',in:'query',required:true,schema:ref('CryptoRequestID')}],description:'提交、审批或取消响应丢失时先查询同一原request_id；unknown不证明交易未发送。'});
    operation(role,'/withdrawals','post','申请原网络USDT结算并预留余额',{body:'CryptoWithdrawalInput',data:ref('CryptoWithdrawalView'),description:'要求批准的原网络收款地址且创建已满24小时。只创建requested并预留本商户该网络USDT，仍需super独立审批和隔离worker处理；不收取人民币、不接受私钥或客户端原始交易。'});
    operation(role,'/refunds','post','申请原USDT收款单的原链退款',{body:'CryptoRefundInput',data:ref('CryptoWithdrawalView'),description:'network由invoice_id固定，不能在body改链。已入账原单累计有效退款不超过原收款单金额，并需本商户该网络可用余额。明确提供已批准且冷静期结束的退款地址；不会从旧人民币退款参数猜测地址，响应只表示申请/预留。'});
    operation(role,'/held-refunds','post','申请退回少付、超付或迟到暂存USDT',{body:'CryptoRefundInput',data:ref('CryptoWithdrawalView'),description:'只使用原invoice_id仍未释放的暂存额度；不把暂存款记入商户可用余额。原链、明确批准的收款地址、独立审批及worker仍必须满足；重复request_id只恢复原意图。'});
    operation(role,'/withdrawals/{withdrawal_id}/cancel','post','申请取消原USDT出款',{body:'CryptoOwnerPasswordInput',data:ref('CryptoWithdrawalView'),description:'requested且尚无发送风险的申请可提交cancelled并一次释放预留。approved/preparing/prepared/signing只登记cancellation_requested=true，先不释放；worker与隔离签名器确认原意图不可再签后才能完成取消。signed/broadcasting/unknown不能直接取消或释放，必须核对原交易；sweep不走此取消入口。'});
    operation(role,'/recipients','get','读取原网络收款地址审批状态',{data:{type:'array',items:ref('CryptoRecipientView')},parameters:[merchantQuery(role)],description:'只有approved且当前时间达到usable_at才可申请该地址出款。'});
    operation(role,'/recipients','post','登记待审批的公开USDT收款地址',{body:'CryptoRecipientInput',data:ref('CryptoRecipientView'),description:'创建requested和自登记起24小时冷静期；super审批本身不跳过usable_at。只登记公开收款地址，不导入私钥，也不修改隔离签名器独立目的地址白名单。'});
    operation(role,'/recipients/{recipient_id}/revoke','post','撤销所属商户USDT收款地址',{body:'CryptoOwnerPasswordInput',description:'撤销后阻止尚未签名的出款准入；不能撤回已签名或已广播交易，也不会自动释放其预留。'});
  }
  operation('admin','/reconciliation','get','核对独立USDT账本内部一致性',{data:ref('CryptoReconciliationView'),description:'只读journal平衡、账户分录投影与异常负余额计数。零异常不等于链上钱包资产已对账；不生成补账或发起出款。'});
  operation('admin','/withdrawals/{withdrawal_id}/approve','post','super批准原USDT出款申请',{body:'CryptoPasswordInput',data:ref('CryptoWithdrawalView'),description:'从requested批准原不可变申请；并不立即签名或广播，不允许替换金额、网络、地址或业务号。'});
  operation('admin','/recipients/{recipient_id}/approve','post','super批准原USDT收款地址',{body:'CryptoPasswordInput',data:ref('CryptoRecipientView'),description:'仅批准原地址；登记24小时冷静期照常生效。此审批不更新隔离签名服务独立部署白名单。'});
  operation('admin','/addresses','post','导入公开USDT地址用途',{body:'CryptoAddressInput',description:'仅导入公开地址池元数据，要求规范地址。不会创建钱包私钥、改变worker热钱包配置、开通真实广播或授予签名器权限。'});
  operation('admin','/sweeps/retry','post','super显式恢复已暂停的原地址归集队列',{body:'CryptoSweepRetryInput',description:'本人密码确认后，仅为已有原网络收款地址的队列清除失败计数并递增revision、重新安排worker检查；有活跃原出款时409。不会直接发送、替换unknown交易或跳过独立签名白名单。'});

  for(const [suffix,summary,mime,data]of [
    ['','使用token打开专用USDT收银台','text/html',{type:'string'}],
    ['/status','使用token读取该收款单安全状态','application/json',envelope(ref('CryptoCheckoutStatusView'))],
    ['/qr','使用token读取该收款单地址二维码','image/png',{type:'string',format:'binary'}],
  ]){
    const route='/crypto/checkout/{invoice_id}'+suffix;
    paths[route]={get:{operationId:`crypto_public_checkout_${suffix.replace('/','')||'page'}`,tags:['crypto-checkout'],summary,description:'专用公开token能力路径，不是登录后台API；无需console Cookie，但必须提供该原收款单token。能力不授权其他收款单、商户身份、回调地址或console操作。禁止日志/浏览器持久存储泄露token。二维码只编码收款地址，付款人仍须自行核对所示网络和USDT金额。',security:[],parameters:[{name:'invoice_id',in:'path',required:true,schema:ref('CryptoRecordID')},{name:'token',in:'query',required:true,schema:{type:'string',pattern:'^[A-Za-z0-9_-]{43}$',minLength:43,maxLength:43,writeOnly:true},description:'该原收款单敏感能力token，来自有权创建/读取单据时的checkout_url。'}],responses:{'200':{description:'Cache-Control: no-store；Referrer-Policy: no-referrer。status返回state/amount/received/expires_at；付款确认后可附签名return_url，不返回商户身份字段、token或通知地址。',headers:{'Cache-Control':{schema:{type:'string',const:'no-store'}},'Referrer-Policy':{schema:{type:'string',const:'no-referrer'}}},content:{[mime]:{schema:data}}},'404':{description:'token错误、单据不存在或路径无效。',content:content(ref('Envelope'))},'405':{description:'只接受GET。',content:content(ref('Envelope'))},'503':{description:'USDT服务或QR生成暂不可用。',content:content(ref('Envelope'))}}}};
  }
}

// Requires the independently authenticated profit71 and balance-membership72 cores.
export function installReferralFundingAPI({paths,schemas,object,str,ref,content}) {
  const policy=paths['/console/v1/{role}/referral-policies/{scope}/{scope_id}'];
  if(!policy?.post||!schemas.ReferralDailyPolicyView)throw Error('Daily referral API must be installed first');
  const basis=str('fee按原手续费、gross按原订单金额（原手续费封顶）；profit按原手续费减去通道配置估计成本后half-up到分，负数按0。配置估计成本不是实际机构账单；成本仅来自加密通道costrate配置，不接受支付请求传入。仅原订单手续费出资。',{enum:['fee','gross','profit']});
  schemas.ReferralPolicy.properties.basis=basis;
  schemas.ReferralDailyPolicyView.properties.basis=basis;
  schemas.ReferralDailyPolicyView.properties.profit_basis_available={type:'boolean',const:true};
  schemas.ReferralDailyPolicyView.properties.membership_referral_policy_available={type:'boolean',const:true};
  schemas.ReferralDailyPolicyView.required.push('profit_basis_available','membership_referral_policy_available');
  policy.get.description+='\nprofit_basis_available严格为true才允许profit；会员推广政策独立，不属于普通订单政策。';
  policy.post.description+='\n支持profit即时或次日发奖：原手续费F减按原订单金额和costrate计算的估计成本，再对差额half-up到分，负利润为0。costrate是百分比十进制配置，最多六位小数、范围0..100；缺失和明确0不同。启用的profit奖励即使零手续费/零比例/零奖励也必须有原可信成本、原政策及支付身份的独立证明。明确关闭或推广者不可用不要求成本、不阻断正常付款。不能用当前配置回填历史或以缺marker降级旧家族。';
  paths['/console/v1/merchant/referral/daily-queue'].get.description+='\n已支持原profit固定条款；队列source_version仍为2，与独立利润成本来源家族不同。';
  const counter=str('规范非负十进制字符串，最大int64。',{pattern:'^(0|[1-9][0-9]{0,18})$'});
  const fields={enabled:{type:['boolean','null'],description:'global必须显式bool；group null继承，false明确关闭。'},rate_bps:{type:['integer','null'],minimum:0,maximum:10000,description:'global必须整数；group null继承，0明确覆盖。'}};
  schemas.MembershipReferralPolicyWrite=object({expected:counter,...fields},['expected','enabled','rate_bps']);
  schemas.MembershipReferralPolicyView=object({id:counter,scope:str(null,{enum:['global','group']}),scope_id:counter,revision:counter,...fields,created_at:str(null,{format:'date-time'}),available:{type:'boolean',const:true}},['id','scope','scope_id','revision','enabled','rate_bps','available']);
  const envelope=object({code:{type:'integer',const:0},message:str(),data:ref('MembershipReferralPolicyView')},['code','message','data']);
  const path='/console/v1/admin/membership-referral-policies/{scope}/{scope_id}';
  const parameters=[{name:'scope',in:'path',required:true,schema:{type:'string',enum:['global','group']}},{name:'scope_id',in:'path',required:true,schema:counter}];
  const responses={'200':{description:'当前独立会员推广政策；默认关闭，组空项继承。',content:content(envelope)},'400':{description:'不规范scope/id、query或重复/未知JSON字段。'},'401':{description:'原会话失效。'},'403':{description:'必须本人super管理员及合法CSRF，委托会话拒绝。'},'409':{description:'原revision/用户组/政策快照已变；重新读取后显式核对。'},'422':{description:'必须准确三字段；global不能null；比例0..10000整数。'},'428':{description:'已启用管理员资金授权时缺少原独立政策操作授权。'}};
  paths[path]={get:{operationId:'membershipReferralPolicyRead',summary:'本人super读取独立会员推广政策',description:'global只能id0，group为正用户组ID，不允许query。三层继承全局→购买前付款商户有效组→推广商户有效组；null继承，false/0明确覆盖。',security:[{AdminSession:[]}],parameters,responses},post:{operationId:'membershipReferralPolicyWrite',summary:'本人super以CAS及独立资金授权修改会员推广政策',description:'仅余额会员购买；默认关闭，新购买生效，旧购买不追发。只有expected/enabled/rate_bps三字段，拒重复、未知和客户端奖励金额。原购买价P从buyer.available进入本purchase专属membership_income，再同事务支付R给原推广人；R按P乘比例half-up到分且R<=P，不能借普通手续费、clearing池或其它购买。余额扣款、权益、源事实、奖励和审计原子提交。原购买/关系/有效组/政策固定，不以当前政策重放奖励。外部tid4购买未开放。',security:[{AdminSession:[]}],parameters:[...parameters,{name:'X-CSRF-Token',in:'header',required:true,schema:{type:'string',writeOnly:true}},{name:'X-Payment-Authorization',in:'header',required:false,schema:{type:'string',writeOnly:true},description:'独立purpose membership-referral-policy，绑定原scope:id:expected和完整三字段，不能复用普通订单推荐政策授权。'}],requestBody:{required:true,content:content(ref('MembershipReferralPolicyWrite'))},responses}};
}

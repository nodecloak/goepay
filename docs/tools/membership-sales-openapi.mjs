export function installMembershipSalesAPI({add,paths,schemas,object,str,ref,content}) {
 const input={id:str('用户组ID',{pattern:'^[1-9][0-9]*$'}),sale_enabled:{type:'boolean'},price_cents:str('每份售价，整数分；0允许免费。',{pattern:'^(0|[1-9][0-9]{0,11})$'}),duration_months:{type:['integer','null'],minimum:0,maximum:1200,description:'0为永久，正数为日历月；null保留原按天配置。'},sort:{type:'integer',minimum:0,maximum:1000000},visible:str('英文逗号分隔现有用户组ID；0为默认组，空为全部。'),description:str('纯文本简介，最多2000字符。',{maxLength:2000})};
 schemas.MembershipSaleGroupInput=object(input,Object.keys(input));
 schemas.MembershipSalesWrite=object({expected_revision:str('上次读取的完整销售配置SHA256；改变返回409，先刷新。',{pattern:'^[a-f0-9]{64}$'}),enabled:{type:'boolean'},groups:{type:'array',maxItems:1000,items:ref('MembershipSaleGroupInput')}},['expected_revision','enabled','groups']);
 schemas.MembershipBalancePurchase=object({merchant_id:str('仅管理员指定；商户由会话确定。',{pattern:'^[1-9][0-9]*$'}),group_id:input.id,external_no:str('稳定幂等业务号，同号改变用户组或数量返回409。',{minLength:1,maxLength:128}),quantity:{type:'integer',minimum:1,maximum:300,default:1}},['group_id','external_no']);
 add('/membership-sales','get','读取管理员销售配置或商户可购买目录',{description:'管理员data包含enabled/revision/groups，groups含组名、启用状态、每份价格、日历月或旧按天期限、上下架、排序、可见组和简介。商户data包含enabled/items，仅返回当前有效用户组可见且已上架的公开报价；关闭时items为空。不调用支付机构。'});
 add('/membership-sales','post','本人super管理员原子保存用户组销售配置',{body:'MembershipSalesWrite',description:'全局开关与groups所有更新、configure审计同事务；版本过期409，未知结果请刷新核对，不自动重试。groups为空仅改全局；支持单组上下架或简介，不覆盖其它组。保留基础费率和名称等无关字段。通用settings接口不能更新/删除group_buy绕过本接口。'});
 const post=paths['/console/v1/{role}/membership-sales'].post;
 post.parameters=post.parameters.filter(p=>p.name!=='role');post.security=[{AdminSession:[]}];
 paths['/console/v1/admin/membership-sales']={post};delete paths['/console/v1/{role}/membership-sales'].post;
 add('/workflows/memberships','get','读取本角色会员购买记录',{list:true,description:'金额和ID为字符串。新购买另返回quantity、unit_price_cents、period_months、permanent；旧无条款购买保留原记录。永久用明确permanent=true显示，不把9999日期当有限会员期限。'});
 add('/workflows/memberships','post','按真实销售配置和数量购买会员',{body:'MembershipBalancePurchase',created:true,description:'整数分总价=每份价格×数量；启用、上下架、可见范围和当前有效组由服务端重检。显式月份按PHP日历月溢出和北京时间日期零点续期，0月永久，旧按天配置仍按天。同组未到期从原到期日续；永久同组不可重复购买。同号同体重放不再扣费。原奖励仅来自本次总价收入，数量、期限、配置与到期快照均绑定私有原资金证明。没有上游支付或真实资金请求。'});
 const auth=paths['/console/v1/{role}/workflows/{kind}'].post.parameters.find(p=>p.name==='X-Payment-Authorization');
 if(auth)paths['/console/v1/{role}/workflows/memberships'].post.parameters.push(structuredClone(auth));
 const complaints=paths['/console/v1/{role}/reports/complaints'].get;
 complaints.parameters.push({name:'view',in:'query',schema:{type:'string',enum:['overview']},description:'仅管理员；overview为今日、昨日和最近7个北京时间业务日，不能同时传from/to。data.source=local_registered_complaints，包含summary及按商户排行items；金额为十进制字符串，未登记金额为null，不能显示0元；近7日投诉数/同期已支付订单数百分比，无支付订单时null。CSV使用同一汇总口径，包含今日/昨日总条数、金额、支付宝/微信条数；不包含支付净额或手续费列。'});
}

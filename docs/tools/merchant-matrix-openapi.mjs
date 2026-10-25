export function installMerchantMatrixAPI({paths,schemas,object,str,ref}) {
 schemas.MerchantMatrixResult=object({
  columns:{type:'array',maxItems:130,items:object({key:str(),label:str()},['key','label'])},
  items:{type:'array',maxItems:100,items:{type:'object',additionalProperties:{type:['string','null']}}},
  summary:{type:'object',description:'全部筛选结果总计，与分页无关；uid=总计，其余为精确元字符串或未知null。',additionalProperties:{type:['string','null']}},
  total:{type:'integer',minimum:0,description:'符合条件的商户数，不包含总计行。'},
  page:{type:'integer'},limit:{type:'integer'},method:str(null,{enum:['type','channel']}),metric:str(null,{enum:['0','1','2','3']}),
  missing_costs:str('缺少成本快照的付款数；利润的相关单元、行总计及平台总计传播为null。'),source:str(),timezone:{const:'Asia/Shanghai'},from:str(null,{format:'date'}),to:str(null,{format:'date'})
 },['columns','items','summary','total','page','limit','method','metric','missing_costs','source','timezone','from','to']);
 schemas.MerchantMatrixEnvelope=object({code:{const:0},message:str(),data:ref('MerchantMatrixResult')},['code','message','data']);
 for(const name of ['merchants','transfer-merchants']) {
  const op=paths[`/console/v1/{role}/reports/${name}`].get;
  op.description+='\n\n显式view=matrix恢复原商户行、总计及方式/通道动态列，返回MerchantMatrixResult；无view保持旧六列表。仅管理员，拒重复/未知参数；matrix不接受通用q或商户身份选择器。日期默认今天、最多367天。交易仅原paid订单、paid_at支付日；type0订单金额、1当前固定收单金额（现模型与0相同）、2原商户净额、3手续费减原成本快照估算（非机构对账利润）。付款仅succeeded、原resolved_at，不含已退/待核实退回，type仅0；方式从原AEAD意图验证，仅返回类型不返回收款资料，历史无意图为未知。全部分/聚合用numeric或big.Int，金额精确字符串。总计覆盖全部筛选而非本页；排序在全部商户上进行、未知始终最后。事实维度至多128，支付至多50000组、付款至多50000源记录，超限422要求缩小日期，不截断。JSON无业务写入；CSV同一矩阵/完整商户含总计、上限50000行/250000单元/8MiB、并发2/20秒，原导出审计成功后才交付。';
  op.parameters.push(...[
   ['view',{type:'string',enum:['matrix']}],['method',{type:'string',enum:['type','channel'],default:'type'}],
   ['type',{type:'string',enum:name==='merchants'?['0','1','2','3']:['0'],default:'0'}],
   ['sort',{type:'string',maxLength:64,default:'total',description:'uid、total或当前真实返回的动态列key；不能传SQL表达式。'}],
   ['direction',{type:'string',enum:['asc','desc'],default:'desc'}],
  ].map(([name,schema])=>({name,in:'query',schema,description:'仅view=matrix。'})));
 }
}

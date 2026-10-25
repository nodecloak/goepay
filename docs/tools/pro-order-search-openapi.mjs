export function installProOrderSearchAPI({paths}) {
  const op=paths['/console/v1/{role}/orders'].get;
  if(op.parameters.some(x=>['search_field','notification_state'].includes(x.name)))throw Error('Duplicate order-search parameters');
  const query=op.parameters.find(x=>x.name==='q'&&x.in==='query');
  query.description='最多256个UTF-8字节；省略search_field保持系统/商户单号和商品综合字面子串搜索。指定字段时按search_field约定匹配；金额使用元十进制字符串，精确转换整数分。重复q/字段、非法金额或地址返回400；空q不启用所选字段。';
  query.schema.maxLength=256;
  op.parameters.push({name:'search_field',in:'query',schema:{type:'string',enum:['trade_no','out_trade_no','api_trade_no','name','money','realmoney','getmoney','buyer','ip','param']},description:'仅订单支持。单号/买家/IP精确匹配；商品/扩展参数大小写不敏感字面子串，无SQL通配符。money/realmoney对应原订单amount；getmoney对应商户净额。接口单号和买家与显示投影同源。没有可信来源的手机号、域名、渠道子单字段不支持。CSV与JSON使用同一筛选及商户隔离。'});
  op.parameters.push({name:'notification_state',in:'query',schema:{type:'string',enum:['none','pending','running','succeeded','dead']},description:'仅订单支持，按最新payment.notify任务state筛选，none为尚无任务；不从订单付款状态推断通知结果。'});
}

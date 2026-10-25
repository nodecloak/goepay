export function installCollectionGeneratorAPI({add,paths,schemas,object,str}) {
 schemas.CollectionGeneratorWrite=object({external_no:str('本人商户内稳定原请求号；未知结果保留原号和原参数，重试不会重新启用已停用码。',{pattern:'^[A-Za-z0-9_-]{8,64}$'}),recipient_name:str('收款方名称，1–100字。',{minLength:1,maxLength:100}),style_id:str('启用样式ID，0为普通二维码。',{pattern:'^(0|[1-9][0-9]*)$'}),origin:str('必须精确匹配GET返回的部署收款origin，不接受任意跳转域名。'),amount:str('固定元金额，空字符串表示付款人输入；固定金额范围0.01–1000000，最多两位小数。',{pattern:'^(?:|[0-9]+(?:\\.[0-9]{1,2})?)$'})},['external_no','recipient_name','style_id','origin','amount']);
 add('/collection-generator','get','读取本人聚合收款生成页上下文',{description:'只读，返回recipient_name、styles、domains和latest。domains仅列出部署明确支持的收款origin；不能把商户任意审核域名当作支付站点。latest包括停用状态，不自动新建或重新启用。'});
 add('/collection-generator','post','本人商户明确生成长期聚合收款码',{body:'CollectionGeneratorWrite',created:true,description:'不创建支付订单。配置/请求摘要/私有证明与审计同事务；同参数重试200返回原码，换参数409。返回code_id、recipient_name、origin、style_id、fixed_amount_cents、config_hash、enabled、replayed、url、expires_at=null；私有proof_hash不暴露。固定金额在公开页和下单事务内复验。公开签名mode=generated绑定配置hash，长期有效、停用即撤销。旧记录两小时链接保持兼容。'});
 const from='/console/v1/{role}/collection-generator',to='/console/v1/merchant/collection-generator';paths[to]={};
 for(const method of ['get','post']) {const op=paths[from][method];op.parameters=op.parameters.filter(p=>p.name!=='role');op.security=[{MerchantSession:[]}];paths[to][method]=op;}
 delete paths[from];
 const link=paths['/console/v1/{role}/collection-codes/{id}/link'].get;link.summary='读取聚合收款链接及海报布局';link.description+=' 新生成配置返回长期签名链接，expires_at=null；停用/归档保留来源证明但撤销公开收款。';
}

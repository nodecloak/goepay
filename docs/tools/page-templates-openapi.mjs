export function installPageTemplateAPI({paths,schemas,object,str,ref,content}) {
 const kind=str(null,{enum:['home','pay','qrpay']}),name=str(null,{enum:['default','compact']}),revision=str('同类型配置保存与模板切换共享的CAS修订；首次0。',{pattern:'^(0|[1-9][0-9]*)$'}),asset=str('规范化本地图片SHA256，不接受路径或外部URL。',{pattern:'^[0-9a-f]{64}$'});
 const common={brand_name:str(null,{minLength:1,maxLength:64}),notice:str(null,{maxLength:240}),accent_color:str(null,{pattern:'^#[0-9a-fA-F]{6}$'}),background_color:str(null,{pattern:'^#[0-9a-fA-F]{6}$'}),logo_asset_id:str('空值清除标志；否则必须已上传的本地图片ID。',{pattern:'^(|[0-9a-f]{64})$'}),density:str(null,{enum:['comfortable','compact']})};
 schemas.PageTemplateConfig=object(common,Object.keys(common));
 schemas.PayPageTemplateConfig=object({...common,show_order_name:{type:'boolean'}},[...Object.keys(common),'show_order_name']);
 schemas.PageTemplateSave=object({expected_revision:revision,config:{oneOf:[ref('PageTemplateConfig'),ref('PayPageTemplateConfig')]}},['expected_revision','config']);
 schemas.PageTemplateSelection=object({expected_revision:revision,name},['expected_revision','name']);
 schemas.PageTemplateAsset=object({id:asset,mime:str(null,{enum:['image/png','image/jpeg']}),width:{type:'integer',minimum:1,maximum:2048},height:{type:'integer',minimum:1,maximum:2048}},['id','mime','width','height']);
 schemas.PageTemplateField=object({name:str(),label:str(),type:str(null,{enum:['text','textarea','color','image','select','switch']}),required:{type:'boolean'},max_length:{type:'integer'},options:{type:'array',items:object({label:str(),value:str()},['label','value'])}},['name','label','type','required']);
 schemas.PageTemplateConfigView=object({type:kind,name,revision,config:{oneOf:[ref('PageTemplateConfig'),ref('PayPageTemplateConfig')]},fields:{type:'array',items:ref('PageTemplateField')},fallback:str('仅外观配置无效时的固定原因；支付身份校验不受fallback替代。')},['type','name','revision','config','fields']);
 schemas.PageTemplateList=object({type:kind,revision,selected:str(),items:{type:'array',minItems:2,maxItems:2,items:object({type:kind,name,title:str(),author:str(),version:str(),description:str(),preview_url:str(),has_config:{type:'boolean'}},['type','name','title','author','version','description','preview_url','has_config'])}},['type','revision','selected','items']);
 const param=(n,s,inPath=true)=>({name:n,in:inPath?'path':'query',required:true,schema:s});
 const templateParams=[param('type',kind),param('name',name)];
 const jsonResponse=(schema,description='成功时code=0，data为下列类型。')=>({description,content:content(object({code:{type:'integer'},message:str(),data:schema},['code','message','data']))});
 const errors={400:jsonResponse({},'拒绝重复键、未知字段、非法query或格式。'),401:jsonResponse({},'未登录。'),403:jsonResponse({},'非本人super或CSRF拒绝。'),404:jsonResponse({},'模板或当前可见图片不存在。'),409:jsonResponse({},'同类型修订冲突；重新读取原配置，禁止自动覆盖。'),413:jsonResponse({},'输入图片或请求超限。'),422:jsonResponse({},'非法类型化配置或PNG/JPEG图片。'),429:jsonResponse({},'上传限速或解码并发受限。'),503:jsonResponse({},'本地依赖不可用。')};
 let i=0;
 function admin(route,method,summary,{params=[],body,responseSchema={},description='',binary=false}={}){
  const operation={operationId:`pageTemplate${++i}`,summary,description:'仅本人super管理员，拒绝代登录；写入需CSRF。除目录type之外禁止query。'+description,security:[{AdminSession:[]}],parameters:[...params],responses:{200:binary?{description:'规范化图片；Cache-Control:no-store。',content:{'image/png':{schema:{type:'string',format:'binary'}},'image/jpeg':{schema:{type:'string',format:'binary'}}}}:jsonResponse(responseSchema),...errors}};
  if(!['get','head'].includes(method))operation.parameters.push({name:'X-CSRF-Token',in:'header',required:true,schema:str()});
  if(body)operation.requestBody={required:true,content:content(ref(body))};
  (paths[`/console/v1/admin${route}`]??={})[method]=operation;return operation;
 }
 admin('/templates','get','读取内置模板目录与当前选择',{params:[param('type',kind,false)],responseSchema:ref('PageTemplateList'),description:'每类仅default/compact白名单，不加载插件文件或执行原isShow表达式。'});
 admin('/templates/{type}/{name}/config','get','读取类型化外观设置与字段定义',{params:templateParams,responseSchema:ref('PageTemplateConfigView')});
 admin('/templates/{type}/{name}/config','put','CAS保存完整外观配置',{params:templateParams,body:'PageTemplateSave',responseSchema:ref('PageTemplateConfigView'),description:'pay必须含show_order_name；home/qrpay拒绝该字段。所有配置字段必须显式完整，未知/重复/null拒绝。仅当前选中的模板会影响公开页面，不改变订单、渠道、支付身份、签名或资金状态。'});
 admin('/templates/{type}/selection','put','CAS切换当前内置模板',{params:[param('type',kind)],body:'PageTemplateSelection',responseSchema:ref('PageTemplateList'),description:'保留各模板已保存草稿；全类型共享修订以避免保存与切换并发丢失。'});
 const upload=admin('/templates/images','post','上传并规范化私有PNG/JPEG',{responseSchema:ref('PageTemplateAsset'),description:'恰好一个file part；输入1..512KiB；边长<=2048且像素<=1048576；标准库完整解码再编码，去除metadata；禁止SVG/GIF/WebP/远程URL。图片仅当前选中的有效配置引用时公开，否则私有。上传不会自动保存模板。'});
 upload.requestBody={required:true,content:{'multipart/form-data':{schema:object({file:{type:'string',format:'binary'}},['file'])}}};upload.responses[201]=upload.responses[200];delete upload.responses[200];
 admin('/templates/images/{asset_id}','get','本人super读取私有图片',{params:[param('asset_id',asset)],binary:true});
 const preview=admin('/templates/{type}/{name}/preview','get','预览已保存模板与固定演示上下文',{params:templateParams,description:'无实际订单或渠道调用，脚本和表单禁止，CSP sandbox allow-same-origin仅供同源私有图片会话；不接受任意trade/URL。'});
 preview.responses[200]={description:'固定演示HTML，无实际支付动作。',content:{'text/html':{schema:str()}}};
 for(const method of ['get','head'])(paths['/template-assets/{asset_id}']??={})[method]={operationId:`publicPageTemplateAsset${method}`,summary:'读取当前已发布引用的规范化本地图片',security:[],parameters:[param('asset_id',asset)],responses:{200:{description:'固定MIME PNG/JPEG；no-store、nosniff、同源CORP。切换/清除引用后立即404。',content:{'image/png':{schema:{type:'string',format:'binary'}},'image/jpeg':{schema:{type:'string',format:'binary'}}}},404:{description:'未被当前有效模板引用，或ID无效。'},503:{description:'本地存储不可用。'}}};
 paths['/']={get:{operationId:'publicPaymentHome',summary:'API源首页使用当前内置home模板',description:'只提供支付提示，不发起支付；独立web源根路径仍为SPA。禁止query，no-store/CSP限制脚本与表单。',security:[],responses:{200:{description:'已验证类型化外观或可观测安全默认外观。',content:{'text/html':{schema:str()}}},503:{description:'存储不可用。'}}}};
}

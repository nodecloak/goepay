export function installCollectionStylesAPI({add,paths,schemas,object,str,content}){
 const integer=str('规范非负十进制整数字符串。',{pattern:'^(0|[1-9][0-9]*)$'}),revision=str('读取所得样式SHA256，过期409；先重新读取原记录。',{pattern:'^[a-f0-9]{64}$'});
 const fields={name:str('原风格标识，字母开头，仅字母数字下划线。',{pattern:'^[A-Za-z][A-Za-z0-9_]{0,99}$'}),label:str('原显示名称。',{maxLength:100}),enabled:{type:'boolean',description:'对应原status启用状态。'},sort:integer,url:str('本地/collection-style-assets/<SHA256>或无凭证的HTTPS背景，不由服务器抓取；浏览器预览及导出远程图片需要CORS。'),img_width:integer,img_height:integer,qr_width:integer,qr_height:integer,qr_left:integer,qr_top:integer,foreground:str('#RRGGBB'),background:str('#RRGGBB或transparent'),font:str('1–256px单一字体，不接受CSS表达式。'),font_color:str('#RRGGBB或transparent'),rec_name_left:str('空为居中，否则画布内整数字符串。'),rec_name_top:integer,word:{type:'string',enum:['0','1','2'],description:'隐藏、商户名、扫码向 商户 付款。'}};
 schemas.CollectionStyleWrite=object(fields,Object.keys(fields));schemas.CollectionStyleUpdate=object({expected_revision:revision,style:object(fields,[])},['expected_revision','style']);
 add('/collection-styles','get','本人读取收款码样式目录',{description:'管理员读取所有；商户仅读取启用样式。items包含id/revision/style/fields；style为实际消费的数字布局，旧简单颜色样式style=null。展示图片并不发起支付。'});
 add('/collection-styles','post','本人管理员创建收款海报布局',{body:'CollectionStyleWrite',created:true,description:'原name/label/尺寸/坐标/颜色/font/word字段落库并由实际PNG渲染消费。画布64–4096px/4MP，二维码至少32px且不得越界。启用并引用素材才公开背景；只保存配置不生成支付订单。'});
 add('/collection-styles/{id}','get','本人管理员读取原样式及版本');
 add('/collection-styles/{id}','put','本人管理员按原revision更新样式',{body:'CollectionStyleUpdate',description:'行锁下核对版本、校验布局/素材并与审计同事务。通用资源接口不能无版本覆盖新布局。旧简单颜色样式可显式升级，不能自动捏造背景和坐标。'});
 add('/collection-styles/{id}','delete','本人管理员明确确认后删除样式',{description:'保留不可变上传素材和审计，失去启用样式引用的背景不再公开；原收款码及订单保留并回退普通二维码。'});
 add('/collection-style-images','post','本人管理员上传并归一化私有背景',{description:'multipart单个file，PNG/JPEG最大1MiB、4MP、4096px边长；重新编码最多4MiB，拒SVG/脚本/异常图片。返回id/mime/width/height。上传与审计同事务，失败不保留素材。'});
 add('/collection-style-images/{id}','get','本人管理员预览未公开背景',{description:'仅私有会话，不向其它角色开放。无缓存。'});
 const upload=paths['/console/v1/{role}/collection-style-images'].post;upload.requestBody={required:true,content:{'multipart/form-data':{schema:object({file:{type:'string',format:'binary'}},['file'])}}};upload.responses['201']=upload.responses['200'];delete upload.responses['200'];
 for(const [suffix,methods]of[['/collection-styles',['post']],['/collection-styles/{id}',['get','put','delete']],['/collection-style-images',['post']],['/collection-style-images/{id}',['get']]]){
  const from='/console/v1/{role}'+suffix,to='/console/v1/admin'+suffix;paths[to]={};for(const method of methods){const op=paths[from][method];op.parameters=op.parameters.filter(p=>p.name!=='role');op.security=[{AdminSession:[]}];paths[to][method]=op;delete paths[from][method];}if(!Object.keys(paths[from]).length)delete paths[from];
 }
 const image={type:'string',pattern:'^[a-f0-9]{64}$'};
 paths['/console/v1/admin/collection-style-images/{id}'].get.parameters.find(p=>p.name==='id').schema=image;
 for(const method of['get','head']){paths['/collection-style-assets/{image_id}']??={};paths['/collection-style-assets/{image_id}'][method]={operationId:'collection_style_asset_'+method,summary:'仅读取当前启用样式实际引用的背景图',security:[],parameters:[{name:'image_id',in:'path',required:true,schema:image}],responses:{200:{description:'重新验证内容摘要/尺寸/MIME；no-store，nosniff；HEAD无正文。',content:{'image/png':{schema:{type:'string',format:'binary'}},'image/jpeg':{schema:{type:'string',format:'binary'}}}},404:{description:'未公开、已停用或不存在'}}};}
 paths['/console/v1/{role}/collection-codes/{id}/link'].get.description+=' data另含merchant_name和layout：新布局用于实际背景/QR/文字PNG；旧样式layout=null保留普通QR。签名链接有效期仍为两小时，不添加背景配置绕过收款权限。';
}

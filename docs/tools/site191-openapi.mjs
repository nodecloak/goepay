export function installSite191API({paths,schemas,object,str,ref,content}) {
  const keys=['sitename','title','keywords','description','orgname','email','kfqq','kfqq_url','wework_url','qqqun','login_subtitle','homepage','homepage_url'];
  const properties=Object.fromEntries(keys.map(key=>[key,str('原网站公开字段',{maxLength:1000})]));
  properties.sitename={...properties.sitename,minLength:1,maxLength:64};
  properties.homepage={type:'string',enum:['0','1','2','3'],description:'默认首页、商户中心、自定义框架、空白页面。'};
  schemas.PublicSite191=object(properties,keys);
  paths['/console/v1/public/site']={get:{operationId:'publicSite191Read',tags:['settings'],summary:'读取原网站公开信息',description:'只返回固定公开字段，全部no-store；拒绝query（包括空?）。sitename优先于兼容site_name；email已配置时优先于support_email（含空值清除）。无配置时网站名称GOEPAY。非法历史值或缺失自定义首页地址返回503，不公开其它设置或凭据。',security:[],responses:{'200':{description:'当前网站名称、SEO、联系、登录标语和首页字段。',content:content(object({code:{type:'integer',const:0},message:str(),data:ref('PublicSite191')},['code','message','data']))},'400':{description:'不接受查询参数。'},'405':{description:'只支持GET。'},'503':{description:'网站设置无法安全读取。'}}}};
}

export function installAnnouncementLayoutAPI({paths,schemas,object,str,ref,content}){
 const rich=str('原公告排版HTML，最多20000字节；空字符串关闭对应展示。客户端只渲染受控节点、属性和URL，旧纯文本公告不自动改为HTML。',{maxLength:20000});
 schemas.PublicAnnouncementLayout=object({zhuce:rich,footer:rich,format:{type:'string',const:'html'}},['zhuce','footer','format']);
 schemas.PublicAnnouncementLayoutEnvelope=object({code:{type:'integer',const:0},message:str(),data:ref('PublicAnnouncementLayout')},['code','message','data']);
 paths['/console/v1/public/layout']={get:{operationId:'publicAnnouncementLayout',tags:['Public'],summary:'读取原注册公告和页脚排版',description:'公开只读、no-store、拒绝所有query参数；不读取欢迎公告modal、支付配置或任何凭据。管理员在原settings资源的zhuce/footer保存后由对应实际页面消费；留空关闭。仅modal/zhuce/footer允许空值和20000字节，其它设置约束保持原状。',security:[],responses:{'200':{description:'公开两处排版位置。',content:content(ref('PublicAnnouncementLayoutEnvelope'))},'400':{description:'不接受query参数。'},'405':{description:'只允许GET，Allow: GET。'},'503':{description:'排版读取不可用。'}}}};
 schemas.DashboardAnnouncement=object({id:str('完整正int64十进制字符串。'),title:{type:['string','null'],description:'仅兼容原Go旧标题，新公告不额外要求标题。'},content:{type:['string','null']},color:str('空或#RRGGBB。'),content_format:{type:'string',enum:['text','html']},created_at:str('原公告创建时间。',{format:'date-time'})},['id','title','content','color','content_format','created_at']);
 schemas.DashboardOverview.properties.announcements={type:'array',maxItems:10,items:ref('DashboardAnnouncement')};
 schemas.DashboardOverview.properties.welcome_notice={type:['string','null'],description:'仅商户首页读取原modal；HTML格式；空关闭，管理员为null。每日本人显示偏好按完整会话商户ID和business_date隔离，代登录不能消费本人日期。',maxLength:20000};
 const op=paths['/console/v1/{role}/dashboard'].get;
 op.description=op.description.replace('公告按角色过滤仅10条纯文本','公告按角色过滤最多10条，按原sort升序/同sort实际数值ID降序；新增HTML有显式content_format，旧公告仍纯文本');
}

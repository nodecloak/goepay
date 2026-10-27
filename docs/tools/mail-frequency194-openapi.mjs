export function installMailFrequency194API({paths,schemas,object,str,ref,content}) {
 const keys=['mail_interval_email','mail_interval_ip','mail_daily_email','mail_daily_ip'];
 const fields=Object.fromEntries(keys.map(k=>[k,{...str(),pattern:'^[1-9][0-9]*$',description:k.includes('interval')?'秒，1至86400。':'次数，1至100000。'}]));
 schemas.MailFrequency194Values=object(fields,keys);
 schemas.MailFrequency194=object({...fields,effective:{...ref('MailFrequency194Values'),description:'实际执行限制。未明确保存接管时保留现有60秒/0秒/7次/11次；此时effective.mail_interval_ip允许字符串0。'},configured:{type:'boolean'},revision:{...str(),pattern:'^(0|[1-9][0-9]*)$'},fingerprint:{...str(),pattern:'^[a-f0-9]{64}$'}},[...keys,'effective','configured','revision','fingerprint']);
 // The legacy effective source-IP interval is intentionally zero; drafts and
 // managed policies only accept positive original four-field values.
 schemas.MailFrequency194.properties.effective=object({...fields,mail_interval_ip:{...str(),pattern:'^(0|[1-9][0-9]*)$'}},keys);
 schemas.MailFrequency194Write=object({...fields,expected_fingerprint:{...str(),pattern:'^[a-f0-9]{64}$'}},[...keys,'expected_fingerprint']);
 const responses={'200':{description:'受保护的当前频率；no-store。只更改限制，保存不发送邮件、不清空计数或验证码。',content:content(object({code:{type:'integer',const:0},message:str(),data:ref('MailFrequency194')},['code','message','data']))},'400':{description:'不接受query、额外或重复JSON字段。'},'403':{description:'本人超级管理员会话和写入CSRF校验失败。'},'409':{description:'确认版本已变化，重新读取。'},'422':{description:'四项均要求有界规范正整数字符串。'},'503':{description:'当前配置或独立见证完整性无法确认。'}};
 const base={tags:['settings'],security:[{AdminSession:[]}],parameters:[],responses};
 paths['/console/v1/admin/settings/mail-frequency']={get:{...base,operationId:'mailFrequency194Read',summary:'读取原四项邮件发送频率及实际执行限制'},patch:{...base,operationId:'mailFrequency194Save',summary:'确认保存原四项邮件发送频率',description:'不可变策略与独立见证、CAS及末次写入后的会话重检。注册/找回在同一预算事务中读取；失败和拒绝尝试计数，配置代次不创建新计数。响应丢失后重新读取，不自动重试保存。',parameters:[{in:'header',name:'X-CSRF-Token',required:true,schema:str()}],requestBody:{required:true,content:content(ref('MailFrequency194Write'))}}};
}

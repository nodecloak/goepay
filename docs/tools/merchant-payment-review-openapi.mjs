export function installMerchantPaymentReviewAPI({ paths, schemas, object, str, id, ref, content }) {
  schemas.MerchantPaymentStatus = str('原merchants.pay收款状态；0禁止收款、1正常、2待审核。独立于账户enabled，普通profile同名字段没有付款权限。', { enum: ['0','1','2'], readOnly: true });
  schemas.PublicEnrollmentCapabilities = object({ registration_open: {type:'boolean'}, email_enabled: {type:'boolean'}, invite_required: {type:'boolean'}, review_required: {type:'boolean',description:'原user_review=1；只影响之后成功注册的新商户。'}, expires_seconds: {type:'integer',minimum:1} }, ['registration_open','email_enabled','invite_required','review_required','expires_seconds']);
  schemas.RegistrationResult = object({ merchant_id: id, username: str(), pay: ref('MerchantPaymentStatus') }, ['merchant_id','username','pay']);
  schemas.MerchantProfileView = object({name:str(),email:str(),pay:ref('MerchantPaymentStatus')},['name','email','pay']);
  const envelope = name => object({code:{type:'integer',const:0},message:str(),data:ref(name)},['code','message','data']);
  const capabilities=paths['/console/v1/public/capabilities'].get;
  capabilities.description='未配置邮件仍返回真实能力；review_required表示之后注册将等待审核。非法或重复user_review配置拒绝提供可用能力。';
  capabilities.responses['200']={description:'当前公开注册能力。',content:content(envelope('PublicEnrollmentCapabilities'))};
  const registration=paths['/console/v1/public/register'].post;
  registration.description+=' 原user_review=1时同事务创建pay=2、仍可登录的零余额商户；结果pay是字符串。客户端不能提交pay或enabled决定审批。不自动建立会话，不回显APIKey。';
  registration.responses['201']={description:'已注册，pay=2仍须管理员审核收款权限。',content:content(envelope('RegistrationResult'))};
  const profile=paths['/console/v1/{role}/account/profile'];
  profile.get.description='仅当前merchant本人；返回原pay只读状态，不使用普通profile的同名字段。待审核登录仍有效。';
  profile.get.responses['200']={description:'当前商户资料与收款状态。',content:content(envelope('MerchantProfileView'))};
  profile.patch.description='仅修改本人name/email。pay等未知字段400拒绝；管理员审核另走原merchants资源权限、CSRF与配置CAS事务。';
  if(schemas.Profile.properties.pay || schemas.Registration.properties.pay || schemas.Profile.additionalProperties!==false || schemas.Registration.additionalProperties!==false) throw new Error('Enrollment/profile must not accept client payment admission');
}

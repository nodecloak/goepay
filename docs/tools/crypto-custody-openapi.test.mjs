import assert from 'node:assert/strict';
import {installCryptoCustodyAPI} from './crypto-custody-openapi.mjs';

const object=(properties,required=[],additionalProperties=false)=>({type:'object',properties,required,additionalProperties});
const str=(description,extra={})=>({type:'string',...(description?{description}:{}),...extra});
const ref=name=>({$ref:`#/components/schemas/${name}`});
const content=schema=>({'application/json':{schema}});
const paths={'/existing':{get:{operationId:'preserved',parameters:[]}}};
const schemas={Envelope:object({code:{type:'integer'},message:str(),data:{}},['code','message','data']),Existing:object({value:str()})};
schemas.RuntimeParentChannel={oneOf:[ref('Existing')]};
const before=structuredClone({paths,schemas});
installCryptoCustodyAPI({paths,schemas,object,str,ref,content});

for(const role of ['admin','merchant']){
  const base=`/console/v1/${role}/crypto`;
  for(const route of ['/config','/channels','/balances','/invoices','/invoices/{invoice_id}','/withdrawals','/withdrawals/by-request','/recipients'])assert.ok(paths[base+route]?.get,`missing GET ${base+route}`);
  for(const route of ['/invoices','/withdrawals','/refunds','/held-refunds','/recipients','/withdrawals/{withdrawal_id}/cancel','/recipients/{recipient_id}/revoke'])assert.ok(paths[base+route]?.post,`missing POST ${base+route}`);
  for(const [route,methods] of Object.entries(paths).filter(([route])=>route.startsWith(base)))for(const [method,op] of Object.entries(methods)){
    assert.deepEqual(op.security,[{[role==='admin'?'AdminSession':'MerchantSession']:[]}]);
    if(method==='post')assert.ok(op.parameters.some(p=>p.name==='X-CSRF-Token'&&p.in==='header'&&p.required),`missing CSRF ${route}`);
  }
  assert.ok(paths[base+'/withdrawals/by-request'].get.parameters.some(p=>p.name==='request_id'&&p.required));
}
for(const route of ['/reconciliation','/addresses','/withdrawals/{withdrawal_id}/approve','/recipients/{recipient_id}/approve','/sweeps/retry']){
  assert.ok(paths['/console/v1/admin/crypto'+route]);
  assert.equal(paths['/console/v1/merchant/crypto'+route],undefined,`invented merchant authority ${route}`);
}
for(const suffix of ['','/status','/qr']){
  const op=paths['/crypto/checkout/{invoice_id}'+suffix]?.get;
  assert.ok(op,`missing token checkout ${suffix}`);
  assert.deepEqual(op.security,[]);
  assert.ok(op.parameters.some(p=>p.name==='token'&&p.in==='query'&&p.required));
}
assert.equal(paths['/console/v1/admin/crypto/withdrawals/{withdrawal_id}'],undefined,'invented withdrawal detail route');
assert.equal(schemas.CryptoAmount.type,'string');
for(const amount of ['1','0.000001','10.123456'])assert.match(amount,new RegExp(schemas.CryptoAmount.pattern));
for(const amount of ['1.0000001','1e6','-1','+1'])assert.doesNotMatch(amount,new RegExp(schemas.CryptoAmount.pattern));
for(const name of ['CryptoInvoiceInput','CryptoWithdrawalInput','CryptoRefundInput','CryptoRecipientInput','CryptoOwnerPasswordInput']){
  const field=schemas[name].properties.merchant_id;
  const resolved=field.$ref?schemas[field.$ref.split('/').at(-1)]:field;
  assert.equal(resolved.type,'string',name);
}
for(const name of ['CryptoWithdrawalInput','CryptoRefundInput','CryptoRecipientInput','CryptoOwnerPasswordInput','CryptoPasswordInput','CryptoSweepRetryInput']){
  assert.ok(schemas[name].required.includes('current_password'),name);
  assert.equal(schemas[name].properties.current_password.writeOnly,true);
}
assert.equal(schemas.CryptoInvoiceInput.required.includes('current_password'),false);
assert.equal(schemas.CryptoInvoiceInput.properties.channel_id.$ref,'#/components/schemas/CryptoChannelID');
for(const field of ['channel_id','channel_revision','fee_bps','fee_source','channel_snapshot'])assert.ok(schemas.CryptoInvoiceView.required.includes(field),field);
for(const route of ['/console/v1/admin/payment-channels/crypto','/console/v1/admin/payment-channels/crypto/{channel_id}']){
  assert.ok(paths[route]?.get);
  for(const method of ['post','patch'])if(paths[route][method]){
    assert.deepEqual(paths[route][method].security,[{AdminSession:[]}]);
    assert.ok(paths[route][method].parameters.some(p=>p.name==='X-CSRF-Token'&&p.required));
  }
}
assert.equal(paths['/console/v1/merchant/payment-channels/crypto'],undefined);
assert.equal(schemas.CryptoChannelChoice.properties.merchant_ids,undefined);
for(const name of ['CryptoChannelView','CryptoChannelCreate','CryptoChannelPatch']){
  assert.equal(schemas[name].properties.merchant_ids,undefined,`${name} must not expose an inverse merchant assignment list`);
  assert.equal(schemas[name].required.includes('merchant_ids'),false);
  assert.equal(schemas[name].additionalProperties,false);
}
assert.ok(schemas.CryptoChannelView.required.includes('runtime_enabled'));
assert.ok(schemas.CryptoChannelCreate.required.includes('request_id'));
assert.ok(schemas.CryptoChannelPatch.required.includes('expected_revision'));
assert.equal(schemas.CryptoChannelPatch.properties.private_key,undefined);
assert.deepEqual(schemas.RuntimeParentChannel.oneOf,[ref('Existing'),ref('CryptoChannelView')]);
assert.equal(schemas.CryptoConfigView.properties.configured?.type,'boolean');
assert.ok(schemas.CryptoConfigView.required.includes('configured'));
for(const role of ['admin','merchant']){
  const op=paths[`/console/v1/${role}/crypto/config`].get;
  assert.match(op.description,/configured=false/);
  assert.match(op.description,/configured=true/);
  assert.match(op.description,/历史/);
}
assert.ok(schemas.CryptoWithdrawalView.required.includes('cancellation_requested'));
assert.equal(schemas.CryptoWithdrawalView.properties.source,undefined,'wallet source is not a console response field');

const ids=new Set();
for(const [route,methods] of Object.entries(paths))for(const op of Object.values(methods)){
  assert.ok(!ids.has(op.operationId),`duplicate operation ${op.operationId}`);ids.add(op.operationId);
  for(const [,name] of route.matchAll(/\{([^}]+)\}/g))assert.ok(op.parameters.some(p=>p.name===name&&p.in==='path'&&p.required),`${route}: ${name}`);
}
function refs(value){if(!value||typeof value!=='object')return;if(value.$ref)assert.ok(schemas[value.$ref.split('/').at(-1)],`unresolved ${value.$ref}`);for(const child of Object.values(value))refs(child)}
refs({paths,schemas});
assert.deepEqual(paths['/existing'],before.paths['/existing']);assert.deepEqual(schemas.Existing,before.schemas.Existing);
const once=structuredClone({paths,schemas});installCryptoCustodyAPI({paths,schemas,object,str,ref,content});assert.deepEqual({paths,schemas},once);
console.log(JSON.stringify({crypto_routes:true,role_security:true,token_checkout:true,string_money:true,resolved_refs:true,idempotent:true}));

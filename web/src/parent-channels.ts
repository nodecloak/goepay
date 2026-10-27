import {channelRetainedRate} from './runtime-channel-policy';
import {readParentWeixinBindingState,type ParentWeixinBindingState} from './parent-channel-bindings';
export interface ParentField {name:string;namespace:'config'|'certificates';secret:boolean;pem:boolean;present:boolean;value?:string}
export interface ParentChannel {id:string;name:string;pay_type:'wxpay'|'alipay';plugin:'wxpay'|'alipay';configured:boolean;source_channel_id?:string;revision?:string;account_version?:string;enabled?:boolean;archived?:boolean;mode?:0;rate?:string|null;fee_rate_bps?:number|null;fingerprint?:string;account?:{plugin:'wxpay'|'alipay';app_types:string[];mode:0;fields:ParentField[];appwxmp?:string;appwxa?:string;weixin_binding_state?:ParentWeixinBindingState};created_at?:string;updated_at?:string}
export interface ParentTemplate {source_channel_id:string;name:string;pay_type:'wxpay'|'alipay';fields:Omit<ParentField,'present'|'value'>[];fingerprint:string;merchant_count:number}
const invalid=()=>new Error('父通道资料无法核实，请刷新后重新确认');
function object(v:unknown):Record<string,unknown>{if(!v||typeof v!=='object'||Array.isArray(v))throw invalid();return v as Record<string,unknown>}
function exact(v:Record<string,unknown>,required:string[],optional:string[]=[]){if(required.some(k=>!Object.hasOwn(v,k))||Object.keys(v).some(k=>!required.includes(k)&&!optional.includes(k)))throw invalid()}
function id(v:unknown):v is string{return typeof v==='string'&&/^[1-9]\d{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n}
function text(v:unknown,max=180):v is string{return typeof v==='string'&&v.length>0&&v.length<=max&&!/[\x00-\x1f]/.test(v)}
function fp(v:unknown):v is string{return typeof v==='string'&&/^[a-f0-9]{64}$/.test(v)}
function field(raw:unknown,template=false):ParentField{
 const v=object(raw);exact(v,['name','namespace','secret','pem',...(template?[]:['present'])],template?[]:['value']);
 if(!text(v.name)||!['config','certificates'].includes(String(v.namespace))||typeof v.secret!=='boolean'||typeof v.pem!=='boolean'||!template&&typeof v.present!=='boolean'||v.value!==undefined&&(typeof v.value!=='string'||v.value.length>131072||v.secret))throw invalid();
 return v as unknown as ParentField;
}
export function parentChannelPath(value:string){if(!id(value))throw invalid();return '/payment-channels/'+value}
export function readParentChannel(raw:unknown):ParentChannel{
 const v=object(raw);const basic=['id','name','pay_type','plugin','configured'];
 if(!id(v.id)||!text(v.name,120)||!['wxpay','alipay'].includes(String(v.pay_type))||v.plugin!==v.pay_type||typeof v.configured!=='boolean')throw invalid();
 if(!v.configured){exact(v,basic);return v as unknown as ParentChannel}
 exact(v,[...basic,'source_channel_id','revision','account_version','enabled','archived','mode','rate','fee_rate_bps','fingerprint','account','created_at','updated_at']);
 if(!id(v.source_channel_id)||!id(v.revision)||!id(v.account_version)||typeof v.enabled!=='boolean'||typeof v.archived!=='boolean'||v.mode!==0||!fp(v.fingerprint)||typeof v.created_at!=='string'||!Number.isFinite(Date.parse(v.created_at))||typeof v.updated_at!=='string'||!Number.isFinite(Date.parse(v.updated_at)))throw invalid();
 if(v.rate===null){if(v.fee_rate_bps!==null)throw invalid()}else{if(typeof v.rate!=='string')throw invalid();const rate=channelRetainedRate(v.rate);if(rate.rate!==v.rate||rate.feeBPS!==v.fee_rate_bps)throw invalid()}
 const a=object(v.account);exact(a,['plugin','app_types','mode','fields'],['appwxmp','appwxa','weixin_binding_state']);if(a.plugin!==v.plugin||a.mode!==0||!Array.isArray(a.app_types)||a.app_types.length>16||a.app_types.some(x=>!text(x,32))||new Set(a.app_types).size!==a.app_types.length||!Array.isArray(a.fields)||a.fields.length>100)throw invalid();
 for(const key of ['appwxmp','appwxa'])if(a[key]!==undefined&&(a.plugin!=='wxpay'||a[key]!=='0'&&!id(a[key])))throw invalid();
 if(a.weixin_binding_state!==undefined){if(a.plugin!=='wxpay')throw invalid();readParentWeixinBindingState(a.weixin_binding_state,a.appwxmp,a.appwxa)}
 const seen=new Set<string>();for(const raw of a.fields){const f=field(raw);const key=f.namespace+'.'+f.name;if(seen.has(key))throw invalid();seen.add(key)}
 return v as unknown as ParentChannel;
}
export function readParentChannelList(raw:unknown):{items:ParentChannel[];total:number;page:number;size:number}{
 const v=object(raw);exact(v,['items','total','page','size']);if(!Array.isArray(v.items)||!Number.isSafeInteger(v.total)||Number(v.total)<v.items.length||!Number.isSafeInteger(v.page)||Number(v.page)<1||!Number.isSafeInteger(v.size)||Number(v.size)<1||Number(v.size)>100||v.items.length>Number(v.size))throw invalid();
 const items=v.items.map(readParentChannel);if(new Set(items.map(x=>x.id)).size!==items.length)throw invalid();return {...v,items} as {items:ParentChannel[];total:number;page:number;size:number};
}
export function readParentTemplates(raw:unknown):ParentTemplate[]{
 const v=object(raw);exact(v,['items','total']);if(!Array.isArray(v.items)||v.total!==v.items.length)throw invalid();
 const items=v.items.map(raw=>{const t=object(raw);exact(t,['source_channel_id','name','pay_type','fields','fingerprint','merchant_count']);if(!id(t.source_channel_id)||!text(t.name)||!['wxpay','alipay'].includes(String(t.pay_type))||!fp(t.fingerprint)||!Number.isSafeInteger(t.merchant_count)||Number(t.merchant_count)<0||!Array.isArray(t.fields))throw invalid();t.fields.forEach(f=>field(f,true));return t as unknown as ParentTemplate});
 if(new Set(items.map(x=>x.source_channel_id)).size!==items.length)throw invalid();return items;
}

export interface ParentDay {day:string;realmoney:string;refundmoney:string;profitmoney:null;orderCount:string;successCount:string;successRate:string|null}
export function readParentStatistics(raw:unknown,channel:string):{channel_id:string;items:ParentDay[];cost_basis:'unknown';money_basis:string}{
 const v=object(raw);exact(v,['channel_id','items','cost_basis','money_basis']);if(!id(channel)||v.channel_id!==channel||v.cost_basis!=='unknown'||!text(v.money_basis,512)||!Array.isArray(v.items)||v.items.length!==7)throw invalid();
 const items=v.items.map(raw=>{const d=object(raw);exact(d,['day','realmoney','refundmoney','profitmoney','orderCount','successCount','successRate']);if(typeof d.day!=='string'||!/^\d{4}-\d{2}-\d{2}$/.test(d.day)||!Number.isFinite(Date.parse(d.day))||new Date(d.day).toISOString().slice(0,10)!==d.day||typeof d.realmoney!=='string'||!/^\d+\.\d{2}$/.test(d.realmoney)||typeof d.refundmoney!=='string'||!/^\d+\.\d{2}$/.test(d.refundmoney)||d.profitmoney!==null||typeof d.orderCount!=='string'||!/^(0|[1-9]\d*)$/.test(d.orderCount)||typeof d.successCount!=='string'||!/^(0|[1-9]\d*)$/.test(d.successCount)||BigInt(d.successCount)>BigInt(d.orderCount))throw invalid();const count=BigInt(d.orderCount),success=BigInt(d.successCount);if(count===0n){if(d.successRate!==null)throw invalid()}else{const units=success*10000n/count;const expected=String(units/100n)+'.'+String(units%100n).padStart(2,'0');if(d.successRate!==expected)throw invalid()}return d as unknown as ParentDay});
 for(let i=1;i<items.length;i++)if(Date.parse(items[i].day)-Date.parse(items[i-1].day)!==86400000)throw invalid();return {channel_id:channel,items,cost_basis:'unknown',money_basis:v.money_basis as string};
}

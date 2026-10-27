import {expect,it,vi} from 'vitest';
import {flushPromises,mount} from '@vue/test-utils';
import {ApiClient} from '../src/api';
import ParentChannelPage from '../src/components/ParentChannelPage.vue';
const row={id:'9007199254740993',name:'微信收款',pay_type:'wxpay',plugin:'wxpay',configured:true,source_channel_id:'8',revision:'1',account_version:'1',enabled:false,archived:false,mode:0,rate:'99.50',fee_rate_bps:50,fingerprint:'a'.repeat(64),account:{plugin:'wxpay',app_types:['1','2'],mode:0,fields:[]},created_at:'2026-10-26T00:00:00Z',updated_at:'2026-10-26T00:00:00Z'};
const catalog={channel_id:row.id,source_channel_id:'8',fingerprint:'b'.repeat(64),items:[{id:'9007199254740995',revision:'1',kind:'web',appid:'public-web-app'},{id:'82',revision:'1',kind:'mini',appid:'public-mini-app'}],total:2};
function fixture(){const client=new ApiClient('admin');client.csrfToken='original-context';const calls:Record<string,unknown>[]=[];vi.spyOn(client,'request').mockImplementation(async(path,options)=>{if(options?.method==='PATCH'){calls.push(options.body as Record<string,unknown>);return {...row,revision:'2',account_version:'2',account:{...row.account,appwxmp:'9007199254740995',appwxa:'82'}}}if(path.endsWith('/weixin-options'))return catalog;if(path.endsWith('/templates'))return {items:[],total:0};if(path.includes('?'))return {items:[row],total:1,page:1,size:20};return row});return {client,calls}}

it('shows original Weixin selectors and saves exact full IDs with the reviewed catalog',async()=>{
 const {client,calls}=fixture(),w=mount(ParentChannelPage,{props:{client,allowed:true}});await flushPromises();await w.get('[data-parent-parameters]').trigger('click');await flushPromises();
 expect(w.find('select[aria-label="关联微信公众号"]').exists()).toBe(true);
 expect(w.get('select[aria-label="关联微信公众号"]').text()).toContain('public-web-app');
 expect(w.get('select[aria-label="关联微信公众号"]').text()).not.toContain('public-mini-app');
 await w.get('select[aria-label="关联微信公众号"]').setValue('9007199254740995');await w.get('select[aria-label="关联微信小程序"]').setValue('82');
 await w.get('form[data-parent-editor]').trigger('submit');await flushPromises();
 expect(calls).toHaveLength(1);expect(calls[0].account).toMatchObject({appwxmp:'9007199254740995',appwxa:'82',expected_bindings_fingerprint:catalog.fingerprint});
 expect(w.text()).toContain('父通道配置已保存');w.unmount();
});

it('drops selected applications when a raw client CSRF context changes',async()=>{
 const {client,calls}=fixture(),w=mount(ParentChannelPage,{props:{client,allowed:true}});await flushPromises();await w.get('[data-parent-parameters]').trigger('click');await flushPromises();
 await w.get('select[aria-label="关联微信公众号"]').setValue('9007199254740995');client.csrfToken='other-context';await w.get('form[data-parent-editor]').trigger('submit');await flushPromises();
 expect(calls).toHaveLength(0);expect(w.find('[role="dialog"]').exists()).toBe(false);w.unmount();
});

it('reviews the original selection before an atomic batch Weixin association',async()=>{
 const {client}=fixture();const writes:unknown[]=[];vi.mocked(client.request).mockImplementation(async(path,options)=>{if(path==='/payment-channels/bind-weixin'){writes.push(options?.body);return {type:0,weixin:'9007199254740995',items:[{...row,account:{...row.account,appwxmp:'9007199254740995'}}],total:1}}if(path.endsWith('/weixin-options'))return catalog;if(path.endsWith('/templates'))return {items:[],total:0};if(path.includes('?'))return {items:[row],total:1,page:1,size:20};return row});
 const w=mount(ParentChannelPage,{props:{client,allowed:true}});await flushPromises();await w.get('input[aria-label="选择通道 '+row.id+'"]').setValue(true);await w.findAll('button').find(b=>b.text()==='批量关联微信公众号')!.trigger('click');await flushPromises();
 expect(w.get('[role="dialog"][aria-label="批量关联微信公众号"]').text()).toContain(row.id);expect(writes).toHaveLength(0);
 await w.get('select[aria-label="批量微信应用"]').setValue('9007199254740995');await w.get('[data-weixin-batch-confirm]').trigger('click');await flushPromises();
 expect(writes).toEqual([{type:0,weixin:'9007199254740995',items:[{id:row.id,expected_fingerprint:row.fingerprint,expected_bindings_fingerprint:catalog.fingerprint}]}]);w.unmount();
});

it('opens the original row payment-test dialog without automatically sending a payment',async()=>{
 const {client}=fixture(),w=mount(ParentChannelPage,{props:{client,allowed:true}});await flushPromises();await w.findAll('button').find(b=>b.text()==='测试')!.trigger('click');await flushPromises();
 expect(w.get('[role="dialog"][aria-label="测试支付"]').text()).toContain(row.id);expect((w.get('input[aria-label="订单名称"]').element as HTMLInputElement).value).toBe('支付测试');expect((w.get('input[aria-label="订单金额"]').element as HTMLInputElement).value).toBe('1');expect(vi.mocked(client.request).mock.calls.some(([,options])=>options?.method==='POST')).toBe(false);w.unmount();
});

it('warns about rotated applications and explicitly rebinds unchanged IDs to the current profile',async()=>{
 const {client}=fixture(),writes:Record<string,unknown>[]=[];
 const currentRow={...row,account:{...row.account,appwxmp:'81',appwxa:'82',weixin_binding_state:{web:{id:'81',revision:'1',appid:'old-public-app',current:false},mini:{id:'82',revision:'1',appid:'public-mini-app',current:true}}}};
 const currentCatalog={...catalog,items:[{id:'81',revision:'2',kind:'web',appid:'new-public-app'},{id:'82',revision:'1',kind:'mini',appid:'public-mini-app'}]};
 vi.mocked(client.request).mockImplementation(async(path,options)=>{if(options?.method==='PATCH'){writes.push(options.body as Record<string,unknown>);return currentRow}if(path.endsWith('/weixin-options'))return currentCatalog;if(path.endsWith('/templates'))return {items:[],total:0};if(path.includes('?'))return {items:[currentRow],total:1,page:1,size:20};return currentRow});
 const w=mount(ParentChannelPage,{props:{client,allowed:true}});await flushPromises();await w.get('[data-parent-parameters]').trigger('click');await flushPromises();expect(w.text()).toContain('应用资料已更新');expect(w.text()).toContain('old-public-app');
 await w.findAll('button').find(b=>b.text()==='重新关联所选应用')!.trigger('click');await w.get('form[data-parent-editor]').trigger('submit');await flushPromises();expect(writes).toHaveLength(1);expect(writes[0].account).toMatchObject({appwxmp:'81',appwxa:'82',expected_bindings_fingerprint:catalog.fingerprint});w.unmount();
});

it('does not announce a saved binding when the response keeps another association',async()=>{
 const {client}=fixture();vi.mocked(client.request).mockImplementation(async(path,options)=>{if(options?.method==='PATCH')return row;if(path.endsWith('/weixin-options'))return catalog;if(path.endsWith('/templates'))return {items:[],total:0};if(path.includes('?'))return {items:[row],total:1,page:1,size:20};return row});
 const w=mount(ParentChannelPage,{props:{client,allowed:true}});await flushPromises();await w.get('[data-parent-parameters]').trigger('click');await flushPromises();await w.get('select[aria-label="关联微信公众号"]').setValue('9007199254740995');await w.get('form[data-parent-editor]').trigger('submit');await flushPromises();expect(w.text()).toContain('结果未知');expect(w.text()).not.toContain('父通道配置已保存');expect((w.get('[data-parent-save]').element as HTMLButtonElement).disabled).toBe(true);w.unmount();
});



import {expect,it,vi} from 'vitest';
import {mount,flushPromises} from '@vue/test-utils';
import ParentChannelPage from '../src/components/ParentChannelPage.vue';
import {ApiClient} from '../src/api';
const fields=[{name:'appid',namespace:'config',secret:false,pem:false,present:true,value:'public-app'},{name:'appkey',namespace:'config',secret:true,pem:false,present:true}];
const row={id:'9007199254740993',name:'微信收款',pay_type:'wxpay',plugin:'wxpay',configured:true,source_channel_id:'8',revision:'1',account_version:'1',enabled:false,archived:false,mode:0,rate:'99.50',fee_rate_bps:50,fingerprint:'a'.repeat(64),account:{plugin:'wxpay',app_types:['1'],mode:0,fields},created_at:'2026-10-26T00:00:00Z',updated_at:'2026-10-26T00:00:00Z'};
function client(){const c=new ApiClient('admin');c.csrfToken='first';vi.spyOn(c,'request').mockImplementation(async path=>path.includes('/templates')?{items:[],total:0}:path.includes('?')?{items:[row],total:1,page:1,size:20}:row);return c}
it('renders original parent columns, filters and versioned secret edits without exposing secrets',async()=>{
 const c=client(),w=mount(ParentChannelPage,{props:{client:c,allowed:true}});await flushPromises();
 expect(w.text()).toContain('分成比例');expect(w.text()).toContain('配置密钥');expect(w.find('input[aria-label="ID或名称"]').exists()).toBe(true);
 await w.get('button[data-parent-parameters]').trigger('click');await flushPromises();
 expect((w.get('select[aria-label="appkey 操作"]').element as HTMLSelectElement).value).toBe('keep');
 await w.get('select[aria-label="appkey 操作"]').setValue('replace');await w.get('input[aria-label="appkey 内容"]').setValue('draft-secret');
 c.csrfToken='changed';await w.get('input[aria-label="通道名称"]').trigger('focusin');await flushPromises();
 expect(w.html()).not.toContain('draft-secret');expect(w.find('[role="dialog"]').exists()).toBe(false);w.unmount();
});
it('does not automatically repeat an uncertain parameter mutation',async()=>{
 const c=client();const original=c.request.bind(c);const spy=vi.spyOn(c,'request').mockImplementation(async(path,options)=>{if(options?.method==='PATCH')throw new Error('synthetic uncertain');return original(path,options)});
 // Replace the read implementation directly to avoid a spy recursive wrapper.
 spy.mockImplementation(async(path,options)=>{if(options?.method==='PATCH')throw new Error('synthetic uncertain');return path.includes('/templates')?{items:[],total:0}:path.includes('?')?{items:[row],total:1,page:1,size:20}:row});
 const w=mount(ParentChannelPage,{props:{client:c,allowed:true}});await flushPromises();await w.get('button[data-parent-parameters]').trigger('click');await flushPromises();
 await w.get('input[aria-label="通道名称"]').setValue('编辑名称');await w.get('form[data-parent-editor]').trigger('submit');await flushPromises();
 expect(w.text()).toContain('结果未知');expect((w.get('button[data-parent-save]').element as HTMLButtonElement).disabled).toBe(true);
 expect(spy.mock.calls.filter(([,o])=>o?.method==='PATCH')).toHaveLength(1);w.unmount();
});
it('reviews the exact original channel before changing its status',async()=>{
 const c=client(),w=mount(ParentChannelPage,{props:{client:c,allowed:true}});await flushPromises();
 // The 状态 pill is now a kit KitSwitch; toggling it asks for confirmation.
 await w.get('input[role="switch"][aria-label="已关闭"]').setValue(true);await flushPromises();
 expect(vi.mocked(c.request).mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(0);
 expect(w.get('[role="dialog"]').text()).toContain(row.id);
 expect(w.get('[role="dialog"]').text()).toContain('开启');
 w.unmount();
});

it('clears a sensitive draft when an in-flight response observes a raw CSRF change without another interaction',async()=>{
 const c=client();let release!:(value:unknown)=>void;const pending=new Promise(resolve=>{release=resolve});vi.mocked(c.request).mockImplementation(async(path,options)=>{if(options?.method==='PATCH')return pending;return path.includes('/templates')?{items:[],total:0}:path.includes('?')?{items:[row],total:1,page:1,size:20}:row});
 const w=mount(ParentChannelPage,{props:{client:c,allowed:true}});await flushPromises();await w.get('[data-parent-parameters]').trigger('click');await flushPromises();await w.get('select[aria-label="appkey 操作"]').setValue('replace');await w.get('input[aria-label="appkey 内容"]').setValue('late-response-secret');await w.get('form[data-parent-editor]').trigger('submit');
 c.csrfToken='changed-during-await';release(row);await flushPromises();expect(w.find('[role="dialog"]').exists()).toBe(false);expect(w.html()).not.toContain('late-response-secret');w.unmount();
});

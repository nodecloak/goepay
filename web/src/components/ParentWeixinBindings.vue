<script setup lang="ts">
import {computed,onBeforeUnmount,ref,watch} from 'vue';
import type {ApiClient} from '../api';
import {parentBindingID,readParentWeixinCatalog,type ParentWeixinCatalog,type ParentWeixinDraft,type ParentWeixinBindingState} from '../parent-channel-bindings';
const props=defineProps<{client:ApiClient;channelID:string;sourceChannelID:string;initialWeb?:string;initialMini?:string;bindingState?:ParentWeixinBindingState;disabled:boolean}>();
const emit=defineEmits<{draft:[ParentWeixinDraft|undefined]}>();
const catalog=ref<ParentWeixinCatalog>(),web=ref('0'),mini=ref('0'),loading=ref(false),error=ref(''),changed=ref(false);
let active=true,epoch=0,controller:AbortController|undefined,originalClient=props.client,originalCSRF=props.client.csrfToken;
const webOptions=computed(()=>catalog.value?.items.filter(x=>x.kind==='web')||[]),miniOptions=computed(()=>catalog.value?.items.filter(x=>x.kind==='mini')||[]);
const originalWeb=()=>props.initialWeb||'0',originalMini=()=>props.initialMini||'0';
function context(){return active&&originalClient===props.client&&originalCSRF===props.client.csrfToken}
async function load(){const n=++epoch,c=props.client,csrf=c.csrfToken;originalClient=c;originalCSRF=csrf;controller?.abort();controller=new AbortController();catalog.value=undefined;error.value='';changed.value=false;web.value=originalWeb();mini.value=originalMini();emit('draft',undefined);if(!parentBindingID(props.channelID)||!parentBindingID(props.sourceChannelID)||!parentBindingID(web.value,true)||!parentBindingID(mini.value,true)){error.value='原关联编号无法核实';return}loading.value=true;
 try{const raw=await c.request('/payment-channels/'+props.channelID+'/weixin-options',{signal:controller.signal});if(!active||n!==epoch||c!==props.client||csrf!==c.csrfToken)return;catalog.value=readParentWeixinCatalog(raw,props.channelID,props.sourceChannelID)}catch(e){if(active&&n===epoch&&context())error.value=e instanceof Error?e.message:'微信应用列表加载失败'}finally{if(active&&n===epoch&&context())loading.value=false}}
function select(refresh=false){if(!context()){emit('draft',undefined);void load();return}const data=catalog.value;if(props.disabled||loading.value||!data)return;const out:ParentWeixinDraft={expected_bindings_fingerprint:data.fingerprint};
 for(const [key,value,initial,kind]of [['appwxmp',web.value,originalWeb(),'web'],['appwxa',mini.value,originalMini(),'mini']] as const){if(value!==initial||refresh&&value!=='0'){if(value!=='0'&&!data.items.some(x=>x.id===value&&x.kind===kind)){error.value='所选微信应用不可用';emit('draft',undefined);return}out[key]=value}}
 changed.value=Object.keys(out).length>1;emit('draft',changed.value?out:undefined);
}
watch(()=>[props.client,props.client.csrfToken,props.channelID,props.sourceChannelID,props.initialWeb,props.initialMini],load,{immediate:true,flush:'sync'});
onBeforeUnmount(()=>{active=false;++epoch;controller?.abort();catalog.value=undefined;web.value='0';mini.value='0'});
</script>
<template>
 <fieldset class="weixin-bindings" :disabled="disabled||loading||!catalog" aria-label="微信应用关联">
  <legend>微信应用关联</legend>
  <p v-if="bindingState?.web?.current===false" role="alert">微信公众号应用资料已更新。原关联 {{bindingState.web.appid}}；请重新关联所选应用并保存通道，以恢复新订单。</p>
  <p v-if="bindingState?.mini?.current===false" role="alert">微信小程序应用资料已更新。原关联 {{bindingState.mini.appid}}；请重新关联所选应用并保存通道，以恢复新订单。</p>
  <label>关联微信公众号<select v-model="web" aria-label="关联微信公众号" @change="select()"><option value="0">不关联</option><option v-if="web!=='0'&&!webOptions.some(x=>x.id===web)" :value="web" disabled>原绑定 #{{web}}（当前不可选择）</option><option v-for="app in webOptions" :key="app.id" :value="app.id">微信公众号 #{{app.id}}（{{app.appid}}）</option></select></label>
  <label>关联微信小程序<select v-model="mini" aria-label="关联微信小程序" @change="select()"><option value="0">不关联</option><option v-if="mini!=='0'&&!miniOptions.some(x=>x.id===mini)" :value="mini" disabled>原绑定 #{{mini}}（当前不可选择）</option><option v-for="app in miniOptions" :key="app.id" :value="app.id">微信小程序 #{{app.id}}（{{app.appid}}）</option></select></label>
  <button v-if="web!=='0'||mini!=='0'" type="button" @click="select(true)">重新关联所选应用</button><span v-if="changed" role="status">关联设置将在保存通道时更新。</span>
 </fieldset>
 <p v-if="loading" role="status">正在加载微信应用…</p><p v-if="error" role="alert">{{error}}</p>
</template>
<style scoped>.weixin-bindings{display:grid;gap:12px;min-width:0}.weixin-bindings label{display:grid;gap:6px}.weixin-bindings select{max-width:100%;min-width:0}.weixin-bindings button{justify-self:start}</style>

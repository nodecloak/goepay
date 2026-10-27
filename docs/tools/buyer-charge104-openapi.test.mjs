import assert from 'node:assert/strict';
import {installBuyerCharge104API} from './buyer-charge104-openapi.mjs';
const parent={paths:{'/parent':{get:{operationId:'sentinel'}}},schemas:{Parent:{type:'object',properties:{unchanged:{type:'string'}}}}},before=structuredClone(parent),object=(properties,required=[],additionalProperties=false)=>({type:'object',properties,required,additionalProperties}),str=(description,extra={})=>({type:'string',...(description?{description}:{}),...extra}),ref=name=>({$ref:'#/components/schemas/'+name}),content=schema=>({'application/json':{schema}});
installBuyerCharge104API({...parent,object,str,ref,content});
assert.deepEqual(parent.paths['/parent'],before.paths['/parent']);assert.deepEqual(parent.schemas.Parent,before.schemas.Parent);assert.equal(Object.keys(parent.paths).length,4);assert.equal(Object.keys(parent.schemas).length,4);
for(const p of Object.values(parent.paths).slice(1))for(const op of Object.values(p)){assert.equal(op.requestBody?.content['application/json'].schema.additionalProperties??false,false);assert(op.responses['409']);assert(op.responses['503'])}
const once=structuredClone(parent);installBuyerCharge104API({...parent,object,str,ref,content});assert.deepEqual(parent,once);console.log('buyer104 additive API contract PASS');

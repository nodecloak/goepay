import {expect,it} from 'vitest';
import fixture from '../e2e/parent-bindings180-http-fixture.json';
import {readParentChannel} from '../src/parent-channels';
import {readParentWeixinCatalog} from '../src/parent-channel-bindings';
it('parses actual disposed-schema HTTP parent and selector DTOs without changing application IDs',()=>{
 const before=readParentChannel(fixture.bound),after=readParentChannel(fixture.rotated),catalog=readParentWeixinCatalog(fixture.catalog,before.id,before.source_channel_id!);
 expect(before.account?.appwxmp).toBe('81');expect(before.account?.appwxa).toBe('82');expect(after.account?.appwxmp).toBe('81');expect(after.account_version).not.toBe(before.account_version);
 expect(catalog.items.map(x=>x.appid)).toEqual(['synthetic-web-A','synthetic-mini-A']);
 for(const raw of [{...fixture.catalog,channel_id:'9223372036854775806'},{...fixture.catalog,source_channel_id:'9'},{...fixture.catalog,items:[{...fixture.catalog.items[0],id:81}]},{...fixture.catalog,appsecret:'not-allowed'}])expect(()=>readParentWeixinCatalog(raw,before.id,before.source_channel_id!)).toThrow();
});

// Copy only the deliberate source allowlist, so tests/builds can use a fixed
// snapshot while the next phase proceeds. Environment and imported keys never
// enter this directory. A changing source tree fails verification explicitly.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

const root = fileURLToPath(new URL('../',import.meta.url));
const label = process.argv[2];
if (!label || !/^[0-9][a-zA-Z0-9_-]{0,50}$/.test(label)) throw new Error('A numeric phase label is required');
const generator=path.join(root,'docs/tools/source-manifest.mjs');
const readManifest=()=>{
  execFileSync(process.execPath,[generator],{cwd:root,stdio:'pipe'});
  return JSON.parse(fs.readFileSync(path.join(root,'docs/testing/source-manifest.json'),'utf8'));
};
const before=readManifest();
const destination=path.join(root,'.tools','checkpoints',`${label}-${randomUUID()}`);
fs.mkdirSync(destination,{recursive:true});
const digest=data=>createHash('sha256').update(data).digest('hex');
for(const entry of before.files){
  const input=path.resolve(root,entry.path),output=path.resolve(destination,entry.path);
  if(!input.startsWith(root)||!output.startsWith(destination+path.sep)) throw new Error('Checkpoint path escaped its source or target');
  if(fs.lstatSync(input).isSymbolicLink()) throw new Error('Source checkpoint does not follow symbolic links');
  const data=fs.readFileSync(input);
  if(digest(data)!==entry.sha256) throw new Error('Source changed before checkpoint copy: '+entry.path);
  fs.mkdirSync(path.dirname(output),{recursive:true});
  fs.writeFileSync(output,data,{flag:'wx'});
  if(digest(fs.readFileSync(output))!==entry.sha256)throw new Error('Checkpoint copy verification failed: '+entry.path);
}
const after=readManifest();
if(JSON.stringify(before.files)!==JSON.stringify(after.files))throw new Error('Source changed during checkpoint; this directory must not be used: '+destination);
fs.writeFileSync(path.join(destination,'SOURCE_CHECKPOINT.json'),JSON.stringify({label,created_at:new Date().toISOString(),source_root:root,scope:'Source only. No environment, production configuration or compatibility completion claim.',files:before.files},null,2)+'\n',{flag:'wx'});
console.log(JSON.stringify({checkpoint:destination,source_files:before.files.length,verified:true}));

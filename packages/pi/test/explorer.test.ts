import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, symlink, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { issueMission, acceptMission } from '../src/workers/mission.ts';
import { createExplorerTools } from '../src/workers/explorer.ts';
async function setup(t: any, overrides = {}) {
 const workspace = await mkdtemp(join(tmpdir(),'pi-explorer-')); t.after(()=>rm(workspace,{recursive:true,force:true}));
 await mkdir(join(workspace,'src')); await writeFile(join(workspace,'outside'),'secret');
 const mission = await acceptMission(issueMission({nonce:crypto.randomUUID(),role:'explore',workspace,mode:'read-only',model:'p/m',effort:'low',goal:'inspect',criteria:[],commands:[],resultLimit:65536,targets:{},exploration:{roots:['src'],maxFiles:1000,maxBytes:100000,maxTokens:65536,...overrides}}));
 const tools = Object.fromEntries(createExplorerTools(mission).map(tool=>[tool.name,tool]));
 const run = async(name:string,input:any)=> {const result = await tools[name].execute('x',input); return {...JSON.parse(result.content[0].text), details:result.details};};
 return {workspace,run};
}
test('explorer pages Unicode text, returns usage, rejects stale cursor',async t=>{
 const {workspace,run}=await setup(t); await writeFile(join(workspace,'src/a'),'😀abc');
 const first=await run('worker_read_page',{path:'src/a',limit:2}); assert.equal(first.text,'😀a'); assert.ok(first.details.usage.tokens>0);
 assert.equal((await run('worker_read_page',{path:'src/a',limit:2,cursor:first.nextCursor})).text,'bc');
 await writeFile(join(workspace,'src/a'),'changed'); await assert.rejects(run('worker_read_page',{path:'src/a',cursor:first.nextCursor}),/stale/);
});
test('explorer confines roots, denies symlinks and binary files',async t=>{
 const {workspace,run}=await setup(t); await symlink(join(workspace,'outside'),join(workspace,'src/link')); await writeFile(join(workspace,'src/binary'),Buffer.from([0,1]));
 await assert.rejects(run('worker_read_page',{path:'outside'}),/outside/); await assert.rejects(run('worker_read_page',{path:'src/../outside'}),/rejected/);
 await assert.rejects(run('worker_read_page',{path:'src/link'}),/symlink/); await assert.rejects(run('worker_read_page',{path:'src/binary'}),/binary/);
 assert.ok(!(await run('worker_list',{path:'src'})).entries.some((e:any)=>e.name==='link'));
});
test('explorer budgets accumulate across calls, bounded search pages',async t=>{
 const {workspace,run}=await setup(t,{maxFiles:3}); await writeFile(join(workspace,'src/a'),Array.from({length:25},(_,i)=>`needle ${i}`).join('\n'));
 const first=await run('worker_search',{path:'src/a',query:'needle'}); assert.equal(first.matches.length,20);
 const second=await run('worker_search',{path:'src/a',query:'needle',cursor:first.nextCursor}); assert.equal(second.matches.length,5);
 await run('worker_read_page',{path:'src/a'}); await assert.rejects(run('worker_read_page',{path:'src/a'}),/budget/);
});
test('explorer fails closed before oversized input or output',async t=>{
 const a=await setup(t,{maxBytes:2});await writeFile(join(a.workspace,'src/a'),'three');await assert.rejects(a.run('worker_read_page',{path:'src/a'}),/budget/);
 const b=await setup(t,{maxTokens:1});await writeFile(join(b.workspace,'src/a'),'x');await assert.rejects(b.run('worker_read_page',{path:'src/a'}),/token budget/);
});
test('explorer directory cursor binds snapshot and search cursor binds query',async t=>{
 const {workspace,run}=await setup(t);for(let i=0;i<55;i++)await writeFile(join(workspace,'src',String(i).padStart(2,'0')),'');
 const first=await run('worker_list',{path:'src'});assert.equal(first.entries.length,50);assert.equal((await run('worker_list',{path:'src',cursor:first.nextCursor})).entries.length,5);
 await writeFile(join(workspace,'src/new'),'x');await assert.rejects(run('worker_list',{path:'src',cursor:first.nextCursor}),/stale/);
 await writeFile(join(workspace,'src/a'),Array(25).fill('needle other').join('\n'));const search=await run('worker_search',{path:'src/a',query:'needle'});await assert.rejects(run('worker_search',{path:'src/a',query:'other',cursor:search.nextCursor}),/stale/);
 await assert.rejects(run('worker_read_page',{path:'src/a',extra:true}),/invalid/);
});

#!/usr/bin/env python3
"""Offline planner and explicitly gated Pi development evaluation transport."""
import argparse
import hashlib
import json
import os
import pathlib
import selectors
import signal
import stat
import subprocess
import sys
import time
import re
import shutil
VERSION="pi-evaluation-runner/2"
def sha(b): return hashlib.sha256(b).hexdigest()
MAX_STREAM=2*1024*1024; MAX_CALLS=64
def _bad(s): raise ValueError(s)
def _path(v, what, exists=True):
 p=pathlib.Path(v)
 if not p.is_absolute(): _bad('absolute '+what+' required')
 # lstat deliberately sees dangling links, unlike Path.exists().
 for q in (p,*p.parents):
  try: mode=os.lstat(q).st_mode
  except FileNotFoundError: continue
  if stat.S_ISLNK(mode): _bad('symlink ancestor '+what)
  if q != p and not stat.S_ISDIR(mode): _bad('non-directory ancestor '+what)
 if exists:
  try: mode=os.lstat(p).st_mode
  except FileNotFoundError: _bad('missing '+what)
  if not (stat.S_ISREG(mode) or stat.S_ISDIR(mode)): _bad('special '+what)
 return p.resolve(strict=exists)
def _hash(p): return sha(_path(p,'file').read_bytes())
def _record(p):
 p=_path(p,'file');
 if not p.is_file(): _bad('not regular file')
 return {'path':str(p),'sha256':_hash(p),'mode':stat.S_IMODE(p.stat().st_mode)}
def _ignored_dirs(root):
 gitignore=root/'.gitignore'
 if not gitignore.exists() or gitignore.is_symlink() or not gitignore.is_file(): _bad('unsafe .gitignore')
 lines=gitignore.read_text(encoding='utf-8').splitlines()
 known={'.atl','.codegraph','agent-eval-output','tools/agent_eval/__pycache__','node_modules'}
 selected=set()
 for raw in lines:
  item=raw.strip()
  if not item or item.startswith('#'): continue
  if item.startswith('!') or any(ch in item for ch in '*?[]'): _bad('unsupported .gitignore pattern')
  if not item.endswith('/'): _bad('unsupported .gitignore file pattern')
  item=item[:-1].strip('/')
  if item not in known: _bad('unsupported ignored directory')
  selected.add(item)
 return selected, _record(gitignore)
def _tree(root, ignored=None):
 root=_path(root,'repo'); out=[]
 ignored=set() if ignored is None else set(ignored)
 for base,ds,fs in os.walk(root,followlinks=False):
  relbase=pathlib.Path(base).relative_to(root)
  kept=[]
  for d in sorted(ds):
   rel=(relbase/d).as_posix()
   if d in ignored or rel in ignored: continue
   q=pathlib.Path(base,d)
   mode=os.lstat(q).st_mode
   if stat.S_ISLNK(mode): _bad('symlink source directory')
   if not stat.S_ISDIR(mode): _bad('special source directory')
   kept.append(d)
  ds[:]=kept
  for n in sorted(fs):
   p=pathlib.Path(base,n)
   mode=os.lstat(p).st_mode
   if stat.S_ISLNK(mode): _bad('symlink source file')
   if not stat.S_ISREG(mode): _bad('special source file')
   out.append(_record(p))
 return out
def _registry(p):
 try:r=json.loads(_path(p,'registry').read_text())
 except Exception as e:_bad('invalid registry '+str(e))
 if not isinstance(r,dict) or type(r.get('schema')) is not int or r.get('schema')!=1 or r.get('partition') not in ('public-development','public development, not protected holdout'): _bad('invalid public development registry')
 cases=r.get('cases')
 if not isinstance(cases,list) or not cases: _bad('nonempty cases required')
 seen=set()
 for c in cases:
  if not isinstance(c,dict) or not isinstance(c.get('id'),str) or not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,63}',c['id']) or c['id'] in seen or c.get('protected') or c.get('holdout'):_bad('invalid case')
  prompt=c.get('prompt')
  if not isinstance(prompt,str) or not prompt.strip() or len(prompt.encode())>32768 or prompt.lstrip().startswith(('-','@','/')):_bad('invalid or unsafe case prompt')
  files=c.get('fixtureFiles',{})
  if not isinstance(files,dict) or len(files)>512:_bad('invalid fixture map')
  total=0
  for rel,content in files.items():
   if not isinstance(rel,str) or not isinstance(content,str):_bad('invalid fixture entry')
   parts=rel.split('/')
   if not rel or '\\' in rel or ':' in rel or pathlib.PurePosixPath(rel).is_absolute() or any(x in ('','.','..','.pi','.agents','.git') for x in parts):_bad('unsafe fixture')
   total+=len(content.encode())
   if total>1024*1024:_bad('fixture bytes limit')
  seen.add(c['id'])
 return r
def _catalog(root):
 root=_path(root,'skills root'); out=[]
 for d in sorted(root.iterdir()):
  f=d/'SKILL.md'
  if d.is_dir() and not d.is_symlink() and f.exists():
   if f.is_symlink():_bad('symlink skill')
   raw=f.read_bytes()
   if len(raw)>65536:_bad('skill manifest limit')
   try:text=raw.decode('utf-8')
   except UnicodeDecodeError:_bad('skill manifest encoding')
   name=re.search(r'^name:\s*([^\n]+)$',text,re.M); compatibility=re.search(r'^compatibility:\s*([^\n]+)$',text,re.M)
   if name and name.group(1).strip()!=d.name: _bad('skill name/directory mismatch')
   if not name or not compatibility or not re.search(r'agent skills hosts',compatibility.group(1),re.I) or not re.search(r'managed-by:\s*vgxness|provenance:\s*["\']VGXNESS',text,re.I): continue
   resources=[]; total=0
   for base,ds,fs in os.walk(d,followlinks=False):
    for x in ds+fs:
     q=pathlib.Path(base,x)
     if q.is_symlink():_bad('selected skill symlink')
    for x in fs:
     q=pathlib.Path(base,x)
     if q==f: continue
     if not q.is_file():_bad('selected skill special')
     # Only textual guidance/resources are injectable into the evaluator.
     # Binary package assets are not selected skill resources.
     if q.stat().st_size>65536:_bad('skill resource limit')
     try:q.read_text(encoding='utf-8')
     except UnicodeDecodeError: continue
     total+=q.stat().st_size
     if total>131072:_bad('skill resource bundle limit')
     resources.append(_record(q))
   resources.sort(key=lambda row:row['path'])
   out.append({'name':d.name,'filePath':str(f.resolve()),'sha256':sha(raw),'mode':stat.S_IMODE(f.stat().st_mode),'resources':resources})
 if not out:_bad('empty catalog')
 return out
def _write_new(p,b,mode=0o600):
 fd=os.open(p,os.O_CREAT|os.O_EXCL|os.O_WRONLY,mode)
 with os.fdopen(fd,'wb') as f:f.write(b)
def _wrapper(extension, workspace, storage, credential_file, copied_catalog, guard_log, worker_cli=''):
 """Generate the evaluator-owned extension; it must exit, never just throw."""
 files=[dict(f,skill=skill['name']) for skill in copied_catalog for f in skill['files']]
 return '''import { createPiExtension } from %s;
import { appendFileSync, lstatSync, readFileSync, realpathSync, writeSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";
const workspace=%s, storage=%s, credentialFile=%s, guardLog=%s, workerCli=%s, files=%s;
let calls=0;
const fatal=(reason)=>{ try { appendFileSync(guardLog,JSON.stringify({reason})+"\\n",{mode:0o600}); } catch {} writeSync(2,"EVALUATOR_FATAL "+reason+"\\n"); process.exit(78); };
const noLink=(path)=>{ let here=resolve(path); for(;;){let info;try{info=lstatSync(here)}catch{fatal("missing bound file")}if(info.isSymbolicLink())fatal("symlink bound file");const parent=dirname(here);if(parent===here)return;here=parent;} };
const inside=(root,path)=>{const rel=relative(root,path);return rel!==".."&&!rel.startsWith(".."+sep)&&!isAbsolute(rel)};
const bound=new Map(), skillRoots=new Map(), skillFiles=new Map();
for(const file of files)if(file.path.endsWith("/SKILL.md")){const path=resolve(file.path);if(skillRoots.has(file.skill))fatal("duplicate skill manifest");skillRoots.set(file.skill,dirname(path));}
for(const file of files){ const path=resolve(file.path), root=skillRoots.get(file.skill);if(!root||!inside(root,path))fatal("bound path outside skill");noLink(path);if(!lstatSync(path).isFile()||realpathSync(path)!==path)fatal("bound nonregular file");if(bound.has(path))fatal("duplicate bound file");bound.set(path,file);const entries=skillFiles.get(file.skill)||new Map(), rel=relative(root,path);if(entries.has(rel))fatal("duplicate bound file");entries.set(rel,path);skillFiles.set(file.skill,entries); }
const catalogFile=(input)=>{ if(typeof input!=="string")fatal("read path invalid");const path=resolve(input);if(!bound.has(path))fatal("read denied");noLink(path);if(!lstatSync(path).isFile()||realpathSync(path)!==path)fatal("read nonregular");return path; };
const fixtureFile=(input)=>{ if(typeof input!=="string")fatal("read path invalid");const path=isAbsolute(input)?resolve(input):resolve(workspace,input);if(!inside(workspace,path))fatal("read outside fixture");noLink(path);if(!lstatSync(path).isFile()||realpathSync(path)!==path)fatal("read nonregular");return path; };
const decode=(value)=>value.replace(/&(amp|lt|gt|quot|apos);/g,(_x,name)=>({amp:"&",lt:"<",gt:">",quot:'"',apos:"'"}[name]));
const tag=(block,name)=>{const pattern=name==="name"?/<name>([\\s\\S]*?)<\\/name>/g:/<location>([\\s\\S]*?)<\\/location>/g,values=[...block.matchAll(pattern)];if(values.length!==1)fatal("catalog malformed");return decode(values[0][1].trim());};
const verifyCatalog=(prompt)=>{if(typeof prompt!=="string")fatal("catalog missing");const blocks=[...prompt.matchAll(/<available_skills>([\\s\\S]*?)<\\/available_skills>/g)];if(blocks.length!==1)fatal("catalog missing");const body=blocks[0][1], skills=[...body.matchAll(/<skill>([\\s\\S]*?)<\\/skill>/g)];if(body.replace(/<skill>[\\s\\S]*?<\\/skill>/g,"").trim())fatal("catalog malformed");const seen=new Set();for(const match of skills){const name=tag(match[1],"name"), location=tag(match[1],"location");const entries=skillFiles.get(name), manifest=entries&&entries.get("SKILL.md");if(!manifest||location!==manifest||seen.has(name))fatal("catalog mismatch");seen.add(name);}if(seen.size!==skillFiles.size)fatal("catalog mismatch");};
export default async function(pi) {
 pi.on("tool_call", event=>{try{const rawName=event.toolName||event.name,name=typeof rawName==="string"?rawName.slice(0,80):"",input=event.input||event.args||{};if(!input||typeof input!=="object"||Array.isArray(input))fatal("tool input invalid");const audit=name==="read"?{type:"attempt",name,path:input.path,offset:input.offset,limit:input.limit}:name==="vgx_skill"?{type:"attempt",name,operation:input.operation,skill:input.name,resources:input.resources}:{type:"attempt",name};const encoded=JSON.stringify(audit);if(Buffer.byteLength(encoded)>8192)fatal("tool audit limit");appendFileSync(guardLog,encoded+"\\n",{mode:0o600});if(++calls>64)fatal("tool budget");if(name==="read"){if(Object.keys(input).some(key=>key!=="path"&&key!=="offset"&&key!=="limit")||typeof input.path!=="string"||[input.offset,input.limit].some(value=>value!==undefined&&(!Number.isInteger(value)||value<1)))fatal("read input invalid");const path=resolve(input.path);bound.has(path)?catalogFile(input.path):fixtureFile(input.path);return;}if(name==="vgx_skill"){if(input.operation==="list"&&Object.keys(input).length===1)return;if(input.operation!=="read"||typeof input.name!=="string"||!skillFiles.has(input.name)||Object.keys(input).some(key=>key!=="operation"&&key!=="name"&&key!=="resources"))fatal("skill denied");if(input.resources!==undefined&&(!Array.isArray(input.resources)||input.resources.length>7||new Set(input.resources).size!==input.resources.length||input.resources.some(resource=>typeof resource!=="string"||resource==="SKILL.md"||resource.includes("/")&&resource.split("/").some(part=>!part||part==="."||part==="..")||!skillFiles.get(input.name).has(resource))))fatal("skill denied");return;}fatal("tool denied "+name);}catch(_error){fatal("tool guard failure");}});
 const extension=createPiExtension({workspace,storageRoot:storage,credentialFile,mode:"full",role:"manager",workerCli:workerCli||undefined});
 await extension(pi);
 pi.on("before_agent_start", event=>{ try { verifyCatalog(event.systemPrompt); for(const file of files){catalogFile(file.path);if(createHash("sha256").update(readFileSync(file.path)).digest("hex")!==file.sha256)fatal("catalog drift");} appendFileSync(guardLog,JSON.stringify({systemPromptSha256:createHash("sha256").update(event.systemPrompt).digest("hex"),catalog:files})+"\\n",{mode:0o600}); return {systemPrompt:event.systemPrompt}; }catch(error){fatal("catalog validation failed");} });
};
'''%(json.dumps(str(pathlib.Path(extension).resolve())),json.dumps(str(workspace)),json.dumps(str(storage)),json.dumps(str(credential_file)),json.dumps(str(guard_log)),json.dumps(str(worker_cli)),json.dumps(files))
def _copy_catalog(catalog, destination):
 destination.mkdir(mode=0o700, parents=True)
 copied=[]
 for skill in catalog:
  root=destination/skill['name'];root.mkdir(mode=0o700)
  entries=[{'path':skill['filePath'],'relative':'SKILL.md','sha256':skill['sha256'],'mode':skill['mode']}] + [dict(x,relative=str(pathlib.Path(x['path']).relative_to(pathlib.Path(skill['filePath']).parent))) for x in skill['resources']]
  copied_rows=[]
  for entry in entries:
   target=root/entry['relative'];target.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
   _write_new(target,_path(entry['path'],'skill source').read_bytes());os.chmod(target,0o600);row=_record(target)
   if row['sha256']!=entry['sha256']: _bad('catalog copy drift')
   copied_rows.append(row)
  copied.append({'name':skill['name'],'files':copied_rows})
 return copied
def plan(a):
 if sys.platform!='linux': _bad('Linux only')
 repo=_path(a.repo,'repo');registry=_path(a.registry,'registry');node=_path(a.node,'node');cli=_path(a.cli,'cli');auth=_path(a.auth_dir,'auth directory');skills=_path(a.skills_root,'skills root')
 if not repo.is_dir() or not auth.is_dir() or not skills.is_dir() or not registry.is_file() or not node.is_file() or not cli.is_file():_bad('invalid planner input kind')
 if not os.access(node,os.X_OK): _bad('node is not executable')
 extension=repo/'packages/pi/src/extension.ts'
 if not extension.is_file() or extension.is_symlink(): _bad('missing bound extension')
 if getattr(a,'extension',None) and _path(a.extension,'extension') != extension.resolve(): _bad('unbound extension')
 package=repo/'packages/pi/package.json'
 if not package.is_file(): _bad('missing Pi package metadata')
 package_data=json.loads(package.read_text())
 if package_data.get('name')!='@vgxness/pi' or not isinstance(package_data.get('version'),str): _bad('invalid Pi package metadata')
 lock=repo/'package-lock.json'
 if not lock.is_file() or lock.is_symlink(): _bad('missing dependency lock')
 sdk_package=cli.parent.parent/'package.json'
 if not sdk_package.is_file() or sdk_package.is_symlink(): _bad('missing SDK package metadata')
 sdk=json.loads(sdk_package.read_text())
 if sdk.get('name')!='@earendil-works/pi-coding-agent' or sdk.get('version')!=a.expected_version: _bad('SDK package mismatch')
 fallback=repo/'packages/pi/resources/skills'
 if fallback.exists(): _bad('unexpected bundled skills fallback')
 if not isinstance(a.provider,str) or not a.provider or not isinstance(a.model,str) or not a.model or a.model.lower() in ('gpt-5.4','gpt-5.5'): _bad('invalid selected model')
 if a.thinking not in ('low','medium','high'): _bad('invalid effort')
 if int(a.timeout)<1 or int(a.timeout)>120: _bad('invalid timeout')
 output=_path(a.output,'output',False)
 if output.exists() or output==repo or repo in output.parents: _bad('new output outside repo required')
 registry_data=_registry(registry); by_id={c['id']:c for c in registry_data['cases']}; selected=getattr(a,'case',None) or list(by_id)
 if len(selected)!=len(set(selected)) or any(x not in by_id for x in selected): _bad('unknown or duplicate selected case')
 catalog=_catalog(skills);ignored,ignore_record=_ignored_dirs(repo)
 old=os.umask(0o077)
 try:
  output.mkdir(mode=0o700);(output/'runs').mkdir(mode=0o700);(output/'private').mkdir(mode=0o700)
  cases=[]
  for case_id in selected:
   case=by_id[case_id]; private=output/'private'/case_id;home=private/'home';workspace=private/'workspace';storage=private/'storage'
   for d in (private,home,workspace,storage):d.mkdir(mode=0o700,parents=True,exist_ok=True)
   copied=_copy_catalog(catalog,home/'.agents'/'skills')
   for rel,text in case.get('fixtureFiles',{}).items():
    target=workspace/rel;target.parent.mkdir(mode=0o700,parents=True,exist_ok=True);_write_new(target,text.encode())
   settings=workspace/'.pi'/'settings.json';settings.parent.mkdir(mode=0o700);_write_new(settings,b'{"retry":{"enabled":false,"maxRetries":0,"provider":{"maxRetries":0}}}\n')
   wrapper=private/'wrapper.mjs';guard_log=private/'guard.jsonl';credential=private/'missing-credentials.json';_write_new(wrapper,_wrapper(extension,workspace,storage,credential,copied,guard_log,cli).encode())
   argv=[str(node),str(cli),'--print','--mode','json','--no-session','--offline','--no-extensions','--extension',str(wrapper),'--no-skills','--no-prompt-templates','--no-themes','--no-context-files','--approve','--tools','read,vgx_skill','--provider',a.provider,'--model',a.model,'--thinking',a.thinking,case['prompt']]
   cases.append({'id':case_id,'prompt':case['prompt'],'workspace':str(workspace),'home':str(home),'storage':str(storage),'credentialFile':str(credential),'guardLog':str(guard_log),'wrapper':_record(wrapper),'catalogCopy':copied,'manifest':_tree(workspace),'argv':argv,'timeout':int(a.timeout),'stdoutLimitBytes':MAX_STREAM,'stderrLimitBytes':MAX_STREAM})
  bindings={'source':_tree(repo,ignored|{'.git'}),'gitignore':ignore_record,'registry':_record(registry),'node':_record(node),'cli':_record(cli),'extension':_record(extension),'package':_record(package),'lock':_record(lock),'sdkPackage':_record(sdk_package),'harness':_record(pathlib.Path(__file__)),'catalog':catalog}
  value={'schema':1,'kind':'pi-public-development','repo':str(repo),'skillsRoot':str(skills),'authDir':str(auth),'provider':a.provider,'model':a.model,'thinking':a.thinking,'expectedVersion':a.expected_version,'bindings':bindings,'cases':cases,'excludedPaths':['.git']+sorted(ignored)}
  raw=(json.dumps(value,sort_keys=True,separators=(',',':'))+'\n').encode();_write_new(output/'plan.json',raw);_write_new(output/'plan.sha256',(sha(raw)+'\n').encode());return value
 finally: os.umask(old)
def _same(xs):
 try:return all(_record(x['path'])==x for x in xs)
 except Exception:return False
def _events(data, requested_provider, requested_model, requested_effort=None):
 """Accept only a clean terminal Pi NDJSON sequence; retain first failure."""
 final = None
 ended = False
 settled = False
 observed_provider = observed_model = observed_effort = None
 usage = cost = None
 for line in data.splitlines():
  try:
   event = json.loads(line)
  except json.JSONDecodeError:
   return 'malformed NDJSON', None
  if not isinstance(event, dict):
   return 'malformed event', None
  event_type = event.get('type')
  if event_type in ('error', 'aborted', 'extension_error'):
   return 'error event', None
  if event_type == 'tool_execution_end' and event.get('isError'):
   return 'failed tool', None
  if event_type == 'message_end':
   message = event.get('message')
   if not isinstance(message, dict) or message.get('role') != 'assistant':
    continue
   stop = message.get('stopReason', event.get('stopReason'))
   if stop in ('error', 'aborted'):
    return 'assistant error or aborted', None
   provider = message.get('provider')
   model = message.get('model')
   if not isinstance(provider, str) or not isinstance(model, str):
    return 'assistant provider/model missing', None
   if provider != requested_provider or model != requested_model:
    return 'provider/model mismatch', None
   content = message.get('content')
   if not isinstance(content, list) or any(not isinstance(part, dict) for part in content):
    return 'assistant content malformed', None
   # A tool-use message is an intermediate assistant turn, not a completion.
   # It still proves the selected provider/model and invalidates an old end.
   if stop == 'toolUse':
    final = None
    ended = settled = False
    continue
   if stop not in ('stop', 'length'):
    return 'assistant missing terminal stop reason', None
   text = ''.join(part['text'] for part in content if part.get('type') == 'text' and isinstance(part.get('text'), str))
   if not text.strip():
    return 'assistant final text missing', None
   final = text
   ended = settled = False
   observed_provider = provider
   observed_model = model
   observed_effort = message.get('thinkingLevel')
   usage = message.get('usage')
   cost = usage.get('cost') if isinstance(usage, dict) else None
  elif event_type == 'agent_end':
   if final is None:
    return 'agent ended before final assistant', None
   if event.get('willRetry'):
    return 'agent retry', None
   ended = True
  elif event_type == 'agent_settled':
   if not ended:
    return 'agent settled before nonretry end', None
   settled = True
 if final is None or not ended or not settled:
  return 'missing successful completion', None
 return None, {'finalAssistant': final, 'actualProvider': observed_provider,
               'actualModel': observed_model, 'requestedEffort': requested_effort,
               'observedEffort': observed_effort, 'effectiveEffort': None,
               'usage': usage if usage is not None else 'estimated/unobserved',
               'cost': {'value': cost, 'label': 'SDK estimate'} if cost is not None else 'estimated/unobserved'}
def _early_failure(stream, line, provider, model):
    """Only definite streaming failures; terminal completeness is checked at EOF."""
    if stream == "stderr":
        text = line.decode("utf-8", errors="replace")
        if "EVALUATOR_FATAL" in text or "Extension error (" in text:
            return "extension guard/load failure"
        return None
    try:
        event = json.loads(line.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return "malformed NDJSON"
    if not isinstance(event, dict):
        return "malformed event"
    if event.get("type") in ("error", "aborted", "extension_error"):
        return "error event"
    if event.get("type") == "tool_execution_end" and event.get("isError"):
        return "failed tool"
    if event.get("type") == "agent_end" and event.get("willRetry"):
        return "agent retry"
    message = event.get("message")
    if event.get("type") == "message_end" and isinstance(message, dict) and message.get("role") == "assistant":
        if message.get("stopReason") in ("error", "aborted"):
            return "assistant error or aborted"
        if message.get("provider") != provider or message.get("model") != model:
            return "provider/model mismatch"
    return None


def _process(argv, cwd, env, stdout_path, stderr_path, deadline, limit=MAX_STREAM, observer=None):
    """Drain independent bounded streams; own, terminate and reap a Linux group.

    deadline is absolute monotonic time, shared with version probes by run().
    A descendant that escapes this process group is outside this trusted-host
    guard. Cleanup can take up to five seconds after the execution deadline.
    """
    started = time.monotonic()
    proc = None
    selector = None
    outputs = {}
    counts = {"stdout": 0, "stderr": 0}
    pending_lines = {"stdout": b"", "stderr": b""}
    result = {"reason": None, "exitCode": None, "reaped": False,
              "streamBytes": counts, "cleanupError": None}

    def fail(reason):
        if result["reason"] is None:
            result["reason"] = reason

    try:
        for name, path in (("stdout", stdout_path), ("stderr", stderr_path)):
            fd = os.open(_path(path, "stream", False),
                         os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            outputs[name] = os.fdopen(fd, "wb", buffering=0)
        if time.monotonic() >= deadline:
            fail("timeout")
        else:
            proc = subprocess.Popen(argv, cwd=cwd, env=env,
                                    stdin=subprocess.DEVNULL,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    start_new_session=True)
            result["pid"] = proc.pid
            selector = selectors.DefaultSelector()
            for name, pipe in (("stdout", proc.stdout), ("stderr", proc.stderr)):
                os.set_blocking(pipe.fileno(), False)
                selector.register(pipe, selectors.EVENT_READ, name)
            while selector.get_map() or proc.poll() is None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    fail("timeout")
                    break
                if not selector.get_map():
                    # EOF does not imply process exit. Still respect the same deadline.
                    try:
                        proc.wait(timeout=min(remaining, 0.05))
                    except subprocess.TimeoutExpired:
                        pass
                    continue
                for key, _ in selector.select(min(remaining, 0.05)):
                    try:
                        data = os.read(key.fileobj.fileno(), 65536)
                    except BlockingIOError:
                        continue
                    if not data:
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                        if observer and pending_lines[key.data]:
                            observed = observer(key.data, pending_lines[key.data])
                            pending_lines[key.data] = b""
                            if observed:
                                fail(observed)
                                break
                        continue
                    name = key.data
                    retained = max(0, limit - counts[name])
                    outputs[name].write(data[:retained])
                    counts[name] += len(data)
                    if counts[name] > limit:
                        fail(name + " stream overflow")
                        break
                    if observer:
                        pending_lines[name] += data
                        while b"\n" in pending_lines[name]:
                            line, _, pending_lines[name] = pending_lines[name].partition(b"\n")
                            observed = observer(name, line)
                            if observed:
                                fail(observed)
                                break
                        if result["reason"] is not None:
                            break
                if result["reason"] is not None:
                    break
            if result["reason"] is None and proc.poll() != 0:
                fail("nonzero exit")
    except OSError:
        fail("spawn/transport failure")
    except Exception:
        fail("transport exception")
    finally:
        if proc is not None:
            # Signal even when the leader exited: children may still hold pipes.
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except OSError:
                result["cleanupError"] = "group termination failed"
                fail("cleanup failure")
            try:
                result["exitCode"] = proc.wait(timeout=5)
                result["reaped"] = True
            except (OSError, subprocess.TimeoutExpired):
                result["cleanupError"] = "leader was not reaped"
                fail("cleanup failure")
            for pipe in (proc.stdout, proc.stderr):
                if pipe is not None:
                    pipe.close()
        if selector is not None:
            selector.close()
        for output in outputs.values():
            output.close()
        result["elapsedSeconds"] = round(time.monotonic() - started, 3)
        result["retainedStreamBytes"] = {
            name: min(limit, count) for name, count in counts.items()
        }
    return result

def _check_bindings(plan, case):
    """Rescan full inventories: checking only old rows misses added files."""
    bindings = plan["bindings"]
    repo = _path(plan["repo"], "repo")
    ignored, ignore_record = _ignored_dirs(repo)
    if ([".git"] + sorted(ignored) != plan["excludedPaths"]
            or ignore_record != bindings["gitignore"]
            or _tree(repo, ignored | {".git"}) != bindings["source"]):
        _bad("source inventory drift")
    keys = ("registry", "node", "cli", "extension", "package", "lock",
            "sdkPackage", "harness")
    if not _same([bindings[key] for key in keys]):
        _bad("runtime or input drift")
    if _catalog(plan["skillsRoot"]) != bindings["catalog"]:
        _bad("source catalog drift")
    for candidate in plan["cases"]:
        for key in ("workspace", "home", "storage"):
            directory = _path(candidate[key], key)
            if not directory.is_dir() or stat.S_IMODE(directory.stat().st_mode) != 0o700:
                _bad("private directory drift")
        if not _same([candidate["wrapper"]]):
            _bad("wrapper drift")
        if _tree(candidate["workspace"]) != candidate["manifest"]:
            _bad("fixture inventory drift")
        copied = [file for skill in candidate["catalogCopy"] for file in skill["files"]]
        actual = _tree(pathlib.Path(candidate["home"]) / ".agents" / "skills")
        if sorted(actual, key=lambda x: x["path"]) != sorted(copied, key=lambda x: x["path"]):
            _bad("copied catalog drift")
        credentials = _path(candidate["credentialFile"], "isolated credentials", False)
        if credentials.exists():
            _bad("unexpected isolated credential file")
    _path(plan["authDir"], "normal account directory")  # Never read its contents.


def _validate_plan(plan, root):
    """Only accept the evaluator's schema and confined per-case paths."""
    if (not isinstance(plan, dict) or type(plan.get("schema")) is not int
            or plan["schema"] != 1 or plan.get("kind") != "pi-public-development"):
        _bad("invalid plan schema")
    for key in ("provider", "model"):
        value = plan.get(key)
        if not isinstance(value, str) or not value or len(value) > 256 or value.startswith("-"):
            _bad("invalid plan target")
    if plan["model"].lower() in ("gpt-5.4", "gpt-5.5") or plan.get("thinking") not in ("low", "medium", "high"):
        _bad("invalid plan model or effort")
    cases = plan.get("cases")
    if not isinstance(cases, list) or not cases:
        _bad("invalid plan cases")
    seen = set()
    for case in cases:
        name = case.get("id") if isinstance(case, dict) else None
        if (not isinstance(name, str) or not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,63}", name)
                or name in seen):
            _bad("invalid plan case identity")
        seen.add(name)
        prompt = case.get("prompt")
        if not isinstance(prompt, str) or not prompt.strip() or len(prompt.encode()) > 32768 or prompt.lstrip().startswith(("-", "@", "/")):
            _bad("invalid plan prompt")
        private = root / "private" / name
        paths = {"home": private / "home", "workspace": private / "workspace",
                 "storage": private / "storage", "credentialFile": private / "missing-credentials.json",
                 "guardLog": private / "guard.jsonl"}
        for key, expected in paths.items():
            if case.get(key) != str(expected):
                _bad("unconfined plan path")
            _path(expected, key, key not in ("credentialFile", "guardLog"))
        if case["wrapper"]["path"] != str(private / "wrapper.mjs"):
            _bad("unconfined wrapper")
        if (type(case.get("timeout")) is not int or not 1 <= case["timeout"] <= 120
                or case.get("stdoutLimitBytes") != MAX_STREAM
                or case.get("stderrLimitBytes") != MAX_STREAM):
            _bad("invalid plan budget")
        expected_argv = [plan["bindings"]["node"]["path"], plan["bindings"]["cli"]["path"],
                         "--print", "--mode", "json", "--no-session", "--offline",
                         "--no-extensions", "--extension", case["wrapper"]["path"],
                         "--no-skills", "--no-prompt-templates", "--no-themes",
                         "--no-context-files", "--approve", "--tools", "read,vgx_skill",
                         "--provider", plan["provider"], "--model", plan["model"],
                         "--thinking", plan["thinking"], case["prompt"]]
        if case["argv"] != expected_argv:
            _bad("plan argv mismatch")


def run(a):
    if not a.allow_live:
        _bad("--allow-live required")
    if sys.platform != "linux":
        _bad("Linux only")
    pp = _path(a.plan, "plan")
    raw = pp.read_bytes()
    if sha(raw) != a.plan_sha256:
        _bad("plan digest mismatch")
    plan = json.loads(raw)
    _validate_plan(plan, pp.parent)
    case = next((case for case in plan["cases"] if case["id"] == a.case), None)
    if case is None:
        _bad("unknown case")
    rd = _path(pp.parent / "runs" / a.case, "run output", False)
    # An attempt remains reserved even when preflight fails; never overwrite.
    rd.mkdir(mode=0o700)
    _write_new(rd / "run.started", b"reserved\n")
    rec = {"schema": 1, "status": "failed-ungraded", "case": a.case,
           "planSha256": sha(raw), "argv": case["argv"], "bindings": plan["bindings"],
           "fixtureBefore": case["manifest"], "catalogCopy": case["catalogCopy"],
           "requestedProvider": plan["provider"], "requestedModel": plan["model"],
           "requestedEffort": plan["thinking"], "effectiveEffort": None,
           "startedAtUnix": time.time(), "reason": None, "runtimeProbes": [],
           "usage": "estimated/unobserved", "timeoutSeconds": case["timeout"]}

    def fail(reason):
        if rec["reason"] is None:
            rec["reason"] = reason

    def probe(label, deadline, env):
        stdout = rd / (label + ".stdout")
        stderr = rd / (label + ".stderr")
        result = _process([plan["bindings"]["node"]["path"],
                           plan["bindings"]["cli"]["path"], "--version"],
                          case["workspace"], env, stdout, stderr, deadline)
        observed = stdout.read_text(encoding="utf-8").strip() if stdout.exists() else None
        rec["runtimeProbes"].append({"label": label, "observedVersion": observed,
                                     "process": result})
        if result["reason"] or observed != plan["expectedVersion"]:
            _bad("version probe failed or mismatched")

    try:
        _check_bindings(plan, case)
        guard = _path(case["guardLog"], "guard log", False)
        _write_new(guard, b"")
        env = dict(os.environ)
        env.update(HOME=case["home"], PI_CODING_AGENT_DIR=plan["authDir"],
                   PI_OFFLINE="1", PI_TELEMETRY="0")
        # Explicit wrapper credentialFile is isolated; no inherited secret path is used.
        deadline = time.monotonic() + case["timeout"]
        probe("version-before", deadline, env)
        process = _process(case["argv"], case["workspace"], env,
                           rd / "stdout.ndjson", rd / "stderr.log", deadline,
                           observer=lambda stream, line: _early_failure(
                               stream, line, plan["provider"], plan["model"]))
        rec["process"] = process
        if process["reason"]:
            fail(process["reason"])
        try:
            text = (rd / "stdout.ndjson").read_text(encoding="utf-8")
            reason, parsed = _events(text, plan["provider"], plan["model"], plan["thinking"])
            if reason:
                fail(reason)
            elif parsed:
                rec.update(parsed)
        except (OSError, UnicodeDecodeError):
            fail("unreadable or invalid UTF-8 trace")
        stderr = (rd / "stderr.log").read_text(encoding="utf-8", errors="replace")
        if "EVALUATOR_FATAL" in stderr or "Extension error (" in stderr:
            fail("extension guard/load failure")
        if guard.stat().st_size > MAX_STREAM:
            fail("guard log overflow")
        else:
            entries = [json.loads(line) for line in guard.read_text(encoding="utf-8").splitlines()]
            rec["guardEvidence"] = entries
            if not any(isinstance(entry, dict) and entry.get("systemPromptSha256") for entry in entries):
                fail("missing catalog observation")
            if any(isinstance(entry, dict) and entry.get("reason") for entry in entries):
                fail("guard failure")
        # A probe is diagnostic, never a new model attempt. Same overall deadline.
        if time.monotonic() < deadline:
            probe("version-after", deadline, env)
        else:
            fail("runtime post-probe unavailable within budget")
    except Exception as error:
        # Keep fixed failure classes, not exception text containing arbitrary paths/data.
        fail("preflight or transport exception: " + type(error).__name__)
    finally:
        try:
            _check_bindings(plan, case)
            rec["postIdentity"] = "matched"
            rec["fixtureAfter"] = _tree(case["workspace"])
        except Exception:
            rec["postIdentity"] = "drift-or-unavailable"
            fail("post-run identity drift")
        rec["finishedAtUnix"] = time.time()
        rec["streamHashes"] = {}
        for path in sorted(rd.iterdir()):
            if path.name.endswith((".ndjson", ".log", ".stdout", ".stderr")):
                rec["streamHashes"][path.name] = _record(path)
        if rec["reason"] is None:
            rec["status"] = "completed-ungraded"
        _write_new(rd / "receipt.json", (json.dumps(rec, sort_keys=True, indent=2) + "\n").encode())
    return 0 if rec["status"] == "completed-ungraded" else 2

def self_test():
    """Check protocol invariants offline; no target process or account access."""
    final = {"type": "message_end", "message": {"role": "assistant",
             "provider": "fixture", "model": "fixture", "stopReason": "stop",
             "content": [{"type": "text", "text": "ok"}]}}
    events = [final, {"type": "agent_end", "willRetry": False}, {"type": "agent_settled"}]
    trace = "\n".join(json.dumps(event) for event in events)
    if _events(trace, "fixture", "fixture")[0] is not None:
        _bad("self-test completion failed")
    if _early_failure("stdout", b"{bad", "fixture", "fixture") != "malformed NDJSON":
        _bad("self-test malformed trace failed")
    if _events('{"type":"error"}\n' + trace, "fixture", "fixture")[0] is None:
        _bad("self-test failure preservation failed")
    print(VERSION + " offline self-test OK")
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", action="version", version=VERSION)
    commands = parser.add_subparsers(dest="cmd", required=True)
    commands.add_parser("self-test", help="check protocol invariants without starting Pi")
    prepare = commands.add_parser("plan")
    for name in ("repo", "registry", "node", "cli", "expected-version", "provider",
                 "model", "thinking", "auth-dir", "skills-root", "output"):
        prepare.add_argument("--" + name, required=True)
    prepare.add_argument("--extension")
    prepare.add_argument("--case", action="append")
    prepare.add_argument("--timeout", default="120")
    execute = commands.add_parser("run")
    execute.add_argument("--allow-live", action="store_true")
    for name in ("plan", "plan-sha256", "case"):
        execute.add_argument("--" + name, required=True)
    args = parser.parse_args(argv)
    try:
        if args.cmd == "self-test":
            return self_test()
        if args.cmd == "plan":
            plan(args)
            output = _path(args.output, "output")
            print(str(output / "plan.json"))
            print((output / "plan.sha256").read_text().strip())
            return 0
        return run(args)
    except (ValueError, OSError, KeyError, TypeError) as error:
        print("pi-eval: " + str(error), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

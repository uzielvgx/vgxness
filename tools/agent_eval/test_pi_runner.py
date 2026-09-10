import argparse, hashlib, json, os, pathlib, shutil, subprocess, tempfile, time, unittest, sys
from unittest import mock
sys.path.insert(0,str(pathlib.Path(__file__).parent)); import pi_runner
NODE=shutil.which('node')
class T(unittest.TestCase):
 def test_plan_and_gate(self):
  self.assertEqual(64, pi_runner.MAX_CALLS)
 def test_events_normal_completion(self):
  trace='{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"p","model":"m","thinkingLevel":"low","content":[{"type":"text","text":"ok"}],"usage":{"input":1,"cost":0.02}}}\n{"type":"agent_end","willRetry":false}\n{"type":"agent_settled"}'
  reason,value=pi_runner._events(trace,'p','m','low');self.assertIsNone(reason);self.assertEqual('ok',value['finalAssistant']);self.assertEqual('p',value['actualProvider']);self.assertEqual('low',value['observedEffort'])
 def test_events_error_even_if_later_good_fails(self):
  self.assertIsNotNone(pi_runner._events('{"type":"error"}\n{"type":"agent_end"}\n{"type":"agent_settled"}','p','m')[0])
 def test_events_model_mismatch_fails(self):
  self.assertEqual('provider/model mismatch',pi_runner._events('{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"x","model":"m","content":[{"type":"text","text":"x"}]}}','p','m')[0])
 def test_events_retry_fails(self):
  self.assertEqual('agent retry',pi_runner._events('{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"p","model":"m","content":[{"type":"text","text":"x"}]}}\n{"type":"agent_end","willRetry":true}','p','m')[0])
 def test_events_wrong_order_fails(self):
  self.assertEqual('agent settled before nonretry end',pi_runner._events('{"type":"agent_settled"}','p','m')[0])
 def test_events_malformed_nonobject_fails(self):
  self.assertEqual('malformed event',pi_runner._events('[]','p','m')[0])
 def test_events_missing_text_fails(self):
  self.assertEqual('assistant final text missing',pi_runner._events('{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"p","model":"m","content":[]}}','p','m')[0])
 def test_events_tool_call_text_is_not_final_text(self):
  trace='{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"p","model":"m","content":[{"type":"toolCall","text":"not a response"}]}}'
  self.assertEqual('assistant final text missing',pi_runner._events(trace,'p','m')[0])
 def test_events_tool_use_then_final_succeeds(self):
  trace='{"type":"message_end","message":{"role":"assistant","stopReason":"toolUse","provider":"p","model":"m","content":[{"type":"toolCall"}]}}\n{"type":"tool_execution_end","isError":false}\n{"type":"message_end","message":{"role":"assistant","stopReason":"length","provider":"p","model":"m","content":[{"type":"text","text":"final"}]}}\n{"type":"agent_end","willRetry":false}\n{"type":"agent_settled"}'
  self.assertIsNone(pi_runner._events(trace,'p','m')[0])
 def test_events_failure_after_prior_end_fails(self):
  trace='{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"p","model":"m","content":[{"type":"text","text":"old"}]}}\n{"type":"agent_end","willRetry":false}\n{"type":"agent_settled"}\n{"type":"message_end","message":{"role":"assistant","stopReason":"error","provider":"p","model":"m","content":[]}}'
  self.assertEqual('assistant error or aborted',pi_runner._events(trace,'p','m')[0])
 def test_events_malformed_content_fails_safely(self):
  self.assertEqual('assistant content malformed',pi_runner._events('{"type":"message_end","message":{"role":"assistant","stopReason":"stop","provider":"p","model":"m","content":{}}}','p','m')[0])
 def test_events_failed_tool_fails(self):
  self.assertEqual('failed tool',pi_runner._events('{"type":"tool_execution_end","isError":true}','p','m')[0])
 def test_path_rejects_dangling_symlink_ancestor(self):
  with tempfile.TemporaryDirectory() as d:
   d=pathlib.Path(d);(d/'link').symlink_to(d/'missing');self.assertRaises(ValueError,pi_runner._path,d/'link'/'x','x',False)
 def test_tree_rejects_directory_symlink(self):
  with tempfile.TemporaryDirectory() as d:
   d=pathlib.Path(d);(d/'link').symlink_to(d,target_is_directory=True);self.assertRaises(ValueError,pi_runner._tree,d)
 def test_tree_ignores_known_state_and_includes_new_file_mode(self):
  with tempfile.TemporaryDirectory() as d:
   d=pathlib.Path(d);(d/'.gitignore').write_text('.atl/\n.codegraph/\nagent-eval-output/\ntools/agent_eval/__pycache__/\n\nnode_modules/\n');(d/'.atl').mkdir();(d/'.atl'/'private').write_text('never');(d/'new').write_text('yes');os=__import__('os');os.chmod(d/'new',0o640);ignored,_=pi_runner._ignored_dirs(d);rows=pi_runner._tree(d,ignored);self.assertTrue(any(x['path'].endswith('/new') and x['mode']==0o640 for x in rows));self.assertFalse(any('.atl' in x['path'] for x in rows))
 def test_registry_rejects_protected_duplicates_and_unsafe_prompt(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'r';p.write_text(json.dumps({'schema':1,'partition':'public-development','cases':[{'id':'a','prompt':'@x'}]}));self.assertRaises(ValueError,pi_runner._registry,p)
   p.write_text(json.dumps({'schema':1,'partition':'public-development','cases':[{'id':'a','prompt':'x'},{'id':'a','prompt':'x'}]}));self.assertRaises(ValueError,pi_runner._registry,p)
 def test_registry_accepts_actual_partition_and_gpt_notes(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'r';p.write_text(json.dumps({'schema':1,'partition':'public development, not protected holdout','notes':'gpt-5.5 excluded elsewhere','cases':[{'id':'a','prompt':'x'}]}));self.assertEqual('a',pi_runner._registry(p)['cases'][0]['id'])
 def test_registry_rejects_unsafe_fixture(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'r';p.write_text(json.dumps({'schema':1,'partition':'public-development','cases':[{'id':'a','prompt':'x','fixtureFiles':{'.pi/settings.json':'x'}}]}));self.assertRaises(ValueError,pi_runner._registry,p)
 def test_catalog_filters_incompatible_and_binds_resources(self):
  with tempfile.TemporaryDirectory() as d:
   d=pathlib.Path(d);(d/'good').mkdir();(d/'bad').mkdir();(d/'good'/'SKILL.md').write_text('name: good\ncompatibility: Agent Skills hosts\n<!-- managed-by: vgxness -->');(d/'good'/'r.txt').write_text('r');(d/'bad'/'SKILL.md').write_text('name: bad\ncompatibility: other\n<!-- managed-by: vgxness -->');rows=pi_runner._catalog(d);self.assertEqual(['good'],[x['name'] for x in rows]);self.assertEqual('r.txt',pathlib.Path(rows[0]['resources'][0]['path']).name)
class PlannerFixture(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.root=pathlib.Path(self.temp.name);self.repo=self.root/'repo';self.repo.mkdir();(self.repo/'.gitignore').write_text('.atl/\n.codegraph/\nagent-eval-output/\ntools/agent_eval/__pycache__/\n\nnode_modules/\n');(self.repo/'.atl').mkdir();(self.repo/'.atl'/'secret').write_text('private');(self.repo/'new.txt').write_text('new')
  ext=self.repo/'packages/pi/src/extension.ts';ext.parent.mkdir(parents=True);ext.write_text('export default {}');pkg=self.repo/'packages/pi/package.json';pkg.parent.mkdir(parents=True,exist_ok=True);pkg.write_text('{"name":"@vgxness/pi","version":"0.84.4"}');(self.repo/'package-lock.json').write_text('{}')
  self.node=self.root/'node';self.node.write_text('#!/bin/sh\n');os.chmod(self.node,0o700);self.cli=self.root/'node_modules/@earendil-works/pi-coding-agent/dist/cli.js';self.cli.parent.mkdir(parents=True);self.cli.write_text('cli');(self.cli.parent.parent/'package.json').write_text('{"name":"@earendil-works/pi-coding-agent","version":"0.84.4"}');self.auth=self.root/'auth';self.auth.mkdir();self.skills=self.root/'skills'
  for name in ('one','two'):
   d=self.skills/name;d.mkdir(parents=True);(d/'SKILL.md').write_text('name: '+name+'\ncompatibility: Agent Skills hosts\n<!-- managed-by: vgxness -->');(d/'guide.txt').write_text(name)
  d=self.skills/'bad';d.mkdir();(d/'SKILL.md').write_text('name: bad\ncompatibility: other\n')
  self.registry=self.root/'registry.json';self.registry.write_text(json.dumps({'schema':1,'partition':'public-development','notes':'GPT-5.5 exclusion note','cases':[{'id':'case-a','prompt':'read A','fixtureFiles':{'a.txt':'A'},'expectedSkill':'one','rubric':{'secret':'no'}},{'id':'case-b','prompt':'read B','fixtureFiles':{'b.txt':'B'}}]}))
 def tearDown(self):self.temp.cleanup()
 def args(self,out,cases=None,model='m'):
  return argparse.Namespace(repo=str(self.repo),registry=str(self.registry),node=str(self.node),cli=str(self.cli),expected_version='0.84.4',provider='p',model=model,thinking='low',auth_dir=str(self.auth),skills_root=str(self.skills),extension=None,output=str(out),timeout='120',case=cases)
 def test_plan_has_zero_probes_and_per_case_isolation(self):
  out=self.root/'out'
  with mock.patch.object(pi_runner.subprocess,'run',side_effect=AssertionError('probe')): plan=pi_runner.plan(self.args(out,['case-a','case-b']))
  self.assertEqual(['case-a','case-b'],[x['id'] for x in plan['cases']]);self.assertNotEqual(plan['cases'][0]['home'],plan['cases'][1]['home'])
  for case in plan['cases']:
   self.assertEqual(['one','two'],[x['name'] for x in case['catalogCopy']]);self.assertEqual(case['prompt'],case['argv'][-1]);self.assertNotIn('secret',' '.join(case['argv']))
 def test_plan_digest_permissions_and_ignore(self):
  previous=os.umask(0o002);os.umask(previous);out=self.root/'out';pi_runner.plan(self.args(out,['case-a']));raw=(out/'plan.json').read_bytes();self.assertEqual(hashlib.sha256(raw).hexdigest(),(out/'plan.sha256').read_text().strip());self.assertEqual(previous,os.umask(previous));os.umask(previous)
  self.assertEqual(0o700,stat_mode(out));self.assertEqual(0o600,stat_mode(out/'plan.json'));self.assertFalse(any('.atl' in x['path'] for x in json.loads(raw)['bindings']['source']))
 def test_invalid_selection_output_and_model_do_not_create(self):
  out=self.root/'out';self.assertRaises(ValueError,pi_runner.plan,self.args(out,['unknown']));self.assertFalse(out.exists());self.assertRaises(ValueError,pi_runner.plan,self.args(self.root/'out2',None,'gpt-5.5'));self.assertFalse((self.root/'out2').exists())
 def test_catalog_copy_hash_and_bound_extension(self):
  out=self.root/'out';plan=pi_runner.plan(self.args(out,['case-a']));copy=plan['cases'][0]['catalogCopy'][0]['files'][0];self.assertEqual(copy['sha256'],hashlib.sha256(pathlib.Path(copy['path']).read_bytes()).hexdigest());self.assertEqual(str(self.repo/'packages/pi/src/extension.ts'),plan['bindings']['extension']['path'])
 def test_runtime_metadata_mismatch_fails_before_output(self):
  (self.cli.parent.parent/'package.json').write_text('{"name":"wrong","version":"0"}');out=self.root/'out';self.assertRaises(ValueError,pi_runner.plan,self.args(out));self.assertFalse(out.exists())
 def test_plan_records_runtime_metadata_and_paths(self):
  out=self.root/'out';plan=pi_runner.plan(self.args(out));self.assertEqual(str(self.repo),plan['repo']);self.assertIn('sdkPackage',plan['bindings']);self.assertIn('lock',plan['bindings'])
 def test_generated_wrapper_loads_real_extension_with_mock_pi(self):
  workspace=self.root/'workspace';storage=self.root/'storage';workspace.mkdir();storage.mkdir();(workspace/'read.txt').write_text('ok')
  skills=self.root/'copied/.agents/skills/fixture';skills.mkdir(parents=True);manifest=skills/'SKILL.md';manifest.write_text('name: fixture\ncompatibility: Agent Skills hosts\n<!-- managed-by: vgxness -->')
  copied=[{'name':'fixture','files':[{'path':str(manifest),'sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),'mode':0o600}]}]
  extension=pathlib.Path(__file__).parents[2]/'packages/pi/src/extension.ts';wrapper=self.root/'wrapper.mjs';wrapper.write_text(pi_runner._wrapper(extension,workspace,storage,self.root/'missing.json',copied,self.root/'guard.jsonl'))
  script=self.root/'mock.mjs';script.write_text("""import {pathToFileURL} from 'node:url';const handlers={};const pi={on:(n,h)=>{(handlers[n]??=[]).push(h)},registerTool:()=>{},registerCommand:()=>{},getCommands:()=>[],appendEntry:()=>{}};const {default:extension}=await import(pathToFileURL(process.argv[2]).href);await extension(pi);for(const h of handlers.tool_call)h({toolName:'read',input:{path:'read.txt'}});for(const h of handlers.tool_call)h({toolName:'vgx_skill',input:{operation:'list'}});for(const h of handlers.before_agent_start)await h({systemPrompt:process.argv[3]});console.log('wrapper-ok');""")
  catalog='<available_skills><skill><name>fixture</name><location>'+str(manifest)+'</location></skill></available_skills>'
  result=subprocess.run([NODE,'--experimental-strip-types',str(script),str(wrapper),catalog],cwd=self.root,env={'HOME':str(self.root/'home'),'PATH':os.environ['PATH'],'PI_CODING_AGENT_DIR':str(self.root/'home')},capture_output=True,text=True,timeout=15)
  self.assertEqual(0,result.returncode,result.stderr);self.assertIn('wrapper-ok',result.stdout);self.assertIn('systemPromptSha256',(self.root/'guard.jsonl').read_text())
 def test_generated_wrapper_denies_forbidden_tool_in_real_node(self):
  workspace=self.root/'workspace';storage=self.root/'storage';workspace.mkdir();storage.mkdir();skills=self.root/'copied/.agents/skills/fixture';skills.mkdir(parents=True);manifest=skills/'SKILL.md';manifest.write_text('name: fixture\ncompatibility: Agent Skills hosts\n<!-- managed-by: vgxness -->');copied=[{'name':'fixture','files':[{'path':str(manifest),'sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),'mode':0o600}]}];extension=pathlib.Path(__file__).parents[2]/'packages/pi/src/extension.ts';wrapper=self.root/'wrapper.mjs';wrapper.write_text(pi_runner._wrapper(extension,workspace,storage,self.root/'missing.json',copied,self.root/'guard.jsonl'));script=self.root/'deny.mjs';script.write_text("""import {pathToFileURL} from 'node:url';const h={};const pi={on:(n,f)=>{(h[n]??=[]).push(f)},registerTool:()=>{},registerCommand:()=>{},getCommands:()=>[],appendEntry:()=>{}};const {default:e}=await import(pathToFileURL(process.argv[2]).href);await e(pi);for(const f of h.tool_call)f({toolName:'task',input:{}});""");result=subprocess.run([NODE,'--experimental-strip-types',str(script),str(wrapper)],cwd=self.root,env={'HOME':str(self.root/'home'),'PATH':os.environ['PATH'],'PI_CODING_AGENT_DIR':str(self.root/'home')},capture_output=True,text=True,timeout=15);self.assertEqual(78,result.returncode);self.assertIn('tool denied task',(self.root/'guard.jsonl').read_text())
 def test_real_node_wrapper_matrix(self):
  cases={'fixture-read':0,'absolute-fixture-read':0,'paged-fixture-read':0,'manifest-read':0,'resource-read':0,'vgxread-known-resource':0,'outside-read':78,'symlink-read':78,'unknownskill':78,'unknownresource':78,'64calls':0,'65calls':78,'exactcatalog':0,'missingcatalog':78,'extracatalog':78,'duplicatecatalog':78,'changedmanifest':78,'changedresource':78}
  for scenario,expected in cases.items():
   with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as d:
    d=pathlib.Path(d);workspace=d/'workspace';storage=d/'storage';workspace.mkdir();storage.mkdir();(workspace/'ok.txt').write_text('ok')
    skill=d/'skills/fixture';skill.mkdir(parents=True);manifest=skill/'SKILL.md';resource=skill/'refs/guide.txt';resource.parent.mkdir()
    manifest.write_text('name: fixture\ncompatibility: Agent Skills hosts\n<!-- managed-by: vgxness -->');resource.write_text('guide')
    copied=[{'name':'fixture','files':[{'path':str(manifest),'sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),'mode':0o600},{'path':str(resource),'sha256':hashlib.sha256(resource.read_bytes()).hexdigest(),'mode':0o600}]}]
    wrapper=d/'wrapper.mjs';wrapper.write_text(pi_runner._wrapper(pathlib.Path(__file__).parents[2]/'packages/pi/src/extension.ts',workspace,storage,d/'missing.json',copied,d/'guard.jsonl'))
    payload={'scenario':scenario,'workspace':str(workspace),'manifest':str(manifest),'resource':str(resource),'catalog':'<available_skills><skill><name>fixture</name><description>x</description><location>'+str(manifest)+'</location></skill></available_skills>'}
    script=d/'matrix.mjs';script.write_text("""import{pathToFileURL}from'node:url';import{symlinkSync,writeFileSync}from'node:fs';const h={};const pi={on:(n,f)=>(h[n]??=[]).push(f),registerTool:()=>{},registerCommand:()=>{},getCommands:()=>[],appendEntry:()=>{}};const p=JSON.parse(process.argv[3]);const{default:e}=await import(pathToFileURL(process.argv[2]).href);await e(pi);let catalog=p.catalog;if(p.scenario==='missingcatalog')catalog='<available_skills></available_skills>';if(p.scenario==='extracatalog')catalog=catalog.replace('</available_skills>','<skill><name>other</name><location>/tmp/other</location></skill></available_skills>');if(p.scenario==='duplicatecatalog')catalog=catalog.replace('</available_skills>','<skill><name>fixture</name><location>'+p.manifest+'</location></skill></available_skills>');if(p.scenario==='changedmanifest')writeFileSync(p.manifest,'changed');if(p.scenario==='changedresource')writeFileSync(p.resource,'changed');if(p.scenario==='symlink-read')symlinkSync('ok.txt',new URL('./link.txt',pathToFileURL(process.cwd()+'/workspace/')));for(const f of h.before_agent_start||[])await f({systemPrompt:catalog});let event={toolName:'read',input:{path:'ok.txt'}};if(p.scenario==='absolute-fixture-read')event={toolName:'read',input:{path:p.workspace+'/ok.txt'}};if(p.scenario==='paged-fixture-read')event={toolName:'read',input:{path:'ok.txt',offset:1,limit:1}};if(p.scenario==='outside-read')event={toolName:'read',input:{path:'/etc/passwd'}};if(p.scenario==='symlink-read')event={toolName:'read',input:{path:'link.txt'}};if(p.scenario==='unknownskill')event={toolName:'vgx_skill',input:{operation:'read',name:'other'}};if(p.scenario==='unknownresource')event={toolName:'vgx_skill',input:{operation:'read',name:'fixture',resources:['bad.txt']}};if(p.scenario==='manifest-read')event={toolName:'read',input:{path:p.manifest}};if(p.scenario==='resource-read')event={toolName:'read',input:{path:p.resource}};if(p.scenario==='vgxread-known-resource')event={toolName:'vgx_skill',input:{operation:'read',name:'fixture',resources:['refs/guide.txt']}};const count=p.scenario==='65calls'?65:p.scenario==='64calls'?64:1;for(let i=0;i<count;i++)for(const f of h.tool_call||[])f(event);console.log('wrapper-ok');""")
    result=subprocess.run([NODE,'--experimental-strip-types',str(script),str(wrapper),json.dumps(payload)],cwd=d,env={'HOME':str(d/'home'),'PATH':os.environ['PATH'],'PI_CODING_AGENT_DIR':str(d/'home')},capture_output=True,text=True,timeout=15)
    self.assertEqual(expected,result.returncode,result.stderr)
    if expected==0:self.assertIn('wrapper-ok',result.stdout);self.assertIn('systemPromptSha256',(d/'guard.jsonl').read_text())
class ProcessTests(unittest.TestCase):
    def call_child(self, code, timeout=0.25, limit=4096):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            result = pi_runner._process(
                [sys.executable, '-c', code], root, dict(os.environ),
                root / 'stdout', root / 'stderr', time.monotonic() + timeout, limit)
            return result, (root / 'stdout').read_bytes(), (root / 'stderr').read_bytes()

    def test_success_and_nonzero_are_reaped(self):
        result, stdout, stderr = self.call_child("print('ok')")
        self.assertIsNone(result['reason'])
        self.assertTrue(result['reaped'])
        self.assertEqual(b'ok\n', stdout)
        self.assertEqual(b'', stderr)
        result, _, _ = self.call_child('raise SystemExit(7)')
        self.assertEqual('nonzero exit', result['reason'])
        self.assertEqual(7, result['exitCode'])
        self.assertTrue(result['reaped'])

    def test_each_stream_is_bounded_and_terminated(self):
        for descriptor, name in ((1, 'stdout'), (2, 'stderr')):
            with self.subTest(name=name):
                result, stdout, stderr = self.call_child(
                    f"import os,time; os.write({descriptor},b'x'*100000); time.sleep(60)")
                self.assertEqual(name + ' stream overflow', result['reason'])
                self.assertTrue(result['reaped'])
                self.assertLessEqual(len(stdout), 4096)
                self.assertLessEqual(len(stderr), 4096)

    def test_eof_does_not_disable_deadline(self):
        result, _, _ = self.call_child('import os,time; os.close(1); os.close(2); time.sleep(60)')
        self.assertEqual('timeout', result['reason'])
        self.assertTrue(result['reaped'])
        self.assertLess(result['elapsedSeconds'], 2)

    def test_descendant_holding_pipes_is_killed(self):
        code = ("import subprocess,sys; p=subprocess.Popen([sys.executable,'-c',"
                "'import time; time.sleep(60)']); print(p.pid,flush=True)")
        result, stdout, _ = self.call_child(code)
        self.assertEqual('timeout', result['reason'])
        self.assertTrue(result['reaped'])
        child = int(stdout)
        # An orphan may briefly remain a zombie until init reaps it; it must not run.
        for _ in range(50):
            status = pathlib.Path('/proc') / str(child) / 'stat'
            if not status.exists() or status.read_text().split(') ', 1)[1].startswith('Z '):
                break
            time.sleep(0.01)
        else:
            self.fail('descendant is still running')

    def test_spawn_failure_and_expired_deadline_do_not_leak(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            result = pi_runner._process(['/nonexistent-pi-test-executable'], root,
                                        dict(os.environ), root / 'out', root / 'err',
                                        time.monotonic() + 1)
            self.assertEqual('spawn/transport failure', result['reason'])
            self.assertIsNone(result['exitCode'])
            with mock.patch.object(pi_runner.subprocess, 'Popen', side_effect=AssertionError('must not spawn')):
                result = pi_runner._process([sys.executable], root, dict(os.environ),
                                            root / 'out2', root / 'err2', time.monotonic() - 1)
            self.assertEqual('timeout', result['reason'])

class RunTests(unittest.TestCase):
    setUp = PlannerFixture.setUp
    tearDown = PlannerFixture.tearDown
    args = PlannerFixture.args
    def prepare_run(self, mode='ok'):
        # This executable is a local Python stand-in, never Pi/a model process.
        self.cli.write_text(mode)
        self.node.write_text('''#!/usr/bin/python3
import hashlib,json,os,pathlib,sys
if '--version' in sys.argv:
 print('0.84.4'); raise SystemExit(0)
mode=pathlib.Path(sys.argv[1]).read_text()
wrapper=pathlib.Path(sys.argv[sys.argv.index('--extension')+1])
guard=wrapper.parent/'guard.jsonl'
guard.write_text(json.dumps({'systemPromptSha256':'0'*64})+'\\n')
if mode=='post-drift': pathlib.Path('added.txt').write_text('drift')
if mode=='stderr-error': print('EVALUATOR_FATAL test',file=sys.stderr)
message={'role':'assistant','stopReason':'error' if mode=='assistant-error' else 'stop','provider':'p','model':'m','content':[{'type':'text','text':'ok'}]}
for event in [{'type':'message_end','message':message},{'type':'agent_end','willRetry':False},{'type':'agent_settled'}]: print(json.dumps(event))
''')
        os.chmod(self.node, 0o700)
        output = self.root / 'out'
        pi_runner.plan(self.args(output, ['case-a']))
        return argparse.Namespace(allow_live=True, plan=str(output / 'plan.json'),
                                  plan_sha256=(output / 'plan.sha256').read_text().strip(), case='case-a')

    def receipt(self):
        return json.loads((self.root / 'out/runs/case-a/receipt.json').read_text())

    def test_live_gate_and_digest_prevent_any_spawn(self):
        args = self.prepare_run()
        with mock.patch.object(pi_runner.subprocess, 'Popen', side_effect=AssertionError('spawn')):
            args.allow_live = False
            self.assertRaises(ValueError, pi_runner.run, args)
            args.allow_live = True
            args.plan_sha256 = '0' * 64
            self.assertRaises(ValueError, pi_runner.run, args)
        self.assertFalse((self.root / 'out/runs/case-a').exists())

    def test_completed_ungraded_receipt_and_exclusive_attempt(self):
        args = self.prepare_run()
        with mock.patch.dict(os.environ, {'TEST_PRIVATE_SENTINEL': 'not-for-receipt'}):
            self.assertEqual(0, pi_runner.run(args))
        receipt = self.receipt()
        self.assertEqual('completed-ungraded', receipt['status'])
        self.assertEqual('matched', receipt['postIdentity'])
        self.assertIsNone(receipt['effectiveEffort'])
        self.assertNotIn('not-for-receipt', json.dumps(receipt))
        self.assertEqual(2, len(receipt['runtimeProbes']))
        self.assertRaises((FileExistsError, ValueError), pi_runner.run, args)

    def test_prelaunch_new_file_drift_retains_receipt_without_spawn(self):
        args = self.prepare_run()
        (self.repo / 'new-after-plan').write_text('unexpected')
        with mock.patch.object(pi_runner.subprocess, 'Popen', side_effect=AssertionError('spawn')):
            self.assertEqual(2, pi_runner.run(args))
        self.assertEqual('failed-ungraded', self.receipt()['status'])
        self.assertNotIn('process', self.receipt())

    def test_postlaunch_new_fixture_file_fails_with_receipt(self):
        self.assertEqual(2, pi_runner.run(self.prepare_run('post-drift')))
        self.assertEqual('post-run identity drift', self.receipt()['reason'])

    def test_rczero_assistant_error_remains_failure(self):
        self.assertEqual(2, pi_runner.run(self.prepare_run('assistant-error')))
        self.assertEqual('assistant error or aborted', self.receipt()['process']['reason'])
        self.assertEqual('assistant error or aborted', self.receipt()['reason'])

    def test_rczero_guard_error_remains_failure(self):
        self.assertEqual(2, pi_runner.run(self.prepare_run('stderr-error')))
        self.assertEqual('extension guard/load failure', self.receipt()['reason'])

    def test_added_copied_resource_prevents_spawn(self):
        args = self.prepare_run()
        (self.root / 'out/private/case-a/home/.agents/skills/one/new.txt').write_text('extra')
        with mock.patch.object(pi_runner.subprocess, 'Popen', side_effect=AssertionError('spawn')):
            self.assertEqual(2, pi_runner.run(args))
        self.assertEqual('failed-ungraded', self.receipt()['status'])
def stat_mode(path): return os.stat(path).st_mode & 0o777
class EarlyFailureTests(unittest.TestCase):
    def test_fatal_lines_stop_a_child_that_would_keep_running(self):
        cases = [
            ('stdout', b'{bad', 'malformed NDJSON'),
            ('stdout', b'{"type":"error"}', 'error event'),
            ('stdout', b'{"type":"agent_end","willRetry":true}', 'agent retry'),
            ('stdout', b'{"type":"message_end","message":{"role":"assistant","provider":"other","model":"m"}}', 'provider/model mismatch'),
            ('stderr', b'EVALUATOR_FATAL fixture', 'extension guard/load failure'),
        ]
        for stream, payload, reason in cases:
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                fd = 1 if stream == 'stdout' else 2
                code = f'import os,time; os.write({fd},{payload + bytes([10])!r}); time.sleep(60)'
                result = pi_runner._process(
                    [sys.executable, '-c', code], root, dict(os.environ),
                    root / 'out', root / 'err', time.monotonic() + 10,
                    observer=lambda name, line: pi_runner._early_failure(name, line, 'p', 'm'))
                self.assertEqual(reason, result['reason'])
                self.assertTrue(result['reaped'])
                self.assertLess(result['elapsedSeconds'], 2)

    def test_split_utf8_and_final_line_without_newline(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            code = "import os,time; os.write(1,b'{\"type\":\"note\",\"text\":\"\\xc3'); time.sleep(.01); os.write(1,b'\\xb1\"}')"
            result = pi_runner._process(
                [sys.executable, '-c', code], root, dict(os.environ),
                root / 'out', root / 'err', time.monotonic() + 2,
                observer=lambda name, line: pi_runner._early_failure(name, line, 'p', 'm'))
            self.assertIsNone(result['reason'])

    def test_self_test_is_offline(self):
        with mock.patch.object(pi_runner.subprocess, 'Popen', side_effect=AssertionError('must not spawn')), mock.patch('builtins.print'):
            self.assertEqual(0, pi_runner.main(['self-test']))


class AdditionalPlanTests(unittest.TestCase):
    setUp = PlannerFixture.setUp
    tearDown = PlannerFixture.tearDown
    args = PlannerFixture.args

    def test_canonical_output_cannot_bypass_repository_exclusion(self):
        output = self.repo / '..' / 'repo' / 'unsafe-output'
        self.assertRaises(ValueError, pi_runner.plan, self.args(output))
        self.assertFalse((self.repo / 'unsafe-output').exists())

    def test_account_must_be_directory_before_output_creation(self):
        self.auth.rmdir()
        self.auth.write_text('not an account directory')
        output = self.root / 'out'
        self.assertRaises(ValueError, pi_runner.plan, self.args(output))
        self.assertFalse(output.exists())

    def test_handcrafted_invalid_plan_cannot_spawn(self):
        output = self.root / 'out'
        original = pi_runner.plan(self.args(output, ['case-a']))
        for mutation in ('schema', 'duplicate', 'argv', 'prompt', 'model'):
            with self.subTest(mutation=mutation):
                data = json.loads(json.dumps(original))
                if mutation == 'schema':
                    data['schema'] = True
                elif mutation == 'duplicate':
                    data['cases'].append(data['cases'][0])
                elif mutation == 'argv':
                    data['cases'][0]['argv'][0] = '/bin/false'
                elif mutation == 'prompt':
                    data['cases'][0]['prompt'] = '@private'
                    data['cases'][0]['argv'][-1] = '@private'
                else:
                    data['model'] = 'gpt-5.5'
                    data['cases'][0]['argv'][-4] = 'gpt-5.5'
                raw = json.dumps(data).encode()
                (output / 'plan.json').write_bytes(raw)
                args = argparse.Namespace(allow_live=True, plan=str(output / 'plan.json'),
                                          plan_sha256=hashlib.sha256(raw).hexdigest(), case='case-a')
                with mock.patch.object(pi_runner.subprocess, 'Popen', side_effect=AssertionError('must not spawn')):
                    self.assertRaises(ValueError, pi_runner.run, args)
                self.assertFalse((output / 'runs/case-a').exists())

if __name__=="__main__": unittest.main()

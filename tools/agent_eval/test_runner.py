"""Offline regression tests for the opt-in evaluator transport."""
import json
import io
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stdout, redirect_stderr

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import runner


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.temp.name)
        self.agents = self.root / "AGENTS.md"
        self.agents.write_text("rules\n", encoding="utf-8")
        self.fake = self.root / "fake_cli.py"
        self.fake.write_text(
            "import json, os, pathlib, sys, time\n"
            "mode=os.environ.get('FAKE_MODE','ok'); log=os.environ.get('FAKE_ARGV_LOG')\n"
            "if log: pathlib.Path(log).open('a', encoding='utf-8').write(json.dumps(sys.argv)+'\\n')\n"
            "if '--version' in sys.argv:\n"
            "  log=os.environ.get('FAKE_LOG'); pathlib.Path(log).write_text('version') if log else None; print('fake 1.0'); raise SystemExit(0)\n"
            "raw=sys.stdin.buffer.read(); base=pathlib.Path(sys.argv[sys.argv.index('-C')+1]); secret=(base/'preflight.txt').read_text(encoding='utf-8') if (base/'preflight.txt').exists() else ''\n"
            "if mode == 'timeout': print('started', flush=True); time.sleep(30)\n"
            "if mode == 'exit': print('broken'); raise SystemExit(9)\n"
            "if mode == 'suite-exit' and not secret: print('broken'); raise SystemExit(9)\n"
            "if mode == 'malformed': sys.stdout.buffer.write(b'{bad\\n'); raise SystemExit(0)\n"
            "event={'type':'item.completed','item':{'type':'command_execution','command':'Get-Content preflight.txt' if secret else 'Write-Output done','aggregated_output':secret,'exit_code':0,'status':'completed'}}\n"
            "if mode == 'no-completion': print(json.dumps(event)); raise SystemExit(0)\n"
            "if mode == 'preflight-fail': event['item']['aggregated_output']='wrong'; print(json.dumps(event)); print(json.dumps({'type':'turn.completed'})); raise SystemExit(0)\n"
            "if mode == 'failed-then-completed': print(json.dumps(event)); print(json.dumps({'type':'turn.failed','error':'bad'})); print(json.dumps({'type':'turn.completed'})); raise SystemExit(0)\n"
            "if mode == 'out-of-order': print(json.dumps({'type':'turn.completed'})); print(json.dumps(event)); raise SystemExit(0)\n"
            "print(json.dumps(event)); print(json.dumps({'type':'turn.completed'}))\n",
            encoding="utf-8",
        )

    def tearDown(self):
        self.temp.cleanup()

    def config(self, output, live=True):
        return [
            "run", "--cli", str(pathlib.Path(sys.executable).resolve()), "--cli-version", "fake 1.0", "--model", "fake-model",
            "--agents", str(self.agents), "--output", str(output), "--timeout-seconds", "1",
            "--cli-prefix", str(self.fake), *( ["--allow-live"] if live else [] ),
        ]

    def invoke(self, output, mode="ok", live=True):
        old = dict(runner.os.environ)
        runner.os.environ["FAKE_MODE"] = mode
        try:
            return runner.main(self.config(output, live))
        finally:
            runner.os.environ.clear(); runner.os.environ.update(old)

    def test_argv_is_single_source_of_truth(self):
        args = runner.parse_args(self.config(self.root / "out"))
        argv = runner.build_argv(args, self.root / "fixture")
        self.assertIn("read-only", argv)
        self.assertIn('approval_policy="never"', argv)
        self.assertEqual(argv, runner.build_argv(args, self.root / "fixture"))

    def test_preflight_failure_stops_suite(self):
        output = self.root / "out"
        self.assertNotEqual(0, self.invoke(output, "preflight-fail"))
        manifest = json.loads((output / "execution-manifest.json").read_text(encoding="utf-8"))
        self.assertEqual("failed", manifest["preflight"]["status"])
        self.assertEqual([], manifest["runs"])

    def test_allow_live_gate_prevents_even_version_execution(self):
        output, log = self.root / "out", self.root / "version.log"
        old = dict(runner.os.environ); runner.os.environ["FAKE_LOG"] = str(log)
        try:
            self.assertEqual(2, runner.main(self.config(output, live=False)))
        finally:
            runner.os.environ.clear(); runner.os.environ.update(old)
        self.assertFalse(log.exists())
        self.assertFalse(output.exists())

    def test_child_failure_timeout_malformed_and_missing_completion_fail_outer(self):
        for mode in ("exit", "timeout", "malformed", "no-completion", "failed-then-completed", "out-of-order", "suite-exit"):
            with self.subTest(mode=mode):
                output = self.root / mode
                self.assertNotEqual(0, self.invoke(output, mode))
                manifest = json.loads((output / "execution-manifest.json").read_text(encoding="utf-8"))
                self.assertEqual("failed", manifest["status"])

    def test_binary_and_utf8_streams_are_retained_exactly(self):
        stdout, stderr = self.root / "stdout.raw", self.root / "stderr.raw"
        payload = "¡índigo!".encode()
        result = runner.run_process([sys.executable, "-c", "import sys;data=sys.stdin.buffer.read();sys.stdout.buffer.write(data+b'\\xffout');sys.stderr.buffer.write(b'err\\xfe')"], payload, stdout, stderr, 2)
        self.assertEqual(0, result["exitCode"])
        self.assertEqual(payload + b"\xffout", stdout.read_bytes())
        self.assertEqual(b"err\xfe", stderr.read_bytes())

    def test_output_and_fixture_path_safety(self):
        output = self.root / "out"; output.mkdir()
        with self.assertRaises(runner.RunnerError):
            runner.prepare_output(output)
        fixture = self.root / "fixture"
        with self.assertRaises(runner.RunnerError):
            runner.write_fixture(fixture, {"../escape": "bad"}, b"rules")
        self.assertFalse((self.root / "escape").exists())

    def test_fixture_integrity_and_development_cases(self):
        cases = runner.select_cases(runner.load_case_spec()[1], [], [])
        self.assertEqual(["C1-direct-spanish-greeting", "C2-exact-file-read", "C3-untrusted-file-summary", "C4-missing-verification-evidence"], [c["id"] for c in cases])
        fixture = self.root / "fixture"
        runner.write_fixture(fixture, {"dato.txt": "color=indigo\n"}, b"rules")
        before = runner.file_manifest(fixture)
        self.assertEqual(before, runner.file_manifest(fixture))

    def test_successful_suite_is_ungraded_and_snapshotted(self):
        output = self.root / "out"
        log = self.root / "argv.jsonl"
        old = dict(runner.os.environ); runner.os.environ["FAKE_ARGV_LOG"] = str(log)
        try:
            self.assertEqual(0, self.invoke(output))
        finally:
            runner.os.environ.clear(); runner.os.environ.update(old)
        manifest = json.loads((output / "execution-manifest.json").read_text(encoding="utf-8"))
        self.assertEqual("completed-ungraded", manifest["status"])
        self.assertEqual(4, len(manifest["runs"]))
        self.assertIn("runnerSha256", manifest["target"])
        self.assertIn("caseSpecSha256", manifest["target"])
        self.assertTrue((output / "case-spec.json").is_file())
        self.assertTrue((output / "AGENTS.md").is_file())
        received = [json.loads(line) for line in log.read_text(encoding="utf-8").splitlines()]
        received = [argv for argv in received if "--version" not in argv]
        expected = [manifest["preflight"]["argv"], *(run["argv"] for run in manifest["runs"])]
        self.assertEqual([argv[1:] for argv in expected], received)

    def test_catalogue_selects_decisions_and_repeats_with_fresh_attempt_ids(self):
        spec = runner.load_case_spec()[1]
        selected = runner.select_cases(spec, ["C5-conflicting-data"], [])
        self.assertEqual(["C5-conflicting-data"], [case["id"] for case in selected])
        attempts = runner.expand_attempts(selected, 2)
        self.assertEqual(["C5-conflicting-data--attempt-1", "C5-conflicting-data--attempt-2"], [attempt["attemptId"] for attempt in attempts])
        self.assertNotEqual(attempts[0]["fixtureRelative"], attempts[1]["fixtureRelative"])
        with self.assertRaises(runner.RunnerError):
            runner.select_cases(spec, ["C5-conflicting-data"], ["missing-suite"])
        for repeat in (0, 11):
            with self.assertRaises(runner.RunnerError):
                runner.expand_attempts(selected, repeat)

    def test_planned_cases_refuse_before_version_probe(self):
        for case_id in ("I1-opencode-delegation", "I2-memory-persistence"):
            with self.subTest(case_id=case_id):
                output, log = self.root / case_id, self.root / (case_id + ".jsonl")
                old = dict(runner.os.environ); runner.os.environ["FAKE_ARGV_LOG"] = str(log)
                try:
                    result = runner.main(self.config(output) + ["--case", case_id])
                finally:
                    runner.os.environ.clear(); runner.os.environ.update(old)
                self.assertEqual(2, result)
                self.assertFalse(log.exists())
                self.assertFalse(output.exists())

    def test_list_and_plan_are_offline_and_show_readiness(self):
        output = io.StringIO()
        with redirect_stdout(output), redirect_stderr(io.StringIO()):
            self.assertEqual(0, runner.main(["list", "--case", "C5-conflicting-data"]))
        listed = json.loads(output.getvalue())
        self.assertEqual(["C5-conflicting-data"], [case["id"] for case in listed["cases"]])
        self.assertIn("prerequisites", listed["cases"][0])
        plan = io.StringIO()
        with redirect_stdout(plan), redirect_stderr(io.StringIO()):
            self.assertEqual(0, runner.main(["plan", "--case", "C5-conflicting-data", "--repeat", "2"]))
        self.assertEqual(2, len(json.loads(plan.getvalue())["attempts"]))
        with self.assertRaises(SystemExit):
            runner.main(["list", "--repeat", "2"])

    def test_repeated_execution_retains_each_attempt(self):
        output = self.root / "repeated"
        self.assertEqual(0, runner.main(self.config(output) + ["--case", "C5-conflicting-data", "--repeat", "2"]))
        manifest = json.loads((output / "execution-manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(2, len(manifest["runs"]))
        self.assertEqual(["C5-conflicting-data--attempt-1", "C5-conflicting-data--attempt-2"], [run["attemptId"] for run in manifest["runs"]])
        for run in manifest["runs"]:
            self.assertTrue((output / run["fixtureRelative"]).is_dir())
            self.assertTrue((output / run["traceRelative"] / "stdout.raw").is_file())


if __name__ == "__main__":
    unittest.main()

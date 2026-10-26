"""Run the reusable publisher's actual inline scripts with closed external fixtures.

No registry, image, scanner database, token endpoint or Git remote is contacted.
The fake release/checksum commands model success/failure; the production checksum
literal is separately pinned by contract. Raw report/environment canaries must
never appear in the publisher's stdout/stderr.
"""
from __future__ import annotations

import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/build-publish.yml"
DOC = yaml.safe_load(WORKFLOW.read_text())
STEPS = {s["name"]: s for s in DOC["jobs"]["pin"]["steps"] if "name" in s}
SOURCE = "1" * 40
DIGEST = "sha256:" + "2" * 64
OTHER = "sha256:" + "3" * 64
CHECKSUM = "c7cbe3137fb8a48a17574bede3dd6beb16742d1e9772929940fc90082c39d9c0"  # pragma: allowlist secret - official public Trivy v0.76.1 archive checksum
CANARY = "PRIVATE_ENV_MUST_NOT_BE_LOGGED"


class Publisher:
    def __init__(self, tmp: Path, *, strict=True, mode="clean", services=("api",)):
        self.tmp, self.strict, self.mode, self.services = tmp, strict, mode, services
        self.workspace = tmp / "caller"
        self.workspace.mkdir()
        (self.workspace / "infra").mkdir()
        (self.workspace / "infra/kustomization.yaml").write_text("old manifest\n")
        self.bin = tmp / "bin"
        self.bin.mkdir()
        self.runner = tmp / "runner"
        self.runner.mkdir()
        self.config = tmp / "fixture.json"
        self.config.write_text(json.dumps({"mode": mode}))
        self.events = tmp / "events.jsonl"
        self.events.touch()
        self.envfile = tmp / "github-env"
        self.envfile.touch()
        self.output = tmp / "github-output"
        self.output.touch()
        common = f'''import json,os,pathlib,sys,datetime,shutil
cfg=json.loads(pathlib.Path({str(self.config)!r}).read_text())
mode=cfg['mode']
events=pathlib.Path({str(self.events)!r})
def event(kind,**fields):
 with events.open('a') as f:f.write(json.dumps(dict(kind=kind,**fields))+'\\n')
args=sys.argv[1:]
'''
        scanner = common + f'''
if args == ['--version']:
 print('Version: '+('0.0.0' if mode=='version' else '0.76.1'));sys.exit(0)
assert args[0]=='image'
assert 'HOME' not in os.environ
assert not any(k.startswith(('HTTP_', 'HTTPS_', 'ALL_PROXY','PYTHON','TRIVY_')) for k in os.environ if k not in ['TRIVY_USERNAME','TRIVY_PASSWORD'])
assert os.environ['TRIVY_PASSWORD']=='fixture-registry-credential-only'  # pragma: allowlist secret - fixed synthetic registry fixture, never a credential
assert not any('fixture-registry' in a for a in args)
assert pathlib.Path(os.environ['DOCKER_CONFIG']).name=='docker'
assert pathlib.Path(args[args.index('--config')+1]).read_text()=='{{}}\\n'
assert pathlib.Path(args[args.index('--ignorefile')+1]).read_text()==''
for flag,value in [('--pkg-types','os,library'),('--scanners','vuln'),('--severity','HIGH,CRITICAL'),('--image-src','remote'),('--exit-code','1'),('--format','json')]:assert args[args.index(flag)+1]==value
for flag in ['--ignore-unfixed=false','--show-suppressed','--list-all-pkgs=true','--skip-db-update=false','--skip-java-db-update=false','--offline-scan=false','--include-dev-deps=true']:assert flag in args
assert args[args.index('--db-repository')+1]=='ghcr.io/aquasecurity/trivy-db:2'
assert args[args.index('--java-db-repository')+1]=='ghcr.io/aquasecurity/trivy-java-db:1'
ref=args[-1];event('scan',ref=ref,env_keys=sorted(os.environ))
print({CANARY!r},file=sys.stderr);print('fixture-registry-credential-only',file=sys.stderr)
if mode in ['auth','scanner-exit']:sys.exit(2)
if mode=='oversize-log':print('x'*(33*1024*1024),file=sys.stderr);sys.exit(2)
if mode=='oversize-report':print('x'*(33*1024*1024));sys.exit(2)
if mode=='capture-timeout':
 import time;time.sleep(600)
now=datetime.datetime.now(datetime.timezone.utc)
stamp=lambda t:t.isoformat()
report={{'SchemaVersion':2,'Trivy':{{'Version':'0.76.1'}},'ArtifactName':ref,'ArtifactType':'container_image','CreatedAt':stamp(now),'Metadata':{{'OS':{{'Family':'alpine','Name':'3.24','EOSL':False}},'ImageConfig':{{'Env':[{CANARY!r}],'WorkingDir':'/private/path'}}}},'Results':[{{'Target':'image(alpine3.24)','Class':'os-pkgs','Type':'alpine','Packages':[{{'Name':'libc','Version':'1.0'}}]}},{{'Target':'private/package-lock.json','Class':'lang-pkgs','Type':'npm','Packages':[{{'Name':'zod','Version':'4.6.6'}}]}}]}}
if mode in ['high','unfixed','language-high','second-high','oversized-cve','oversized-ghsa'] and (mode!='second-high' or '/web@' in ref):
 index=1 if mode=='language-high' else 0
 report['Results'][index]['Vulnerabilities']=[{{'VulnerabilityID':'CVE-2026-0000','PkgName':'fixture','Severity':'HIGH','FixedVersion':'' if mode=='unfixed' else '2.0'}}]
if mode in ['oversized-cve','oversized-ghsa']:report['Results'][0]['Vulnerabilities'][0]['VulnerabilityID']=('CVE-2026-' if mode=='oversized-cve' else 'GHSA-')+'9'*(2*1024*1024)
if mode=='wrong-ref':report['ArtifactName']='wrong'
if mode=='wrong-type':report['ArtifactType']='filesystem'
if mode=='no-os':report['Metadata'].pop('OS')
if mode=='unknown-os':report['Metadata']['OS']['Family']='unrecognized'
if mode=='wrong-version':report['Trivy']['Version']='0.0.0'
if mode=='report-error':report['Errors']=['fixture error']
if mode=='no-packages':report['Results'][0].pop('Packages')
if mode=='empty-results':report['Results']=[]
if mode=='suppressed':report['Results'][0]['SuppressedFindings']=[{{'Status':'ignored'}}]
if mode=='old-report':report['CreatedAt']=stamp(now-datetime.timedelta(hours=1))
assert '--output' not in args
if mode=='missing-report':sys.exit(0)
cache=pathlib.Path(args[args.index('--cache-dir')+1]);(cache/'db').mkdir(exist_ok=True)
if mode!='missing-db':(cache/'db/metadata.json').write_text(json.dumps({{'Version':2,'UpdatedAt':stamp(now-datetime.timedelta(hours=72 if mode=='old-db' else 1)),'DownloadedAt':stamp(now),'NextUpdate':stamp(now+datetime.timedelta(hours=2))}}))
if mode=='large-cache':
 with (cache/'db/trivy.db').open('wb') as f:f.truncate(40*1024*1024)
 event('cache-file',size=(cache/'db/trivy.db').stat().st_size)
print('not json' if mode=='malformed' else json.dumps(report))
sys.exit(0)
'''
        self.archive = tmp / "fake-release.tar.gz"
        with tarfile.open(self.archive, "w:gz") as archive:
            b = ("#!/usr/bin/env python3\n" + scanner).encode()
            member = tarfile.TarInfo("trivy")
            member.size, member.mode = len(b), 0o700
            archive.addfile(member, io.BytesIO(b))
        self.tool("curl", common + f'''
assert 'HOME' not in os.environ
assert not any(k.startswith(('TRIVY_','HTTP_','HTTPS_','ALL_PROXY','DOCKER_')) for k in os.environ)
assert 'https://github.com/aquasecurity/trivy/releases/download/v0.76.1/trivy_0.76.1_Linux-64bit.tar.gz' in args
assert '-q' in args
event('download')
if mode=='download':sys.exit(1)
shutil.copyfile({str(self.archive)!r},args[args.index('--output')+1])
''')
        self.tool("sha256sum", common + f'''
assert args==['--check','--status']
assert sys.stdin.read().split()[0]=={CHECKSUM!r}
event('checksum')
sys.exit(1 if mode=='checksum' else 0)
''')
        self.tool("uname", common + "print('Linux' if args==['-s'] else 'x86_64')\n")
        self.tool("sleep", common + "pass\n")
        self.tool("docker", common + f'''
assert args[:3]==['buildx','imagetools','inspect'];event('derive',ref=args[3])
if mode=='missing-image':sys.exit(1)
print(json.dumps({DIGEST!r}))
''')
        self.tool("cosign", common + f'''
event('verify',ref=args[-1]);print({CANARY!r},file=sys.stderr)
assert '--certificate-oidc-issuer=https://token.actions.githubusercontent.com' in args
sys.exit(1 if mode=='signature' else 0)
''')
        self.tool("kustomize", common + '''
assert args[:3]==['edit','set','image'];event('write',image=args[3])
pathlib.Path('kustomization.yaml').write_text(args[3]+'\\n')
''')
        self.tool("git", common + f'''
event('git',args=args)
if args[:2]==['diff','--quiet']:sys.exit(1)
if args[0]=='rev-parse':print({SOURCE!r})
if args==['push']:
 count=sum(1 for line in events.read_text().splitlines() if json.loads(line)=={{'kind':'git','args':['push']}})
 if count==1 and mode in ['retry','retry-drift']:
  if mode=='retry-drift':pathlib.Path({str(self.workspace / 'digests/api.txt')!r}).write_text('api='+{OTHER!r}+'\\n')
  sys.exit(1)
''')
        # The only clock override is an external Python fixture, never a workflow option.
        capture_clock = (
            "import runpy,sys,time,subprocess,pathlib; old=time.monotonic; start=old(); "
            "time.monotonic=lambda:old()+(2000 if old()-start>0.5 else 0); RealPopen=subprocess.Popen\n"
            "class Popen(RealPopen):\n"
            " def __init__(self,*a,**kw):\n"
            f"  super().__init__(*a,**kw);pathlib.Path({str(self.tmp / 'scanner.pid')!r}).write_text(str(self.pid))\n"
            "subprocess.Popen=Popen;sys.argv=sys.argv[1:];runpy.run_path(sys.argv[0],run_name='__main__')"
        )
        self.tool("python3", common + f'''
if mode=='capture-timeout' and len(args)>1 and args[0].endswith('/scan.py') and args[1]=='scan':
 code={capture_clock!r}
 os.execv({sys.executable!r},[{sys.executable!r},'-c',code,*args])
if mode=='stale' and len(args)>1 and args[0].endswith('/scan.py') and args[1]=='check':
 code="import runpy,sys,time; old=time.time; time.time=lambda:old()+3601;sys.argv=sys.argv[1:];runpy.run_path(sys.argv[0],run_name='__main__')"
 os.execv({sys.executable!r},[{sys.executable!r},'-c',code,*args])
os.execv({sys.executable!r},[{sys.executable!r},*args])
''')
        self.env = {**os.environ, "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
                    "GITHUB_WORKSPACE": str(self.workspace), "RUNNER_TEMP": str(self.runner),
                    "GITHUB_ENV": str(self.envfile), "GITHUB_OUTPUT": str(self.output),
                    "GITHUB_STEP_SUMMARY": str(tmp / "summary"), "GITHUB_SHA": SOURCE,
                    "GITHUB_RUN_ID": "123", "GITHUB_RUN_ATTEMPT": "1", "GITHUB_REF_NAME": "main",
                    "GITHUB_REPOSITORY": "madfam-org/fixture", "IMAGE_PREFIX": "ghcr.io/madfam-org/fixture",
                    "REQUIRE_IMAGE_SCAN": str(strict).lower(), "KUSTOMIZATION_PATH": "infra",
                    "SERVICES_MATRIX": json.dumps([{"name": n} for n in services]),
                    "CERT_IDENTITY_REGEXP": "fixture-approved-identity", "CI_USER_NAME": "fixture",
                    "CI_USER_EMAIL": "fixture@example.invalid", "GHCR_SCAN_USERNAME": "fixture-user",
                    "GHCR_SCAN_TOKEN": "fixture-registry-credential-only",
                    "TRIVY_IGNORE_UNFIXED": "true", "TRIVY_SCANNERS": "secret",
                    "TRIVY_CONFIG": "/untrusted/config", "TRIVY_SKIP_DB_UPDATE": "true",
                    "HTTPS_PROXY": "http://untrusted.invalid", "DOCKER_HOST": "tcp://untrusted.invalid"}
        self.results = []

    def tool(self, name, code):
        path = self.bin / name
        # Fixed absolute Python avoids recursion through the python3 fixture.
        path.write_text(f"#!{sys.executable}\n" + code)
        path.chmod(0o700)

    def step(self, name):
        script = STEPS[name]["run"].replace("${{ github.sha }}", SOURCE)
        result = subprocess.run(["bash", "-c", script], cwd=self.workspace, env=self.env,
                                capture_output=True, text=True, timeout=30)
        self.results.append(result)
        for line in self.envfile.read_text().splitlines():
            key, value = line.split("=", 1)
            self.env[key] = value
        assert CANARY not in result.stdout + result.stderr
        assert "fixture-registry-credential-only" not in result.stdout + result.stderr
        return result

    def run(self):
        for name in ["Derive digests from GHCR", "Prepare fixed pre-pin image scanner",
                     "Update kustomization image digests", "Commit & push digest update"]:
            if name.startswith("Prepare") and not self.strict:
                continue
            result = self.step(name)
            if result.returncode:
                return result
        return result

    def calls(self, kind):
        return [x for x in map(json.loads, self.events.read_text().splitlines()) if x["kind"] == kind]


def test_typed_optin_and_existing_signer_and_pin_job_contract():
    calls = DOC.get("on", DOC.get(True))["workflow_call"]
    assert calls["inputs"]["require_image_scan"] == {
        "description": "Require the fixed full OS/language HIGH+CRITICAL pre-pin policy, including unfixed findings.",
        "required": False, "type": "boolean", "default": False}
    assert "always()" in DOC["jobs"]["pin"]["if"]
    assert "needs.build.result != 'cancelled'" in DOC["jobs"]["pin"]["if"]
    assert "needs.build.result != 'skipped'" in DOC["jobs"]["pin"]["if"]
    assert CHECKSUM in STEPS["Prepare fixed pre-pin image scanner"]["run"]
    assert not any(s.get("continue-on-error") for s in DOC["jobs"]["pin"]["steps"])
    assert STEPS["Remove private pre-pin scan evidence"]["if"] == "always() && inputs.require_image_scan"


@pytest.mark.parametrize("build_result", ["success", "failure"])
def test_late_registry_digest_is_verified_scanned_before_any_write(tmp_path, build_result):
    p = Publisher(tmp_path, services=("api", "web"))
    p.env["BUILD_RESULT"] = build_result
    assert p.run().returncode == 0
    events = list(map(json.loads, p.events.read_text().splitlines()))
    assert len(p.calls("scan")) == 2
    assert max(i for i, e in enumerate(events) if e["kind"] == "scan") < min(i for i, e in enumerate(events) if e["kind"] == "write")
    for service in p.services:
        ref = f"ghcr.io/madfam-org/fixture/{service}@{DIGEST}"
        assert {x["ref"] for x in p.calls("verify")} == {x["ref"] for x in p.calls("scan")}
        assert ref in {x["ref"] for x in p.calls("scan")}
    assert all("HOME" not in x["env_keys"] for x in p.calls("scan"))
    for report in Path(p.env["PREPIN_SCAN_ROOT"]).glob("*.report.json"):
        assert report.stat().st_mode & 0o777 == 0o600
    assert p.step("Remove private pre-pin scan evidence").returncode == 0
    assert not Path(p.env["PREPIN_SCAN_ROOT"]).exists()


@pytest.mark.parametrize("mode", ["download", "checksum", "version", "signature", "high", "unfixed",
    "language-high", "auth", "scanner-exit", "oversize-log", "oversize-report", "missing-report", "malformed", "wrong-ref", "wrong-type",
    "no-os", "unknown-os", "wrong-version", "report-error", "no-packages", "empty-results", "suppressed", "old-report", "missing-db", "old-db", "stale"])
def test_scan_install_auth_database_and_report_failures_cannot_write_or_pin(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode)
    assert p.run().returncode != 0
    assert not p.calls("write")
    assert not p.calls("git")
    if mode.startswith("oversize-"):
        private = Path(p.env["PREPIN_SCAN_ROOT"])
        assert all(f.stat().st_size <= 32 * 1024 * 1024 for f in private.glob("*.report.json"))
        assert all(f.stat().st_size <= 32 * 1024 * 1024 for f in private.glob("*.log"))


def test_second_service_failure_prevents_first_manifest_write(tmp_path):
    p = Publisher(tmp_path, mode="second-high", services=("api", "web"))
    assert p.run().returncode != 0
    assert len(p.calls("scan")) == 2
    assert not p.calls("write")
    assert not p.calls("git")


@pytest.mark.parametrize("mode", ["missing-image", "stale-digests"])
def test_missing_image_or_stale_digest_directory_refused(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode)
    if mode == "stale-digests":
        (p.workspace / "digests").mkdir()
        (p.workspace / "digests/old.txt").write_text("stale")
    assert p.run().returncode != 0
    assert not p.calls("write")
    assert not p.calls("git")


def test_skipped_scan_cannot_enter_commit_step(tmp_path):
    p = Publisher(tmp_path)
    assert p.step("Derive digests from GHCR").returncode == 0
    assert p.step("Prepare fixed pre-pin image scanner").returncode == 0
    assert p.step("Commit & push digest update").returncode != 0
    assert not p.calls("write")
    assert not p.calls("git")


def test_retry_keeps_scanned_digest_without_rederiving_tag(tmp_path):
    p = Publisher(tmp_path, mode="retry")
    assert p.run().returncode == 0
    assert len(p.calls("derive")) == 1
    assert len(p.calls("scan")) == 1
    assert len([x for x in p.calls("git") if x["args"] == ["push"]]) == 2
    assert all(x["image"].endswith(DIGEST) for x in p.calls("write"))


def test_retry_digest_file_drift_refuses_reapplication_and_second_push(tmp_path):
    p = Publisher(tmp_path, mode="retry-drift")
    assert p.run().returncode != 0
    assert len(p.calls("write")) == 1
    assert len([x for x in p.calls("git") if x["args"] == ["push"]]) == 1
    assert OTHER not in "".join(x["image"] for x in p.calls("write"))


def test_legacy_false_preserves_no_scan_behavior(tmp_path):
    p = Publisher(tmp_path, strict=False)
    assert p.run().returncode == 0
    assert p.calls("write")
    assert not p.calls("scan")
    assert not p.calls("download")


def test_actual_inline_scripts_parse_and_driver_compiles():
    for name in ["Derive digests from GHCR", "Prepare fixed pre-pin image scanner",
                 "Update kustomization image digests", "Commit & push digest update",
                 "Remove private pre-pin scan evidence"]:
        assert subprocess.run(["bash", "-n"], input=STEPS[name]["run"], text=True, capture_output=True).returncode == 0
    run = STEPS["Prepare fixed pre-pin image scanner"]["run"]
    driver = run.split("<<'PY_SCAN'\n", 1)[1].split("\nPY_SCAN", 1)[0]
    compile(driver, "inline-scan.py", "exec")
    assert "HOME=" not in WORKFLOW.read_text()


def test_digest_drift_after_scan_refuses_initial_commit(tmp_path):
    p = Publisher(tmp_path)
    for name in ["Derive digests from GHCR", "Prepare fixed pre-pin image scanner", "Update kustomization image digests"]:
        assert p.step(name).returncode == 0
    (p.workspace / "digests/api.txt").write_text("api=" + OTHER + "\n")
    assert p.step("Commit & push digest update").returncode != 0
    assert not p.calls("git")


def test_changed_approval_ledger_refuses_initial_commit(tmp_path):
    p = Publisher(tmp_path)
    for name in ["Derive digests from GHCR", "Prepare fixed pre-pin image scanner", "Update kustomization image digests"]:
        assert p.step(name).returncode == 0
    ledger = Path(p.env["PREPIN_SCAN_ROOT"]) / "approved.json"
    ledger.write_text(ledger.read_text() + " ")
    assert p.step("Commit & push digest update").returncode != 0
    assert not p.calls("git")


@pytest.mark.parametrize("mode", ["oversized-cve", "oversized-ghsa"])
def test_oversized_vulnerability_coordinate_is_redacted_and_cannot_pin(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode)
    assert p.run().returncode != 0
    public_output = "".join(r.stdout + r.stderr for r in p.results)
    assert len(public_output) < 4096
    assert '"vulnerability": "redacted"' in public_output
    assert "9" * 256 not in public_output
    assert not p.calls("write")
    assert not p.calls("git")


def test_realistic_cache_larger_than_output_limit_succeeds(tmp_path):
    p = Publisher(tmp_path, mode="large-cache")
    assert p.run().returncode == 0
    assert p.calls("cache-file") == [{"kind": "cache-file", "size": 40 * 1024 * 1024}]
    assert p.calls("write")
    assert p.step("Remove private pre-pin scan evidence").returncode == 0


def test_output_capture_timeout_kills_and_reaps_owned_scanner(tmp_path):
    p = Publisher(tmp_path, mode="capture-timeout")
    assert p.run().returncode != 0
    pid = int((tmp_path / "scanner.pid").read_text())
    with pytest.raises(ProcessLookupError):
        os.kill(pid, 0)
    assert not p.calls("write")
    assert not p.calls("git")

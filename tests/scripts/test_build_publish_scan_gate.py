"""Run the reusable publisher's actual inline scripts with closed external fixtures.

No registry, image, scanner database, token endpoint or Git remote is contacted.
The fake release/checksum commands model success/failure; the production checksum
literal is separately pinned by contract. Raw report/environment canaries must
never appear in the publisher's stdout/stderr.
"""
from __future__ import annotations

import io
import datetime
import hashlib
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
SUMMARY_PACKAGES = {
    "summary-package-absolute": "/fixture-private/example",
    "summary-package-backslash": "fixture-private\\example",
    "summary-package-relative": "../fixture-package",
    "summary-package-dot": "./fixture-package",
    "summary-package-traversal": "fixture/../example",
    "summary-package-drive": "C:/fixture-private/example",
    "summary-package-file-uri": "file:///fixture-private/example",
    "summary-package-uri": "https://fixture-private.invalid/package",
    "summary-package-empty-segment": "fixture-private//package",
    "summary-package-scoped": "@scope/name",
    "summary-package-go": "github.com/fixture/package",
}


class Publisher:
    def __init__(self, tmp: Path, *, strict=True, mode="clean", services=("api",),
                 repository="madfam-org/fixture", image_prefix=None):
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
        common = f'''import json,os,pathlib,sys,datetime,shutil,hashlib
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
for flag,value in [('--pkg-types','os,library'),('--pkg-relationships','unknown,root,workspace,direct,indirect'),('--scanners','vuln'),('--severity','HIGH,CRITICAL'),('--image-src','remote'),('--exit-code','1'),('--format','json')]:assert args[args.index(flag)+1]==value
for flag in ['--ignore-unfixed=false','--show-suppressed','--list-all-pkgs=true','--skip-db-update=false','--skip-java-db-update=false','--offline-scan=false']:assert flag in args
assert not any(a.startswith('--include-dev-deps') for a in args)
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
stamp=lambda t:(t.astimezone(datetime.timezone(datetime.timedelta(hours=5,minutes=30))) if mode=='offset-time' else t).isoformat()
report={{'SchemaVersion':2,'Trivy':{{'Version':'0.76.1'}},'ArtifactName':ref,'ArtifactType':'container_image','CreatedAt':stamp(now),'Metadata':{{'OS':{{'Family':'alpine','Name':'3.24','EOSL':False}},'ImageConfig':{{'Env':[{CANARY!r}],'WorkingDir':'/private/path'}}}},'Results':[{{'Target':'image(alpine3.24)','Class':'os-pkgs','Type':'alpine','Packages':[{{'Name':'libc','Version':'1.0'}}]}},{{'Target':'private/package-lock.json','Class':'lang-pkgs','Type':'npm','Packages':[{{'Name':'zod','Version':'4.6.6'}}]}}]}}
if (mode in ['high','unfixed','language-high','second-high','oversized-cve','oversized-ghsa'] or mode in {tuple(SUMMARY_PACKAGES)!r}) and (mode!='second-high' or '/web@' in ref):
 index=1 if mode=='language-high' else 0
 report['Results'][index]['Vulnerabilities']=[{{'VulnerabilityID':'CVE-2026-0000','PkgName':'fixture','Severity':'HIGH','FixedVersion':'' if mode=='unfixed' else '2.0'}}]
if mode in ['oversized-cve','oversized-ghsa']:report['Results'][0]['Vulnerabilities'][0]['VulnerabilityID']=('CVE-2026-' if mode=='oversized-cve' else 'GHSA-')+'9'*(2*1024*1024)
if mode in {tuple(SUMMARY_PACKAGES)!r}:report['Results'][0]['Vulnerabilities'][0]['PkgName']={SUMMARY_PACKAGES!r}[mode]
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
if mode=='naive-report-time':report['CreatedAt']=now.replace(tzinfo=None).isoformat()
assert '--output' not in args
if mode=='missing-report':sys.exit(0)
cache=pathlib.Path(args[args.index('--cache-dir')+1]);(cache/'db').mkdir(exist_ok=True)
metadata={{'Version':2,'UpdatedAt':stamp(now-datetime.timedelta(hours=72 if mode=='old-db' else 1)),'DownloadedAt':stamp(now),'NextUpdate':stamp(now+datetime.timedelta(hours=2))}}
if mode=='naive-db-time':metadata['UpdatedAt']=now.replace(tzinfo=None).isoformat()
if mode=='bool-db-time':metadata['UpdatedAt']=True
if mode=='nonfinite-db-time':metadata['UpdatedAt']=float('nan')
if mode!='missing-db':
 (cache/'db/metadata.json').write_text(json.dumps(metadata))
 event('db-metadata',ref=ref,metadata=metadata,sha256=hashlib.sha256((cache/'db/metadata.json').read_bytes()).hexdigest())
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
print('FIXTURE_PIN_WRITE')
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
                    "GITHUB_REPOSITORY": repository,
                    "IMAGE_PREFIX": image_prefix if image_prefix is not None else "ghcr.io/" + repository,
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

    def driver(self, *arguments):
        result = subprocess.run(
            ["python3", str(Path(self.env["PREPIN_SCAN_ROOT"]) / "scan.py"), *arguments],
            cwd=self.workspace, env=self.env, capture_output=True, text=True, timeout=30)
        self.results.append(result)
        assert CANARY not in result.stdout + result.stderr
        assert "fixture-registry-credential-only" not in result.stdout + result.stderr
        return result

    def calls(self, kind):
        return [x for x in map(json.loads, self.events.read_text().splitlines()) if x["kind"] == kind]


def emitted_evidence(output):
    receipts, checkpoints = [], []
    for line in output.splitlines():
        if line.startswith("ENCLII_PREPIN_SCAN_RECEIPT "):
            receipts.append(json.loads(line.split(" ", 1)[1]))
        elif line.startswith("ENCLII_PREPIN_ALL_IMAGES_VERIFIED "):
            checkpoints.append(json.loads(line.split(" ", 1)[1]))
    return receipts, checkpoints


def scanned_without_pin(p):
    assert p.step("Derive digests from GHCR").returncode == 0
    assert p.step("Prepare fixed pre-pin image scanner").returncode == 0
    for name in p.services:
        assert p.driver("scan", f"{p.env['IMAGE_PREFIX']}/{name}@{DIGEST}").returncode == 0
    sealed = p.driver("seal")
    assert sealed.returncode == 0
    assert re.fullmatch(r"[0-9a-f]{64}\n", sealed.stdout)
    assert p.driver("check", sealed.stdout.strip()).returncode == 0
    return sealed.stdout.strip()


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


def test_exact_money_flat_image_mapping_preserves_package_and_binds_repository(tmp_path):
    p = Publisher(tmp_path, repository="madfam-org/money-moves",
                  image_prefix="ghcr.io/madfam-org", services=("money-moves",))
    assert p.run().returncode == 0
    ref = f"ghcr.io/madfam-org/money-moves@{DIGEST}"
    assert {x["ref"] for x in p.calls("verify")} == {ref}
    assert {x["ref"] for x in p.calls("scan")} == {ref}
    assert [x["image"] for x in p.calls("write")] == [
        f"ghcr.io/madfam-org/money-moves={ref}"]
    context = json.loads((Path(p.env["PREPIN_SCAN_ROOT"]) / "context.json").read_text())
    assert context["identity"] == {
        "source": SOURCE, "run": "123", "attempt": "1",
        "repository": "madfam-org/money-moves", "prefix": "ghcr.io/madfam-org"}
    assert context["services"] == ["money-moves"]


@pytest.mark.parametrize("repository,prefix,services", [
    ("madfam-org/another", "ghcr.io/madfam-org", ("money-moves",)),
    ("another-org/money-moves", "ghcr.io/madfam-org", ("money-moves",)),
    ("madfam-org/money-moves", "ghcr.io/madfam-org/another", ("money-moves",)),
    ("madfam-org/money-moves", "docker.io/madfam-org", ("money-moves",)),
    ("madfam-org/money-moves", "", ("money-moves",)),
    ("madfam-org/money-moves", "ghcr.io/madfam-org", ("money-moves", "api")),
    ("madfam-org/money-moves", "ghcr.io/madfam-org", ("api",)),
])
def test_flat_mapping_rejects_foreign_identity_or_unapproved_cohort(tmp_path, repository, prefix, services):
    p = Publisher(tmp_path, repository=repository, image_prefix=prefix, services=services)
    assert p.run().returncode != 0
    assert not p.calls("scan")
    assert not p.calls("write")
    assert not p.calls("git")


@pytest.mark.parametrize("tamper", ["repository", "prefix", "extra-service", "wrong-service"])
def test_money_flat_context_revalidates_repository_and_exact_cohort(tmp_path, tamper):
    p = Publisher(tmp_path, repository="madfam-org/money-moves",
                  image_prefix="ghcr.io/madfam-org", services=("money-moves",))
    assert p.step("Derive digests from GHCR").returncode == 0
    assert p.step("Prepare fixed pre-pin image scanner").returncode == 0
    path = Path(p.env["PREPIN_SCAN_ROOT"]) / "context.json"
    context = json.loads(path.read_text())
    if tamper == "repository":
        context["identity"]["repository"] = "madfam-org/another"
    elif tamper == "prefix":
        context["identity"]["prefix"] = "ghcr.io/madfam-org/another"
    else:
        # Keep the digest files coherent with the forged cohort so inventory
        # shape alone cannot substitute for the approved transport binding.
        context["services"] = ["api", "money-moves"] if tamper == "extra-service" else ["api"]
        (p.workspace / "digests/api.txt").write_text("api=" + DIGEST + "\n")
        if tamper == "wrong-service":
            (p.workspace / "digests/money-moves.txt").unlink()
    path.write_text(json.dumps(context))
    assert p.step("Update kustomization image digests").returncode != 0
    assert not p.calls("scan")
    assert not p.calls("write")
    assert not p.calls("git")


@pytest.mark.parametrize("mode", ["download", "checksum", "version", "signature", "high", "unfixed",
    "language-high", "auth", "scanner-exit", "oversize-log", "oversize-report", "missing-report", "malformed", "wrong-ref", "wrong-type",
    "no-os", "unknown-os", "wrong-version", "report-error", "no-packages", "empty-results", "suppressed", "old-report", "missing-db", "old-db", "stale"])
def test_scan_install_auth_database_and_report_failures_cannot_write_or_pin(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode)
    assert p.run().returncode != 0
    assert not p.calls("write")
    assert not p.calls("git")
    assert emitted_evidence("".join(r.stdout for r in p.results)) == ([], [])
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
    assert emitted_evidence("".join(r.stdout for r in p.results)) == ([], [])


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


@pytest.mark.parametrize("mode", list(SUMMARY_PACKAGES))
def test_package_summary_redacts_paths_and_preserves_scoped_coordinates(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode)
    assert p.run().returncode != 0
    public_output = "".join(r.stdout + r.stderr for r in p.results)
    coordinates = [json.loads(line) for line in public_output.splitlines() if line.startswith('{"package":')]
    assert len(coordinates) == 1
    if mode in ["summary-package-scoped", "summary-package-go"]:
        assert coordinates[0]["package"] == SUMMARY_PACKAGES[mode]
    else:
        assert coordinates[0]["package"] == "redacted"
        assert SUMMARY_PACKAGES[mode] not in public_output
    assert len(public_output) < 4096
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


@pytest.mark.parametrize("mode", ["clean", "offset-time"])
def test_durable_receipts_bind_closed_cohort_before_writes_and_survive_cleanup(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode, services=("api", "web"))
    assert p.run().returncode == 0
    public_output = "".join(r.stdout for r in p.results)
    receipts, checkpoints = emitted_evidence(public_output)
    assert len(receipts) == 2 and len(checkpoints) == 1
    checkpoint = checkpoints[0]
    assert set(checkpoint) == {
        "schema", "source", "repository", "run", "attempt", "policy", "trivy_version",
        "ledger_sha256", "image_count", "images", "high", "critical"}
    assert checkpoint["schema"] == "enclii-prepin-all-images-verified-v1"
    assert checkpoint["image_count"] == 2
    assert checkpoint["high"] == checkpoint["critical"] == 0
    scan_root = Path(p.env["PREPIN_SCAN_ROOT"])
    assert checkpoint["ledger_sha256"] == hashlib.sha256((scan_root / "approved.json").read_bytes()).hexdigest()
    expected_keys = {
        "schema", "source", "repository", "run", "attempt", "ref", "policy", "trivy_version",
        "include_unfixed", "all_installed_os_and_language_packages", "package_relationships",
        "high", "critical", "inventory", "report_sha256", "db", "scan_finished_at", "ledger_sha256"}
    database_events = {x["ref"]: x for x in p.calls("db-metadata")}
    reconstructed = []
    for receipt in receipts:
        assert set(receipt) == expected_keys
        assert receipt["schema"] == "enclii-prepin-image-scan-receipt-v1"
        assert receipt["source"] == checkpoint["source"] == SOURCE
        assert receipt["repository"] == checkpoint["repository"] == "madfam-org/fixture"
        assert receipt["run"] == checkpoint["run"] == "123"
        assert receipt["attempt"] == checkpoint["attempt"] == "1"
        assert receipt["policy"] == checkpoint["policy"] == "enclii-image-os-language-high-critical-unfixed-v1"
        assert receipt["trivy_version"] == checkpoint["trivy_version"] == "0.76.1"
        assert receipt["include_unfixed"] is receipt["all_installed_os_and_language_packages"] is True
        assert receipt["package_relationships"] == ["unknown", "root", "workspace", "direct", "indirect"]
        assert receipt["high"] == receipt["critical"] == 0
        assert receipt["inventory"] == {"os_packages": 1, "language_packages": 1}
        assert receipt["ledger_sha256"] == checkpoint["ledger_sha256"]
        service = receipt["ref"].split("/")[-1].split("@", 1)[0]
        assert receipt["report_sha256"] == hashlib.sha256((scan_root / (service + ".txt.report.json")).read_bytes()).hexdigest()
        db = receipt["db"]
        assert set(db) == {"version", "updated_at", "downloaded_at", "next_update", "metadata_sha256"}
        assert db["version"] == 2 and db["metadata_sha256"] == database_events[receipt["ref"]]["sha256"]
        for public, native in [("updated_at", "UpdatedAt"), ("downloaded_at", "DownloadedAt"), ("next_update", "NextUpdate")]:
            assert db[public].endswith("Z")
            assert datetime.datetime.fromisoformat(db[public].replace("Z", "+00:00")).timestamp() == datetime.datetime.fromisoformat(database_events[receipt["ref"]]["metadata"][native]).timestamp()
        assert receipt["scan_finished_at"].endswith("Z")
        projection = {k: v for k, v in receipt.items() if k != "ledger_sha256"}
        reconstructed.append({"ref": receipt["ref"], "receipt_sha256": hashlib.sha256(
            json.dumps(projection, sort_keys=True, separators=(",", ":")).encode()).hexdigest()})
    assert checkpoint["images"] == reconstructed
    evidence_json = json.dumps([receipts, checkpoints])
    for private in [CANARY, "ImageConfig", "/private/path", "private/package-lock.json", "fixture-registry-credential-only"]:
        assert private not in evidence_json
    assert "ENCLII_PREPIN_ALL_IMAGES_VERIFIED " in public_output
    assert public_output.index("ENCLII_PREPIN_ALL_IMAGES_VERIFIED ") < public_output.index("FIXTURE_PIN_WRITE")
    assert p.step("Remove private pre-pin scan evidence").returncode == 0
    assert not scan_root.exists()
    assert emitted_evidence(public_output) == (receipts, checkpoints)


@pytest.mark.parametrize("mode", ["naive-report-time", "naive-db-time", "bool-db-time", "nonfinite-db-time"])
def test_report_and_database_times_require_timezone_aware_input(tmp_path, mode):
    p = Publisher(tmp_path, mode=mode)
    assert p.run().returncode != 0
    assert emitted_evidence("".join(r.stdout for r in p.results)) == ([], [])
    assert not p.calls("write")
    assert not p.calls("git")


@pytest.mark.parametrize("tamper", [
    "unknown-field", "nonzero-high", "boolean-inventory", "naive-db", "wrong-db-sha",
    "unfixed-false", "wrong-source", "report", "ledger", "digest",
])
def test_durable_receipt_emit_rejects_tampering_without_any_acceptance_marker(tmp_path, tamper):
    p = Publisher(tmp_path)
    ledger_sha = scanned_without_pin(p)
    scan_root = Path(p.env["PREPIN_SCAN_ROOT"])
    receipt_path = scan_root / "api.txt.receipt.json"
    receipt = json.loads(receipt_path.read_text())
    if tamper == "report":
        with (scan_root / "api.txt.report.json").open("a") as out:
            out.write(" ")
    elif tamper == "ledger":
        with (scan_root / "approved.json").open("a") as out:
            out.write(" ")
    elif tamper == "digest":
        (p.workspace / "digests/api.txt").write_text("api=" + OTHER + "\n")
    else:
        if tamper == "unknown-field":
            receipt["unexpected"] = {"Env": CANARY}
        elif tamper == "nonzero-high":
            receipt["high"] = 1
        elif tamper == "boolean-inventory":
            receipt["inventory"]["language_packages"] = True
        elif tamper == "naive-db":
            receipt["db"]["updated_at"] = receipt["db"]["updated_at"].removesuffix("Z")
        elif tamper == "wrong-db-sha":
            receipt["db"]["metadata_sha256"] = CANARY
        elif tamper == "unfixed-false":
            receipt["include_unfixed"] = False
        elif tamper == "wrong-source":
            receipt["source"] = "4" * 40
        receipt_path.write_text(json.dumps(receipt, sort_keys=True, separators=(",", ":")))
    assert p.driver("emit", ledger_sha).returncode != 0
    assert emitted_evidence("".join(r.stdout for r in p.results)) == ([], [])
    assert not p.calls("write")
    assert not p.calls("git")


def test_malformed_later_projection_cannot_emit_a_partial_cohort(tmp_path):
    p = Publisher(tmp_path, services=("api", "web"))
    ledger_sha = scanned_without_pin(p)
    path = Path(p.env["PREPIN_SCAN_ROOT"]) / "web.txt.receipt.json"
    data = json.loads(path.read_text())
    data["unexpected"] = {"Env": CANARY}
    path.write_text(json.dumps(data, sort_keys=True, separators=(",", ":")))
    assert p.driver("emit", ledger_sha).returncode != 0
    assert emitted_evidence("".join(r.stdout for r in p.results)) == ([], [])
    assert not p.calls("write")
    assert not p.calls("git")

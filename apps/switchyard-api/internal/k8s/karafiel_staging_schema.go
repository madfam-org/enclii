package k8s

import (
	"strings"
	"time"
)

// KarafielSchemaProfile is a fixed source-selected operation, not a generic
// command, SQL runner, reset or owner credential export.
const KarafielSchemaProfile = "karafiel-staging-migrate"

var karafielSchemaKind = bootstrapKind{
	manager: "isolated-karafiel-schema", namePrefix: "schema-", version: "karafiel-schema-v1",
	intent: "KARAFIEL_SCHEMA_INTENT", execution: "KARAFIEL_SCHEMA_EXECUTION", retained: "KARAFIEL_SCHEMA_APPLIED", held: "KARAFIEL_SCHEMA_HELD", unretained: "KARAFIEL_SCHEMA_UNRETAINED", nameIncludesKey: true,
}

type KarafielSchemaReceipt struct {
	Schema         string `json:"schema"`
	SourceRevision string `json:"source_revision"`
	Database       string `json:"database"`
	OwnerRole      string `json:"owner_role"`
	GraphSHA256    string `json:"graph_sha256"`
	CatalogSHA256  string `json:"catalog_sha256"`
	Applied        int    `json:"applied"`
	Required       int    `json:"required"`
	Pending        int    `json:"pending"`
	Lineage        string `json:"lineage"`
}

type KarafielSchemaRuntime struct {
	PodName      string `json:"pod_name"`
	PodUID       string `json:"pod_uid"`
	JobUID       string `json:"job_uid"`
	ImageID      string `json:"image_id"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	ExitCode     int32  `json:"exit_code"`
	RestartCount int32  `json:"restart_count"`
}

func validKarafielRuntime(r *KarafielSchemaRuntime, receipt InitialNativeReceipt) bool {
	digest := strings.TrimPrefix(receipt.Image, "ghcr.io/madfam-org/karafiel/api@sha256:")
	if !initialSHA.MatchString(digest) || r == nil || r.PodName == "" || r.PodUID != receipt.PodUID || r.JobUID != receipt.JobUID || r.ExitCode != 0 || r.RestartCount != 0 || (r.ImageID != receipt.Image && r.ImageID != "docker-pullable://"+receipt.Image && r.ImageID != "containerd://sha256:"+digest) {
		return false
	}
	start, err := time.Parse(time.RFC3339Nano, r.StartedAt)
	if err != nil || start.IsZero() {
		return false
	}
	finish, err := time.Parse(time.RFC3339Nano, r.FinishedAt)
	return err == nil && !finish.IsZero() && !finish.Before(start)
}

func validKarafielSchema(r *KarafielSchemaReceipt, source string) bool {
	return r != nil && r.Schema == "karafiel-staging-schema-v1" && r.SourceRevision == source && initialRevision.MatchString(source) && r.Database == "karafiel_staging_sim" && r.OwnerRole == "enclii_migrator" && initialSHA.MatchString(r.GraphSHA256) && initialSHA.MatchString(r.CatalogSHA256) && r.Required > 0 && r.Applied == r.Required && r.Pending == 0 && (r.Lineage == "FRESH_SYNTHETIC" || r.Lineage == "RETAINED_SYNTHETIC")
}

// Only fixed catalog metadata and this profile's provenance row are read.
// Django's reviewed source executes its own graph under ENVIRONMENT=staging;
// identity seed migrations are guarded there. App/provider output is withheld.
const karafielSchemaCommand = `import contextlib,hashlib,io,json,logging,os,re,sys,time
phase='configuration';raw=None
try:
 for name in ('BANK_ALLOCATION_LEDGER_ENABLED','DHANAM_SOURCE_CURRENTNESS_ENABLED','FEATURE_CFDI_AUTO_ISSUE','FEATURE_GRANT_COMPLIANCE'):
  if os.environ.get(name)!='false': raise ValueError()
 if os.environ.get('ENVIRONMENT')!='staging' or os.environ.get('DB_NAME')!='karafiel_staging_sim' or os.environ.get('DB_USER')!='enclii_migrator' or os.environ.get('PGSSLMODE')!='verify-full': raise ValueError()
 source=os.environ.get('SOURCE_SHA','')
 if not re.fullmatch('[a-f0-9]{40}',source) or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9._-]{0,127}',os.environ.get('CLUSTER_UID','')): raise ValueError()
 phase='database';import psycopg2
 deadline=time.monotonic()+100
 while raw is None:
  try: raw=psycopg2.connect(host=os.environ['DB_HOST'],port=os.environ['DB_PORT'],dbname=os.environ['DB_NAME'],user=os.environ['DB_USER'],password=os.environ['DB_PASSWORD'],sslmode='verify-full',sslrootcert=os.environ['PGSSLROOTCERT'],connect_timeout=5)
  except psycopg2.OperationalError:
   if time.monotonic()>=deadline: raise
   time.sleep(2)
 raw.autocommit=True;cursor=raw.cursor()
 cursor.execute("SELECT pg_advisory_lock(1792966560)")
 cursor.execute("SELECT current_database(),current_user,r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication,d.datdba=r.oid,EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user")
 identity=cursor.fetchone()
 if identity!=('karafiel_staging_sim','enclii_migrator',False,True,False): raise ValueError()
 cursor.execute("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'")
 count=cursor.fetchone()[0];lineage='FRESH_SYNTHETIC'
 if count:
  phase='lineage';cursor.execute("SELECT profile,cluster_uid,graph_sha256 FROM public.enclii_stage_schema_provenance WHERE singleton=1")
  prior=cursor.fetchall()
  if len(prior)!=1 or prior[0][0]!='karafiel-staging-schema-v1' or prior[0][1]!=os.environ['CLUSTER_UID'] or not re.fullmatch('[a-f0-9]{64}',prior[0][2]): raise ValueError()
  lineage='RETAINED_SYNTHETIC'
 else:
  cursor.execute("CREATE TABLE public.enclii_stage_schema_provenance(singleton integer PRIMARY KEY CHECK(singleton=1),profile text NOT NULL,cluster_uid text NOT NULL,source_revision text NOT NULL,graph_sha256 text NOT NULL)")
  cursor.execute("INSERT INTO public.enclii_stage_schema_provenance VALUES(1,'karafiel-staging-schema-v1',%s,%s,'pending')",(os.environ['CLUSTER_UID'],source))
 phase='schema';logging.disable(logging.CRITICAL)
 os.environ['DJANGO_SETTINGS_MODULE']='karafiel.settings'
 with contextlib.redirect_stdout(io.StringIO()),contextlib.redirect_stderr(io.StringIO()):
  import django;django.setup()
  from django.core.management import call_command
  from django.db import connection
  from django.db.migrations.executor import MigrationExecutor
  call_command('migrate',interactive=False,verbosity=0)
  executor=MigrationExecutor(connection);required=sorted(executor.loader.graph.nodes)
  pending=executor.migration_plan(executor.loader.graph.leaf_nodes())
  applied=set(executor.loader.applied_migrations)
 if not required or pending or not set(required).issubset(applied): raise ValueError()
 graph=hashlib.sha256(json.dumps(required,separators=(',',':')).encode()).hexdigest()
 cursor.execute("UPDATE public.enclii_stage_schema_provenance SET source_revision=%s,graph_sha256=%s WHERE singleton=1 AND profile='karafiel-staging-schema-v1'",(source,graph))
 if cursor.rowcount!=1: raise ValueError()
 cursor.execute("""` + KarafielSchemaCatalogSQL + `""")
 catalog=hashlib.sha256(cursor.fetchone()[0].encode()).hexdigest()
 print(json.dumps(dict(schema='karafiel-staging-schema-v1',source_revision=source,database='karafiel_staging_sim',owner_role='enclii_migrator',graph_sha256=graph,catalog_sha256=catalog,applied=len(required),required=len(required),pending=0,lineage=lineage),separators=(',',':')))
except BaseException:
 sys.stderr.write('Karafiel staging schema refused ('+phase+').\n');sys.exit(1)
finally:
 if raw is not None: raw.close()
`

// NativeJobName exposes only the server-built operation identity, never credentials.
func (p IsolatedInitialJobPlan) NativeJobName() string { return p.jobName() }

package export

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type JobPgDumpProvider struct {
	log       *logrus.Logger
	k8sClient *k8s.Client
	r2Client  *storage.R2Client
	r2Prefix  string
	Timeout   time.Duration
}

func NewJobPgDumpProvider(log *logrus.Logger, k8sClient *k8s.Client, r2Client *storage.R2Client, prefix string) *JobPgDumpProvider {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &JobPgDumpProvider{
		log:       log,
		k8sClient: k8sClient,
		r2Client:  r2Client,
		r2Prefix:  prefix,
		Timeout:   30 * time.Minute,
	}
}

func (p *JobPgDumpProvider) Dump(ctx context.Context, addons []*types.DatabaseAddon) ([]DBDump, error) {
	var out []DBDump
	for _, a := range addons {
		if a == nil || a.Type != types.DatabaseAddonTypePostgres {
			continue
		}
		dump, err := p.dumpOne(ctx, a)
		if err != nil {
			p.log.WithError(err).WithField("addon", a.Name).Warn("tenant export: job pg_dump failed; continuing with other addons")
			out = append(out, DBDump{
				AddonName: a.Name,
				AddonMeta: a,
				SchemaSQL: []byte(fmt.Sprintf("-- pg_dump failed: %v\n", err)),
			})
			continue
		}
		out = append(out, dump)
	}
	return out, nil
}

func (p *JobPgDumpProvider) dumpOne(ctx context.Context, a *types.DatabaseAddon) (DBDump, error) {
	dumpCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	dumpKey := fmt.Sprintf("%s/db-dumps/%s/dump.sql.gz", p.r2Prefix, a.ID.String())
	schemaKey := fmt.Sprintf("%s/db-dumps/%s/schema.sql", p.r2Prefix, a.ID.String())

	dumpURL, err := p.r2Client.GetPresignedUploadURL(dumpCtx, dumpKey, "application/gzip", p.Timeout)
	if err != nil {
		return DBDump{}, fmt.Errorf("presign dump URL: %w", err)
	}
	schemaURL, err := p.r2Client.GetPresignedUploadURL(dumpCtx, schemaKey, "application/sql", p.Timeout)
	if err != nil {
		return DBDump{}, fmt.Errorf("presign schema URL: %w", err)
	}

	jobName := fmt.Sprintf("pg-dump-%s", a.ID.String()[:8])
	ns := a.K8sNamespace
	if ns == "" {
		ns = "default"
	}

	if a.ConnectionSecret == "" {
		return DBDump{}, fmt.Errorf("addon has no connection secret")
	}

	// build script
	script := fmt.Sprintf(`
set -e
export PGPASSWORD=$DB_PASSWORD
echo "Starting schema dump..."
pg_dump -h %s -p %d -U %s -d %s --no-password --schema-only --no-owner > schema.sql
curl -f -s -S -X PUT -T schema.sql "%s"

echo "Starting data dump..."
pg_dump -h %s -p %d -U %s -d %s --no-password --format=custom --verbose --compress=0 | gzip > dump.sql.gz
SIZE=$(stat -c%%s dump.sql.gz)
SHA=$(sha256sum dump.sql.gz | awk '{print $1}')
curl -f -s -S -X PUT -T dump.sql.gz "%s"

echo "DUMP_METADATA: SIZE=$SIZE SHA256=$SHA"
`, 
		a.Host, portOr(a.Port, 5432), a.Username, a.DatabaseName, schemaURL,
		a.Host, portOr(a.Port, 5432), a.Username, a.DatabaseName, dumpURL)

	backoffLimit := int32(0)
	ttl := int32(3600) // 1 hr cleanup
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "pg-dump",
							Image:   "postgres:15-alpine",
							Command: []string{"sh", "-c", script},
							Env: []corev1.EnvVar{
								{
									Name: "DB_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: a.ConnectionSecret,
											},
											Key: "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	p.log.WithField("job", jobName).Info("Creating pg_dump Job")
	_, err = p.k8sClient.CreateJob(dumpCtx, ns, job)
	if err != nil {
		return DBDump{}, fmt.Errorf("create job: %w", err)
	}

	err = p.k8sClient.WaitForJob(dumpCtx, ns, jobName, p.Timeout)
	if err != nil {
		return DBDump{}, fmt.Errorf("wait job: %w", err)
	}

	logs, err := p.k8sClient.GetJobLogs(dumpCtx, ns, jobName)
	if err != nil {
		return DBDump{}, fmt.Errorf("get job logs: %w", err)
	}

	var size int64
	var sha string
	for _, line := range strings.Split(logs, "\n") {
		if strings.HasPrefix(line, "DUMP_METADATA: ") {
			parts := strings.Split(strings.TrimPrefix(line, "DUMP_METADATA: "), " ")
			for _, part := range parts {
				if strings.HasPrefix(part, "SIZE=") {
					size, _ = strconv.ParseInt(strings.TrimPrefix(part, "SIZE="), 10, 64)
				}
				if strings.HasPrefix(part, "SHA256=") {
					sha = "sha256:" + strings.TrimPrefix(part, "SHA256=")
				}
			}
		}
	}

	if size == 0 || sha == "" {
		snippet := logs
		if len(snippet) > 500 {
			snippet = snippet[len(snippet)-500:]
		}
		return DBDump{}, fmt.Errorf("failed to extract size/sha from logs. Logs snippet: %s", snippet)
	}

	schemaRc, err := p.r2Client.Download(ctx, schemaKey)
	var schemaBytes []byte
	if err == nil {
		defer schemaRc.Close()
		schemaBytes, _ = io.ReadAll(schemaRc)
	} else {
		p.log.WithError(err).Warn("failed to fetch schema from R2")
	}

	return DBDump{
		AddonName:  a.Name,
		AddonMeta:  a,
		DumpReader: func() (io.ReadCloser, error) { return p.r2Client.Download(context.Background(), dumpKey) },
		DumpSize:   size,
		DumpSHA256: sha,
		SchemaSQL:  schemaBytes,
	}, nil
}

func portOr(p, def int) int {
	if p == 0 {
		return def
	}
	return p
}

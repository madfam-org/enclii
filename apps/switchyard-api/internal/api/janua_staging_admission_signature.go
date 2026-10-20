package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const januaStagePostgresSigner = `^https://github\.com/cloudnative-pg/postgres-containers/\.github/workflows/[^@]+@refs/(heads|tags|pull)/.+$`

func januaStagePostgresSignatureArgs() []string {
	return []string{"verify", "--certificate-identity-regexp", januaStagePostgresSigner, "--certificate-oidc-issuer", cosignOIDCIssuer, "--certificate-github-workflow-repository", "cloudnative-pg/postgres-containers", januaStagePostgresImage}
}

// Reuse Enclii's native exec.CommandContext cosign/cache mechanism, never a
// shell or cluster exec. The reviewed descriptor binds the exact catalog digest;
// the fixed official workflow identity verifies its producer. No claim is made
// about a main-ref SHA: official build signatures may originate in a PR workflow.
func verifyJanuaStageDatabaseProducer(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cache, err := os.MkdirTemp("", "enclii-janua-stage-cosign-")
	if err != nil {
		return fmt.Errorf("producer verifier unavailable")
	}
	defer os.RemoveAll(cache)
	if os.WriteFile(cache+"/config.json", []byte(`{"auths":{}}`), 0600) != nil {
		return fmt.Errorf("public producer verifier custody unavailable")
	}
	command := exec.CommandContext(ctx, "cosign", januaStagePostgresSignatureArgs()...)
	command.Env = januaStagePublicVerifierEnv(os.Environ(), cache)
	// Fixed public producer: no registry credential read/copy, stderr capture,
	// raw verifier error propagation or unbounded signature output.
	output := &januaStageSignatureBuffer{}
	command.Stdout = output
	if command.Run() != nil {
		return fmt.Errorf("fixed producer signature refused")
	}
	return januaStageSignatureDigestMatches(output.Bytes())
}

func januaStagePublicVerifierEnv(base []string, cache string) []string {
	// The public image requires no registry/cloud credential. Inherit only
	// executable lookup and optional public trust-store paths; never API/Vault,
	// GitHub, cloud, registry auth, proxy credentials or service-account hints.
	var environment []string
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PATH" || key == "SSL_CERT_FILE" || key == "SSL_CERT_DIR" {
			environment = append(environment, entry)
		}
	}
	return envWithOverrides(cosignVerifyEnv(environment, cache), map[string]string{"DOCKER_CONFIG": cache, "XDG_CONFIG_HOME": cache, "TMPDIR": cache})
}

type januaStageSignatureBuffer struct{ bytes.Buffer }

func (b *januaStageSignatureBuffer) Write(value []byte) (int, error) {
	if b.Len()+len(value) > 1024*1024 {
		return 0, fmt.Errorf("signature receipt too large")
	}
	return b.Buffer.Write(value)
}

func januaStageSignatureDigestMatches(output []byte) error {
	var claims []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
	}
	if json.Unmarshal(output, &claims) != nil || len(claims) == 0 {
		return fmt.Errorf("fixed producer receipt missing")
	}
	_, digest, _ := strings.Cut(januaStagePostgresImage, "@")
	for _, claim := range claims {
		if claim.Critical.Image.Digest == digest {
			return nil
		}
	}
	return fmt.Errorf("fixed producer digest mismatch")
}

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func bootstrapSignatureArgs(p isolatedBootstrapProfile, image, revision, event string) []string {
	args := []string{"verify", "--certificate-identity-regexp", p.SignerIdentity,
		"--certificate-oidc-issuer", cosignOIDCIssuer,
		"--certificate-github-workflow-repository", p.Producer,
		"--certificate-github-workflow-sha", revision,
		"--certificate-github-workflow-ref", "refs/heads/main",
		"--certificate-github-workflow-trigger", event}
	if p.Name == k8s.KarafielSchemaProfile {
		args = append(args, "--annotations", "source_sha="+revision, "--annotations", "candidate_run_id="+strconv.FormatInt(p.CandidateRun, 10))
	}
	return append(args, image)
}

// Reuse the existing writable-cache environment, but bind the signer to the
// exact closed producer/caller SHA/ref. Registry credentials use a private
// transient Docker config, never process arguments or returned command output.
func verifyBootstrapImageSignature(ctx context.Context, p isolatedBootstrapProfile, image, revision, event, user, password string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cache, err := os.MkdirTemp("", "enclii-bootstrap-cosign-")
	if err != nil {
		return fmt.Errorf("signature cache unavailable")
	}
	defer os.RemoveAll(cache)
	environment := cosignVerifyEnv(os.Environ(), cache)
	if user != "" && password != "" {
		config, _ := json.Marshal(map[string]any{"auths": map[string]any{"ghcr.io": map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(user + ":" + password))}}})
		if os.WriteFile(cache+"/config.json", config, 0600) != nil {
			return fmt.Errorf("signature registry custody unavailable")
		}
		environment = envWithOverrides(environment, map[string]string{"DOCKER_CONFIG": cache})
	}
	cmd := exec.CommandContext(timeoutCtx, "cosign", bootstrapSignatureArgs(p, image, revision, event)...)
	cmd.Env = environment
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("producer keyless signature refused")
	}
	var verified []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
		Optional map[string]interface{} `json:"optional"`
	}
	if json.Unmarshal(output, &verified) != nil || len(verified) == 0 {
		return fmt.Errorf("producer signature evidence missing")
	}
	for _, claim := range verified {
		if p.Name == k8s.KarafielSchemaProfile && (p.CandidateRun <= 0 || claim.Optional["source_sha"] != revision || claim.Optional["candidate_run_id"] != strconv.FormatInt(p.CandidateRun, 10)) {
			continue
		}
		if image == p.Image+"@"+claim.Critical.Image.Digest {
			return nil
		}
	}
	return fmt.Errorf("producer signature digest mismatch")
}

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
)

const januaStageAPISigner = `^https://github\.com/madfam-org/janua/\.github/workflows/docker-publish\.yml@refs/heads/main$`

func januaStageMigrationSignatureArgs(image, revision, event string) ([]string, error) {
	prefix := januastagemigrate.ImageRepository + "@sha256:"
	if !strings.HasPrefix(image, prefix) || !januaStagingIntentHash.MatchString(strings.TrimPrefix(image, prefix)) || !bootstrapSHA.MatchString(revision) || (event != "push" && event != "workflow_dispatch") {
		return nil, fmt.Errorf("closed Janua source signature required")
	}
	// Reuse the existing certificate SHA/ref/repository checks. No generic
	// bootstrap profile registration or native043 binding is added.
	p := isolatedBootstrapProfile{Name: januastagemigrate.Profile, Image: januastagemigrate.ImageRepository, Producer: "madfam-org/janua", SignerIdentity: januaStageAPISigner}
	args := bootstrapSignatureArgs(p, image, revision, event)
	return append(args[:len(args)-1], "--output", "json", image), nil
}

// Signature verification binds a source hint to the actual signed immutable
// digest and certificate. It alone does not prove CI, source graph, authority,
// isolation, native execution, a healthy stage or production promotion.
func verifyJanuaStageMigrationSignature(ctx context.Context, image, revision, event, user, password string) error {
	args, err := januaStageMigrationSignatureArgs(image, revision, event)
	if err != nil {
		return err
	}
	output, err := runJanuaStageMigrationCosign(ctx, args, user, password)
	if err != nil {
		return err
	}
	return januaStageMigrationSignatureDigest(image, output)
}

func runJanuaStageMigrationCosign(ctx context.Context, args []string, user, password string) ([]byte, error) {
	if (user == "") != (password == "") {
		return nil, fmt.Errorf("registry verifier custody incomplete")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cache, err := os.MkdirTemp("", "enclii-janua-migration-cosign-")
	if err != nil {
		return nil, fmt.Errorf("source signature verifier unavailable")
	}
	defer os.RemoveAll(cache)
	auths := map[string]any{}
	if user != "" {
		auths["ghcr.io"] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(user + ":" + password))}
	}
	config, _ := json.Marshal(map[string]any{"auths": auths})
	if os.WriteFile(cache+"/config.json", config, 0600) != nil {
		return nil, fmt.Errorf("source signature custody unavailable")
	}
	command := exec.CommandContext(ctx, "cosign", args...)
	command.Env = januaStagePublicVerifierEnv(os.Environ(), cache)
	output := &januaStageSignatureBuffer{}
	command.Stdout = output
	if command.Run() != nil {
		return nil, fmt.Errorf("Janua source signature refused")
	}
	return output.Bytes(), nil
}

func januaStageMigrationSignatureDigest(image string, output []byte) error {
	var claims []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
	}
	if json.Unmarshal(output, &claims) != nil || len(claims) == 0 {
		return fmt.Errorf("Janua source signature receipt missing")
	}
	for _, claim := range claims {
		if image == januastagemigrate.ImageRepository+"@"+claim.Critical.Image.Digest {
			return nil
		}
	}
	return fmt.Errorf("Janua source signature digest mismatch")
}

type januaMigrationCertificateHint struct{ Revision, Event string }

// The deployed verifier is cosign v3.1.3, whose PrintVerification JSON copies
// the certificate extensions into optional. Treat these only as candidate
// hints: an independent second verify with the exact SHA/event flags MUST pass.
// https://github.com/sigstore/cosign/blob/v3.1.3/cmd/cosign/cli/verify/common.go
func discoverJanuaMigrationCertificate(ctx context.Context, image, user, password string) (januaMigrationCertificateHint, error) {
	args, err := januaStageMigrationDiscoveryArgs(image)
	if err != nil {
		return januaMigrationCertificateHint{}, err
	}
	output, err := runJanuaStageMigrationCosign(ctx, args, user, password)
	if err != nil {
		return januaMigrationCertificateHint{}, err
	}
	return januaMigrationCertificateCandidate(image, output)
}

func januaStageMigrationDiscoveryArgs(image string) ([]string, error) {
	if !strings.HasPrefix(image, januastagemigrate.ImageRepository+"@sha256:") || !januaStagingIntentHash.MatchString(strings.TrimPrefix(image, januastagemigrate.ImageRepository+"@sha256:")) {
		return nil, januastagemigrate.ErrRefused
	}
	return []string{"verify", "--certificate-identity-regexp", januaStageAPISigner, "--certificate-oidc-issuer", cosignOIDCIssuer, "--certificate-github-workflow-repository", "madfam-org/janua", "--certificate-github-workflow-ref", "refs/heads/main", "--output", "json", image}, nil
}

func januaMigrationCertificateCandidate(image string, output []byte) (januaMigrationCertificateHint, error) {
	empty := januaMigrationCertificateHint{}
	if len(output) > 1<<20 || januaStageMigrationSignatureDigest(image, output) != nil {
		return empty, januastagemigrate.ErrRefused
	}
	var claims []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
		Optional map[string]any `json:"optional"`
	}
	if json.Unmarshal(output, &claims) != nil || len(claims) == 0 || len(claims) > 32 {
		return empty, januastagemigrate.ErrRefused
	}
	var hint januaMigrationCertificateHint
	for _, claim := range claims {
		if image != januastagemigrate.ImageRepository+"@"+claim.Critical.Image.Digest {
			return empty, januastagemigrate.ErrRefused
		}
		revision, shaOK := claim.Optional["githubWorkflowSha"].(string)
		event, eventOK := claim.Optional["githubWorkflowTrigger"].(string)
		if !shaOK || !eventOK || !bootstrapSHA.MatchString(revision) || (event != "push" && event != "workflow_dispatch") || claim.Optional["githubWorkflowRepository"] != "madfam-org/janua" || claim.Optional["githubWorkflowRef"] != "refs/heads/main" || claim.Optional["Issuer"] != cosignOIDCIssuer || claim.Optional["Subject"] != "https://github.com/madfam-org/janua/.github/workflows/docker-publish.yml@refs/heads/main" {
			return empty, januastagemigrate.ErrRefused
		}
		candidate := januaMigrationCertificateHint{Revision: revision, Event: event}
		if hint != empty && hint != candidate {
			return empty, januastagemigrate.ErrRefused
		}
		hint = candidate
	}
	return hint, nil
}

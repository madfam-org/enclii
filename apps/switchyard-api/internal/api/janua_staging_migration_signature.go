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
	return bootstrapSignatureArgs(p, image, revision, event), nil
}

// Signature verification binds a source hint to the actual signed immutable
// digest and certificate. It alone does not prove CI, source graph, authority,
// isolation, native execution, a healthy stage or production promotion.
func verifyJanuaStageMigrationSignature(ctx context.Context, image, revision, event, user, password string) error {
	args, err := januaStageMigrationSignatureArgs(image, revision, event)
	if err != nil {
		return err
	}
	if (user == "") != (password == "") {
		return fmt.Errorf("registry verifier custody incomplete")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cache, err := os.MkdirTemp("", "enclii-janua-migration-cosign-")
	if err != nil {
		return fmt.Errorf("source signature verifier unavailable")
	}
	defer os.RemoveAll(cache)
	auths := map[string]any{}
	if user != "" {
		auths["ghcr.io"] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(user + ":" + password))}
	}
	config, _ := json.Marshal(map[string]any{"auths": auths})
	if os.WriteFile(cache+"/config.json", config, 0600) != nil {
		return fmt.Errorf("source signature custody unavailable")
	}
	command := exec.CommandContext(ctx, "cosign", args...)
	command.Env = januaStagePublicVerifierEnv(os.Environ(), cache)
	output := &januaStageSignatureBuffer{}
	command.Stdout = output
	if command.Run() != nil {
		return fmt.Errorf("Janua source signature refused")
	}
	var claims []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
	}
	if json.Unmarshal(output.Bytes(), &claims) != nil || len(claims) == 0 {
		return fmt.Errorf("Janua source signature receipt missing")
	}
	for _, claim := range claims {
		if image == januastagemigrate.ImageRepository+"@"+claim.Critical.Image.Digest {
			return nil
		}
	}
	return fmt.Errorf("Janua source signature digest mismatch")
}

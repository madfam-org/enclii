package api

import (
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	"github.com/stretchr/testify/require"
)

func januaCertificateClaim(image, revision string) map[string]any {
	return map[string]any{
		"critical": map[string]any{"image": map[string]any{"docker-manifest-digest": strings.TrimPrefix(image, januastagemigrate.ImageRepository+"@")}},
		"optional": map[string]any{"Issuer": cosignOIDCIssuer, "Subject": "https://github.com/madfam-org/janua/.github/workflows/docker-publish.yml@refs/heads/main", "githubWorkflowSha": revision, "githubWorkflowTrigger": "push", "githubWorkflowRepository": "madfam-org/janua", "githubWorkflowRef": "refs/heads/main"},
	}
}

func TestJanuaMigrationCertificateDiscoveryIsBoundedAndNeverAnExactSHABypass(t *testing.T) {
	image := januastagemigrate.ImageRepository + "@sha256:" + strings.Repeat("a", 64)
	revision := strings.Repeat("b", 40)
	args, err := januaStageMigrationDiscoveryArgs(image)
	require.NoError(t, err)
	joined := strings.Join(args, " ")
	for _, filter := range []string{"--certificate-identity-regexp " + januaStageAPISigner, "--certificate-oidc-issuer " + cosignOIDCIssuer, "--certificate-github-workflow-repository madfam-org/janua", "--certificate-github-workflow-ref refs/heads/main"} {
		require.Contains(t, joined, filter)
	}
	require.NotContains(t, joined, "--certificate-github-workflow-sha")
	for _, forbidden := range []string{"--insecure", "--ignore", "--key", "--annotations", "--local-image"} {
		require.NotContains(t, joined, forbidden)
	}
	require.Equal(t, image, args[len(args)-1])
	for _, scenario := range []string{"verified hint", "missing optional", "missing SHA", "payload SHA type", "foreign issuer", "foreign subject", "foreign repository", "foreign ref", "invalid event", "foreign digest", "ambiguous certificates", "too many claims", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			claim := januaCertificateClaim(image, revision)
			optional := claim["optional"].(map[string]any)
			claims := []any{claim}
			switch scenario {
			case "missing optional":
				delete(claim, "optional")
			case "missing SHA":
				delete(optional, "githubWorkflowSha")
			case "payload SHA type":
				optional["githubWorkflowSha"] = []string{revision}
			case "foreign issuer":
				optional["Issuer"] = "https://issuer.invalid"
			case "foreign subject":
				optional["Subject"] = "https://github.com/fixture/other/.github/workflows/docker-publish.yml@refs/heads/main"
			case "foreign repository":
				optional["githubWorkflowRepository"] = "fixture/other"
			case "foreign ref":
				optional["githubWorkflowRef"] = "refs/heads/fixture"
			case "invalid event":
				optional["githubWorkflowTrigger"] = "pull_request"
			case "foreign digest":
				claim["critical"].(map[string]any)["image"].(map[string]any)["docker-manifest-digest"] = "sha256:" + strings.Repeat("f", 64)
			case "ambiguous certificates":
				claims = append(claims, januaCertificateClaim(image, strings.Repeat("c", 40)))
			case "too many claims":
				for len(claims) < 33 {
					claims = append(claims, claim)
				}
			}
			output := []byte(bootstrapJSON(claims))
			if scenario == "oversize" {
				output = append(output, []byte(strings.Repeat(" ", 1<<20))...)
			}
			hint, err := januaMigrationCertificateCandidate(image, output)
			if scenario != "verified hint" {
				require.Error(t, err)
				require.Empty(t, hint)
				return
			}
			require.NoError(t, err)
			require.Equal(t, revision, hint.Revision)
			exact, err := januaStageMigrationSignatureArgs(image, hint.Revision, hint.Event)
			require.NoError(t, err)
			require.Contains(t, strings.Join(exact, " "), "--certificate-github-workflow-sha "+revision)
			require.Contains(t, strings.Join(exact, " "), "--certificate-github-workflow-trigger push")
		})
	}
	_, err = januaStageMigrationDiscoveryArgs(januastagemigrate.ImageRepository + ":main")
	require.Error(t, err)
}

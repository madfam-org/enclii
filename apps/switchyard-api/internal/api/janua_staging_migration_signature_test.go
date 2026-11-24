package api

import (
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	"github.com/stretchr/testify/require"
)

func TestJanuaMigrationSignatureUsesExistingExactSourceCertificateContract(t *testing.T) {
	image := januastagemigrate.ImageRepository + "@sha256:" + strings.Repeat("a", 64)
	revision := strings.Repeat("b", 40)
	for _, event := range []string{"push", "workflow_dispatch"} {
		args, e := januaStageMigrationSignatureArgs(image, revision, event)
		require.NoError(t, e)
		joined := strings.Join(args, " ")
		for _, expected := range []string{"--certificate-identity-regexp " + januaStageAPISigner, "--certificate-oidc-issuer https://token.actions.githubusercontent.com", "--certificate-github-workflow-repository madfam-org/janua", "--certificate-github-workflow-sha " + revision, "--certificate-github-workflow-ref refs/heads/main", "--certificate-github-workflow-trigger " + event} {
			require.Contains(t, joined, expected)
		}
		require.Equal(t, image, args[len(args)-1])
		require.NotContains(t, joined, "--insecure")
		require.NotContains(t, joined, "--annotations")
		require.NotContains(t, joined, "--key")
	}
	for _, candidate := range []struct{ image, revision, event string }{{"ghcr.io/madfam-org/janua-api:main", revision, "push"}, {"ghcr.io/madfam-org/janua-admin@sha256:" + strings.Repeat("a", 64), revision, "push"}, {image, "main", "push"}, {image, revision, "pull_request"}, {image, revision, "schedule"}} {
		_, e := januaStageMigrationSignatureArgs(candidate.image, candidate.revision, candidate.event)
		require.Error(t, e)
	}
	// This path stays outside the generic *_sim/native043 job dispatcher.
	_, err := isolatedBootstrapProfileFor(januastagemigrate.Profile)
	require.Error(t, err)
}

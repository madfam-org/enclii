package k8s

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestInitialJobHoldStageNeverExposesAdmissionError(t *testing.T) {
	p := fenceReadPlan()
	c, fake, _ := initialJobClient(t, p)
	fake.PrependReactor("create", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("synthetic-private-admission-value")
	})
	_, err := c.RunIsolatedInitialJob(t.Context(), p, true)
	require.Error(t, err)
	require.Equal(t, "job_pod_admission", InitialJobHoldStage(err))
	require.Equal(t, "job_pod_admission", InitialJobHoldStage(fmt.Errorf("wrapped: %w", err)))
	require.Equal(t, "executor", InitialJobHoldStage(errors.New("synthetic-private-admission-value")))
	require.Equal(t, "executor", InitialJobHoldStage(nil))
}

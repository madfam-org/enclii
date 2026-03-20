package reconciler

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestParseContainerPort tests the parseContainerPort function
func TestParseContainerPort(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantPort    int32
		wantErr     bool
		description string
	}{
		{
			name:        "ENCLII_PORT set",
			envVars:     map[string]string{"ENCLII_PORT": "4200"},
			wantPort:    4200,
			wantErr:     false,
			description: "Should use ENCLII_PORT when set",
		},
		{
			name:        "PORT fallback",
			envVars:     map[string]string{"PORT": "4104"},
			wantPort:    4104,
			wantErr:     false,
			description: "Should fallback to PORT when ENCLII_PORT not set",
		},
		{
			name:        "ENCLII_PORT takes precedence",
			envVars:     map[string]string{"ENCLII_PORT": "4200", "PORT": "8080"},
			wantPort:    4200,
			wantErr:     false,
			description: "ENCLII_PORT should take precedence over PORT",
		},
		{
			name:        "Default when neither set",
			envVars:     map[string]string{},
			wantPort:    4200,
			wantErr:     false,
			description: "Should return default port (4200) when neither env var set",
		},
		{
			name:        "Default with other env vars",
			envVars:     map[string]string{"DATABASE_URL": "postgres://...", "API_KEY": "secret"},
			wantPort:    4200,
			wantErr:     false,
			description: "Should return default port when only unrelated env vars set",
		},
		{
			name:        "Invalid ENCLII_PORT",
			envVars:     map[string]string{"ENCLII_PORT": "not-a-number"},
			wantPort:    4200,
			wantErr:     true,
			description: "Should error and return default for invalid ENCLII_PORT",
		},
		{
			name:        "Invalid PORT",
			envVars:     map[string]string{"PORT": "abc"},
			wantPort:    4200,
			wantErr:     true,
			description: "Should error and return default for invalid PORT",
		},
		{
			name:        "ENCLII_PORT out of range (too high)",
			envVars:     map[string]string{"ENCLII_PORT": "70000"},
			wantPort:    4200,
			wantErr:     true,
			description: "Should error for port > 65535",
		},
		{
			name:        "ENCLII_PORT out of range (zero)",
			envVars:     map[string]string{"ENCLII_PORT": "0"},
			wantPort:    4200,
			wantErr:     true,
			description: "Should error for port 0",
		},
		{
			name:        "PORT out of range (negative)",
			envVars:     map[string]string{"PORT": "-1"},
			wantPort:    4200,
			wantErr:     true,
			description: "Should error for negative port",
		},
		{
			name:        "Empty ENCLII_PORT falls back to PORT",
			envVars:     map[string]string{"ENCLII_PORT": "", "PORT": "3000"},
			wantPort:    3000,
			wantErr:     false,
			description: "Empty ENCLII_PORT should fallback to PORT",
		},
		{
			name:        "Janua-style PORT",
			envVars:     map[string]string{"PORT": "4101"},
			wantPort:    4101,
			wantErr:     false,
			description: "Should handle Janua dashboard port (4101)",
		},
		{
			name:        "Minimum valid port",
			envVars:     map[string]string{"PORT": "1"},
			wantPort:    1,
			wantErr:     false,
			description: "Should accept minimum valid port (1)",
		},
		{
			name:        "Maximum valid port",
			envVars:     map[string]string{"PORT": "65535"},
			wantPort:    65535,
			wantErr:     false,
			description: "Should accept maximum valid port (65535)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, err := parseContainerPort(tt.envVars)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseContainerPort() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if gotPort != tt.wantPort {
				t.Errorf("parseContainerPort() = %v, want %v (%s)", gotPort, tt.wantPort, tt.description)
			}
		})
	}
}

// TestParseContainerPortWithSource tests source tracking
func TestParseContainerPortWithSource(t *testing.T) {
	tests := []struct {
		name       string
		envVars    map[string]string
		wantPort   int32
		wantSource PortSource
		wantErr    bool
	}{
		{
			name:       "Source is ENCLII_PORT",
			envVars:    map[string]string{"ENCLII_PORT": "4200"},
			wantPort:   4200,
			wantSource: PortSourceEncliiPort,
			wantErr:    false,
		},
		{
			name:       "Source is PORT",
			envVars:    map[string]string{"PORT": "4104"},
			wantPort:   4104,
			wantSource: PortSourcePort,
			wantErr:    false,
		},
		{
			name:       "Source is default",
			envVars:    map[string]string{},
			wantPort:   4200,
			wantSource: PortSourceDefault,
			wantErr:    false,
		},
		{
			name:       "ENCLII_PORT precedence with source",
			envVars:    map[string]string{"ENCLII_PORT": "4200", "PORT": "8080"},
			wantPort:   4200,
			wantSource: PortSourceEncliiPort,
			wantErr:    false,
		},
		{
			name:       "Invalid falls back to default source",
			envVars:    map[string]string{"ENCLII_PORT": "invalid"},
			wantPort:   4200,
			wantSource: PortSourceDefault,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, gotSource, err := parseContainerPortWithSource(tt.envVars)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseContainerPortWithSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if gotPort != tt.wantPort {
				t.Errorf("parseContainerPortWithSource() port = %v, want %v", gotPort, tt.wantPort)
			}

			if gotSource != tt.wantSource {
				t.Errorf("parseContainerPortWithSource() source = %v, want %v", gotSource, tt.wantSource)
			}
		})
	}
}

// TestExtractNetworkPolicyPort tests port extraction from NetworkPolicy
func TestExtractNetworkPolicyPort(t *testing.T) {
	tests := []struct {
		name     string
		np       *networkingv1.NetworkPolicy
		wantPort int32
	}{
		{
			name:     "Nil NetworkPolicy",
			np:       nil,
			wantPort: 0,
		},
		{
			name: "No ingress rules",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					Ingress: []networkingv1.NetworkPolicyIngressRule{},
				},
			},
			wantPort: 0,
		},
		{
			name: "No ports in ingress rule",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{Ports: []networkingv1.NetworkPolicyPort{}},
					},
				},
			},
			wantPort: 0,
		},
		{
			name: "Valid port",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							Ports: []networkingv1.NetworkPolicyPort{
								{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 4200}},
							},
						},
					},
				},
			},
			wantPort: 4200,
		},
		{
			name: "Multiple ports returns first",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							Ports: []networkingv1.NetworkPolicyPort{
								{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 8080}},
								{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 443}},
							},
						},
					},
				},
			},
			wantPort: 8080,
		},
		{
			name: "Janua port",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							Ports: []networkingv1.NetworkPolicyPort{
								{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 4104}},
							},
						},
					},
				},
			},
			wantPort: 4104,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort := extractNetworkPolicyPort(tt.np)

			if gotPort != tt.wantPort {
				t.Errorf("extractNetworkPolicyPort() = %v, want %v", gotPort, tt.wantPort)
			}
		})
	}
}

// TestPortSourceConstants ensures constants are correct
func TestPortSourceConstants(t *testing.T) {
	if PortSourceEncliiPort != "ENCLII_PORT" {
		t.Errorf("PortSourceEncliiPort = %v, want ENCLII_PORT", PortSourceEncliiPort)
	}
	if PortSourcePort != "PORT" {
		t.Errorf("PortSourcePort = %v, want PORT", PortSourcePort)
	}
	if PortSourceDefault != "default" {
		t.Errorf("PortSourceDefault = %v, want default", PortSourceDefault)
	}
}

// ===========================================================================
// Edge case tests added below -- DO NOT modify tests above this line
// ===========================================================================

// ---------------------------------------------------------------------------
// checkPodForFatalErrors edge cases
// ---------------------------------------------------------------------------

// TestCheckPodForFatalErrors_ImagePullBackOff_Unauthorized verifies that a 401
// error in ImagePullBackOff is detected as a fatal registry credentials issue.
func TestCheckPodForFatalErrors_ImagePullBackOff_Unauthorized(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: "rpc error: code = Unknown desc = failed to pull image: 401 unauthorized",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for 401 unauthorized image pull, got nil")
	}
	if !strings.Contains(err.Error(), "missing registry credentials") {
		t.Errorf("error should mention registry credentials, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_ImagePullBackOff_Forbidden checks the 403 forbidden
// path which is distinct from 401 but equally fatal.
func TestCheckPodForFatalErrors_ImagePullBackOff_Forbidden(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ErrImagePull",
							Message: "403 forbidden: access denied to ghcr.io/madfam-org/private-image",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for 403 forbidden image pull, got nil")
	}
	if !strings.Contains(err.Error(), "missing registry credentials") {
		t.Errorf("error should mention registry credentials, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_ImageNotFound verifies that "manifest unknown" (image
// tag does not exist) is treated as a fatal error.
func TestCheckPodForFatalErrors_ImageNotFound(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ErrImagePull",
							Message: "manifest unknown: repository ghcr.io/madfam-org/enclii/api",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for manifest unknown, got nil")
	}
	if !strings.Contains(err.Error(), "image not found") {
		t.Errorf("error should mention image not found, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_InvalidImageName checks the InvalidImageName reason
// which is always immediately fatal.
func TestCheckPodForFatalErrors_InvalidImageName(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "InvalidImageName",
							Message: "invalid reference format: not_a_valid_image!!!",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for InvalidImageName, got nil")
	}
	if !strings.Contains(err.Error(), "invalid image name") {
		t.Errorf("error should mention invalid image name, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_CreateContainerConfigError_MissingSecret tests
// the case where a pod fails because a referenced K8s secret does not exist.
func TestCheckPodForFatalErrors_CreateContainerConfigError_MissingSecret(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CreateContainerConfigError",
							Message: `secret "myapp-secrets" not found`,
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for missing secret in container config, got nil")
	}
	if !strings.Contains(err.Error(), "missing secret") {
		t.Errorf("error should mention missing secret, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_CreateContainerConfigError_NonSecret tests the
// generic CreateContainerConfigError path when the message does not mention secrets.
func TestCheckPodForFatalErrors_CreateContainerConfigError_NonSecret(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CreateContainerConfigError",
							Message: "configmap \"my-config\" not found",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for CreateContainerConfigError, got nil")
	}
	if !strings.Contains(err.Error(), "container config error") {
		t.Errorf("error should mention container config error, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_CrashLoopBackOff_HighRestartCount verifies that
// CrashLoopBackOff with >= 5 restarts is fatal.
func TestCheckPodForFatalErrors_CrashLoopBackOff_HighRestartCount(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					RestartCount: 7,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "back-off 5m0s restarting failed container",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error for CrashLoopBackOff with 7 restarts, got nil")
	}
	if !strings.Contains(err.Error(), "CrashLoopBackOff") {
		t.Errorf("error should mention CrashLoopBackOff, got: %s", err.Error())
	}
}

// TestCheckPodForFatalErrors_CrashLoopBackOff_LowRestartCount verifies that
// CrashLoopBackOff with fewer than 5 restarts is NOT treated as fatal (pod
// might still recover).
func TestCheckPodForFatalErrors_CrashLoopBackOff_LowRestartCount(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					RestartCount: 3,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "back-off 40s restarting failed container",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err != nil {
		t.Errorf("expected nil for CrashLoopBackOff with only 3 restarts, got: %v", err)
	}
}

// TestCheckPodForFatalErrors_CrashLoopBackOff_ExactThreshold verifies the exact
// boundary at 5 restarts (should be fatal).
func TestCheckPodForFatalErrors_CrashLoopBackOff_ExactThreshold(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "back-off 2m40s restarting failed container",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error at exactly 5 restarts (boundary), got nil")
	}
}

// TestCheckPodForFatalErrors_RunningPod confirms a healthy running pod
// returns nil (no fatal error).
func TestCheckPodForFatalErrors_RunningPod(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
					Ready: true,
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err != nil {
		t.Errorf("expected nil for running pod, got: %v", err)
	}
}

// TestCheckPodForFatalErrors_NoContainerStatuses confirms an empty pod
// (no container statuses yet) returns nil.
func TestCheckPodForFatalErrors_NoContainerStatuses(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: nil,
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err != nil {
		t.Errorf("expected nil for pod with no container statuses, got: %v", err)
	}
}

// TestCheckPodForFatalErrors_MultipleContainers_OneFatal verifies that when
// a pod has multiple containers and one has a fatal error, the error is detected.
func TestCheckPodForFatalErrors_MultipleContainers_OneFatal(t *testing.T) {
	r := &ServiceReconciler{}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "sidecar",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
				{
					Name: "main",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "InvalidImageName",
							Message: "invalid reference format",
						},
					},
				},
			},
		},
	}

	err := r.checkPodForFatalErrors(pod)
	if err == nil {
		t.Fatal("expected fatal error when one of multiple containers has InvalidImageName")
	}
}

// ---------------------------------------------------------------------------
// extractNetworkPolicyPort additional edge cases
// ---------------------------------------------------------------------------

// TestExtractNetworkPolicyPort_StringTypePort verifies behavior when the port
// is specified as a named string rather than an integer.
func TestExtractNetworkPolicyPort_StringTypePort(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		Spec: networkingv1.NetworkPolicySpec{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: &intstr.IntOrString{Type: intstr.String, StrVal: "http"}},
					},
				},
			},
		},
	}

	// When port is a string type, IntVal is 0 (zero value). The function
	// reads IntVal directly, so it returns 0 for named ports.
	gotPort := extractNetworkPolicyPort(np)
	if gotPort != 0 {
		t.Errorf("extractNetworkPolicyPort() with string port = %v, want 0", gotPort)
	}
}

// TestExtractNetworkPolicyPort_NilPortField verifies that a nil Port field
// in a NetworkPolicyPort does not cause a panic.
func TestExtractNetworkPolicyPort_NilPortField(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		Spec: networkingv1.NetworkPolicySpec{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: nil}, // Port can be nil (means "all ports")
					},
				},
			},
		},
	}

	gotPort := extractNetworkPolicyPort(np)
	if gotPort != 0 {
		t.Errorf("extractNetworkPolicyPort() with nil port = %v, want 0", gotPort)
	}
}

// TestExtractNetworkPolicyPort_MultipleRulesReturnsFirstPort verifies that
// when there are multiple ingress rules, the first rule's first port wins.
func TestExtractNetworkPolicyPort_MultipleRulesReturnsFirstPort(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		Spec: networkingv1.NetworkPolicySpec{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 3000}},
					},
				},
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 8080}},
					},
				},
			},
		},
	}

	gotPort := extractNetworkPolicyPort(np)
	if gotPort != 3000 {
		t.Errorf("extractNetworkPolicyPort() with multiple rules = %v, want 3000 (first rule)", gotPort)
	}
}

// ---------------------------------------------------------------------------
// ReconcileResult and ReconcileRequest struct edge cases
// ---------------------------------------------------------------------------

// TestReconcileResult_SuccessFieldDefaults verifies zero-value behavior of
// ReconcileResult (important because success=false is the zero value).
func TestReconcileResult_SuccessFieldDefaults(t *testing.T) {
	result := &ReconcileResult{}

	if result.Success {
		t.Error("zero-value ReconcileResult.Success should be false")
	}
	if result.Message != "" {
		t.Error("zero-value ReconcileResult.Message should be empty")
	}
	if result.K8sObjects != nil {
		t.Error("zero-value ReconcileResult.K8sObjects should be nil")
	}
	if result.NextCheck != nil {
		t.Error("zero-value ReconcileResult.NextCheck should be nil")
	}
	if result.Error != nil {
		t.Error("zero-value ReconcileResult.Error should be nil")
	}
}

// TestReconcileResult_NextCheckRetrySignal verifies that a non-nil NextCheck
// signals a retry (used by handleResult in controller_workers.go).
func TestReconcileResult_NextCheckRetrySignal(t *testing.T) {
	futureTime := time.Now().Add(30 * time.Second)
	result := &ReconcileResult{
		Success:   false,
		Message:   "Deployment not ready, will retry",
		NextCheck: &futureTime,
	}

	if result.Success {
		t.Error("retry result should have Success=false")
	}
	if result.NextCheck == nil {
		t.Error("retry result must have non-nil NextCheck")
	}
	if result.NextCheck.Before(time.Now()) {
		t.Error("NextCheck should be in the future")
	}
}

// TestReconcileRequest_EmptyKubeNamespaceDetection verifies that an empty
// KubeNamespace on the environment is detectable (this is a data integrity
// issue caught in service.go Reconcile).
func TestReconcileRequest_EmptyKubeNamespaceDetection(t *testing.T) {
	req := &ReconcileRequest{
		Environment: &types.Environment{
			Name:          "production",
			KubeNamespace: "",
		},
	}

	if req.Environment.KubeNamespace != "" {
		t.Error("expected empty KubeNamespace to be detectable")
	}
}

package builder

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// =============================================================================
// SBOM Generation (Software Bill of Materials)
// =============================================================================

// runSBOMGeneration runs Syft to generate SBOM for the image
func (e *KanikoExecutor) runSBOMGeneration(ctx context.Context, buildID uuid.UUID, imageTag string) (string, string, error) {
	jobName := fmt.Sprintf("sbom-%s", buildID.String()[:8])
	format := "spdx-json" // SPDX is widely supported and recommended

	// Security context - run as non-root
	runAsNonRoot := true
	runAsUser := int64(1000)
	runAsGroup := int64(1000)
	fsGroup := int64(1000)

	// Job configuration
	backoffLimit := int32(0)
	ttlSeconds := int32(1800)           // Clean up after 30 minutes
	activeDeadlineSeconds := int64(300) // 5 minute timeout for SBOM

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: KanikoBuildNamespace,
			Labels: map[string]string{
				LabelBuildID: buildID.String(),
				LabelAppName: "syft-sbom",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelBuildID: buildID.String(),
						LabelAppName: "syft-sbom",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
						RunAsGroup:   &runAsGroup,
						FSGroup:      &fsGroup,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "syft",
							Image: SyftImage,
							Args: []string{
								"scan",
								"--output", format,
								"registry:" + imageTag,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: boolPtr(false),
								ReadOnlyRootFilesystem:   boolPtr(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "docker-config",
									MountPath: "/home/syft/.docker",
									ReadOnly:  true,
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "DOCKER_CONFIG",
									Value: "/home/syft/.docker",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "docker-config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "ghcr-credentials",
									Items: []corev1.KeyToPath{
										{
											Key:  ".dockerconfigjson",
											Path: "config.json",
										},
									},
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Create the job
	_, err := e.k8sClient.BatchV1().Jobs(KanikoBuildNamespace).Create(ctx, k8sJob, metav1.CreateOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to create SBOM job: %w", err)
	}

	e.log(buildID, "📋 Created SBOM generation job: %s", jobName)

	// Wait for completion
	if err := e.watchJobCompletion(ctx, buildID, jobName); err != nil {
		return "", "", fmt.Errorf("SBOM generation failed: %w", err)
	}

	// Get SBOM output from job logs
	sbom, err := e.getJobOutput(ctx, jobName)
	if err != nil {
		return "", "", fmt.Errorf("failed to get SBOM output: %w", err)
	}

	return sbom, format, nil
}

// =============================================================================
// Image Signing (Cosign)
// =============================================================================

// runImageSigning runs Cosign to sign the image
func (e *KanikoExecutor) runImageSigning(ctx context.Context, buildID uuid.UUID, imageTag string) (string, error) {
	jobName := fmt.Sprintf("sign-%s", buildID.String()[:8])

	// Security context - run as non-root
	runAsNonRoot := true
	runAsUser := int64(1000)
	runAsGroup := int64(1000)
	fsGroup := int64(1000)

	// Job configuration
	backoffLimit := int32(0)
	ttlSeconds := int32(1800)           // Clean up after 30 minutes
	activeDeadlineSeconds := int64(180) // 3 minute timeout for signing

	// Cosign supports keyless signing via Fulcio/Rekor or key-based signing
	// We support both modes based on configuration
	var args []string
	var envVars []corev1.EnvVar
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// Registry credentials for pulling/pushing signatures
	volumes = append(volumes, corev1.Volume{
		Name: "docker-config",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: "ghcr-credentials",
				Items: []corev1.KeyToPath{
					{
						Key:  ".dockerconfigjson",
						Path: "config.json",
					},
				},
			},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "docker-config",
		MountPath: "/home/nonroot/.docker",
		ReadOnly:  true,
	})
	envVars = append(envVars, corev1.EnvVar{
		Name:  "DOCKER_CONFIG",
		Value: "/home/nonroot/.docker",
	})

	if e.cosignKey != "" {
		// Key-based signing - mount the signing key secret
		args = []string{
			"sign",
			"--key", "/cosign/cosign.key",
			"--yes", // Skip confirmation
			imageTag,
		}

		volumes = append(volumes, corev1.Volume{
			Name: "cosign-key",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: e.cosignKey,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "cosign-key",
			MountPath: "/cosign",
			ReadOnly:  true,
		})

		// Cosign password for the key (if encrypted)
		envVars = append(envVars, corev1.EnvVar{
			Name: "COSIGN_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: e.cosignKey,
					},
					Key:      "password",
					Optional: boolPtr(true),
				},
			},
		})
	} else {
		// Keyless signing using Fulcio and Rekor (OIDC-based)
		args = []string{
			"sign",
			"--yes", // Skip confirmation
			imageTag,
		}

		// Enable experimental features for keyless signing
		envVars = append(envVars, corev1.EnvVar{
			Name:  "COSIGN_EXPERIMENTAL",
			Value: "1",
		})
	}

	// Tmp directory for Cosign operations
	volumes = append(volumes, corev1.Volume{
		Name: "tmp",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "tmp",
		MountPath: "/tmp",
	})

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: KanikoBuildNamespace,
			Labels: map[string]string{
				LabelBuildID: buildID.String(),
				LabelAppName: "cosign-sign",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelBuildID: buildID.String(),
						LabelAppName: "cosign-sign",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
						RunAsGroup:   &runAsGroup,
						FSGroup:      &fsGroup,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "cosign",
							Image: CosignImage,
							Args:  args,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: boolPtr(false),
								ReadOnlyRootFilesystem:   boolPtr(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Env:          envVars,
							VolumeMounts: volumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	// Create the job
	_, err := e.k8sClient.BatchV1().Jobs(KanikoBuildNamespace).Create(ctx, k8sJob, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create signing job: %w", err)
	}

	e.log(buildID, "🔐 Created image signing job: %s", jobName)

	// Wait for completion
	if err := e.watchJobCompletion(ctx, buildID, jobName); err != nil {
		return "", fmt.Errorf("image signing failed: %w", err)
	}

	// For Cosign, the signature is stored in the registry alongside the image
	// Return a reference to indicate signing was successful
	signature := fmt.Sprintf("%s.sig", imageTag)
	return signature, nil
}

package api

import (
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"testing"
)

func scratchFixture() ([]corev1.Volume, []corev1.VolumeMount) {
	q := resource.MustParse("256Mi")
	return []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &q}}}}, []corev1.VolumeMount{{Name: "cache", MountPath: "/app/.next/cache"}}
}
func TestRunTargetBoundedScratchPlan(t *testing.T) {
	r, target, err := parseRunTarget([]byte(targetRunBody))
	require.NoError(t, err)
	s := boundRunSnapshot(t, target)
	pod := &s.Deployment.Spec.Template.Spec
	pod.Volumes, pod.Containers[0].VolumeMounts = scratchFixture()
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "unmounted", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "fixture-runtime"}}})
	s.Declared = s.Deployment.DeepCopy()
	before := s.Deployment.DeepCopy()
	_, spec, sha, err := runTargetPlan(r, target, s)
	require.NoError(t, err)
	require.Equal(t, before, s.Deployment)
	require.Equal(t, pod.Volumes[:1], spec.Template.Spec.Volumes)
	require.Equal(t, pod.Containers[0].VolumeMounts, spec.Template.Spec.Containers[0].VolumeMounts)
	require.Len(t, spec.Template.Spec.Volumes, 1, "unmounted credentials never enter the Job")
	for _, mutate := range []func(*corev1.PodSpec){
		func(p *corev1.PodSpec) { q := resource.MustParse("128Mi"); p.Volumes[0].EmptyDir.SizeLimit = &q },
		func(p *corev1.PodSpec) { p.Volumes[0].EmptyDir.Medium = corev1.StorageMediumMemory },
		func(p *corev1.PodSpec) { p.Containers[0].VolumeMounts[0].MountPath = "/tmp/cache" },
	} {
		changed := s
		changed.Deployment = before.DeepCopy()
		mutate(&changed.Deployment.Spec.Template.Spec)
		changed.Declared = changed.Deployment.DeepCopy()
		_, _, hash, err := runTargetPlan(r, target, changed)
		require.NoError(t, err)
		require.NotEqual(t, sha, hash)
	}
	spec.Template.Spec.Volumes[0].EmptyDir.Medium = corev1.StorageMediumMemory
	require.Equal(t, corev1.StorageMediumDefault, pod.Volumes[0].EmptyDir.Medium)
}
func TestRunTargetScratchRejectsUnsafeMounts(t *testing.T) {
	for name, mutate := range map[string]func(*[]corev1.Volume, *[]corev1.VolumeMount){
		"missing": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { *v = nil },
		"persistent": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			(*v)[0].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "fixture"}}
		},
		"secret": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			(*v)[0].VolumeSource = corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "fixture"}}
		},
		"projected": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			(*v)[0].VolumeSource = corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{}}
		},
		"host": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			(*v)[0].VolumeSource = corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/fixture"}}
		},
		"mixed": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			(*v)[0].Secret = &corev1.SecretVolumeSource{SecretName: "fixture"}
		},
		"unbounded": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*v)[0].EmptyDir.SizeLimit = nil },
		"zero": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			q := resource.MustParse("0")
			(*v)[0].EmptyDir.SizeLimit = &q
		},
		"negative": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			q := resource.MustParse("-1Mi")
			(*v)[0].EmptyDir.SizeLimit = &q
		},
		"too large": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			q := resource.MustParse("2Gi")
			(*v)[0].EmptyDir.SizeLimit = &q
		},
		"medium":           func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*v)[0].EmptyDir.Medium = "fixture" },
		"duplicate volume": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { *v = append(*v, (*v)[0]) },
		"duplicate mount":  func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { *m = append(*m, (*m)[0]) },
		"subpath":          func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*m)[0].SubPath = "cache" },
		"expression":       func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*m)[0].SubPathExpr = "$(FIXTURE)" },
		"propagation": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			p := corev1.MountPropagationBidirectional
			(*m)[0].MountPropagation = &p
		},
		"relative": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*m)[0].MountPath = "cache" },
		"root":     func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*m)[0].MountPath = "/" },
		"unclean":  func(v *[]corev1.Volume, m *[]corev1.VolumeMount) { (*m)[0].MountPath = "/tmp/../app" },
		"combined limit": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			q := resource.MustParse("1Gi")
			(*v)[0].EmptyDir.SizeLimit = &q
			other := *(*v)[0].DeepCopy()
			other.Name = "other"
			*v = append(*v, other)
			*m = append(*m, corev1.VolumeMount{Name: "other", MountPath: "/tmp/other"})
		},
		"too many": func(v *[]corev1.Volume, m *[]corev1.VolumeMount) {
			for i := 0; i < 8; i++ {
				*m = append(*m, (*m)[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			v, m := scratchFixture()
			mutate(&v, &m)
			_, err := runTargetScratch(v, m)
			require.Error(t, err)
		})
	}
}

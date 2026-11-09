package k8s

import batchv1 "k8s.io/api/batch/v1"

// Called only on copied specs. Quantity.Cmp compares exact values without
// byte rounding; every other field retains the final strict comparison.
func normalizeInitialEmptyDirQuantities(got, want *batchv1.JobSpec) bool {
	if len(got.Template.Spec.Volumes) != len(want.Template.Spec.Volumes) {
		return false
	}
	for i := range want.Template.Spec.Volumes {
		actual, desired := got.Template.Spec.Volumes[i].EmptyDir, want.Template.Spec.Volumes[i].EmptyDir
		if actual == nil || desired == nil || actual.SizeLimit == nil || desired.SizeLimit == nil {
			continue // The strict comparison preserves nil versus present.
		}
		if actual.SizeLimit.Cmp(*desired.SizeLimit) != 0 {
			return false
		}
		quantity := desired.SizeLimit.DeepCopy()
		actual.SizeLimit = &quantity
	}
	return true
}

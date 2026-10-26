package workloadtargets

// ProductionFenceDiagnostic records only the first failed dry-plan gate. It is
// never part of a Plan, fingerprint, binding or apply request. Nil sinks leave
// the original error values and all ordinary/apply behavior unchanged.
type ProductionFenceDiagnostic struct {
	stage  string
	reason string
}

func (d *ProductionFenceDiagnostic) SnapshotFailure(reason string, err error) error {
	return d.failure("snapshot", reason, err)
}

func (d *ProductionFenceDiagnostic) ValidationFailure(reason string, err error) error {
	return d.failure("validation", reason, err)
}

func (d *ProductionFenceDiagnostic) failure(stage, reason string, err error) error {
	if d != nil && err != nil && d.stage == "" && productionDiagnosticAllowed(stage, reason) {
		d.stage, d.reason = stage, reason
	}
	return err
}

// Fields returns no caller/upstream text, resource identity or observed value.
func (d *ProductionFenceDiagnostic) Fields() (stage, reason string, ok bool) {
	if d == nil || !productionDiagnosticAllowed(d.stage, d.reason) {
		return "", "", false
	}
	return d.stage, d.reason, true
}

func productionDiagnosticAllowed(stage, reason string) bool {
	switch stage {
	case "snapshot":
		switch reason {
		case "prerequisites", "project_intent", "environment_intent", "registration_intent", "fleet_registry", "namespace_claim", "namespace_read", "application_read", "root_read", "parent_read", "root_revision", "source_configuration", "tracking_configuration", "application_revision", "manifest_fetch", "deployment_read", "declared_target", "incumbent_read":
			return true
		}
	case "validation":
		switch reason {
		case "fleet_identity", "repository", "parent_identity", "parent_controller", "root_authority", "parent_generator", "parent_template", "source_configuration", "configuration_revision", "application_project", "application_sync", "namespace_owner", "persisted_intent", "native_identity", "application_source", "compared_source", "application_revision", "tracking_configuration", "tracking_marker", "resource_inventory", "declared_labels", "image_shape", "container_set", "declared_identity", "incumbent_binding", "fleet_coherence", "plan_identity", "plan_fingerprint", "selected_target":
			return true
		}
	}
	return false
}

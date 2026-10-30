package simulationreset

// Target contains metadata projected by a future trusted reader. Its values
// alone never establish authenticity, a write barrier or lifecycle authority.
type Target struct {
	Namespace, NamespaceUID, DeploymentName, DeploymentUID, ServiceUID string
	Image, SourceSHA, OriginSHA                                        string
}

func (t Target) matches(e Enrollment) bool {
	return t.Namespace == e.Namespace && t.NamespaceUID == e.NamespaceUID &&
		t.DeploymentName == e.DeploymentName && t.DeploymentUID == e.DeploymentUID &&
		t.ServiceUID == e.ServiceUID && t.Image == e.Image &&
		t.SourceSHA == e.SourceSHA && t.OriginSHA == e.OriginSHA
}

// GuardOrdinaryMutation is a pure decision. A hold in any known phase, including
// uncertain or barrier-unverified, refuses ordinary lifecycle writes. No caller
// flag, elapsed time or positive replica value clears it.
func GuardOrdinaryMutation(e Enrollment, h *Hold, t Target) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if !t.matches(e) {
		return ErrIdentityDrift
	}
	if h == nil {
		return nil
	}
	if err := h.Validate(e); err != nil {
		return err
	}
	return ErrHeld
}

// GuardResetProjection checks metadata for a candidate only. BarrierProof has
// no production constructor, so this foundation cannot authorize a writer.
func GuardResetProjection(e Enrollment, h Hold, t Target, p *BarrierProof) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := h.Validate(e); err != nil {
		return err
	}
	if !t.matches(e) {
		return ErrIdentityDrift
	}
	return RequireBarrier(e, h, p)
}

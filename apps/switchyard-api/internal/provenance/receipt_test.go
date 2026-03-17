package provenance

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperPR returns a minimal PullRequest for receipt generation tests.
func helperPR() *PullRequest {
	pr := &PullRequest{
		Number:      42,
		Title:       "feat: add widget endpoint",
		State:       "closed",
		MergedAt:    time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC),
		HTMLURL:     "https://github.com/madfam-org/enclii/pull/42",
		MergeCommit: "abc123merge",
	}
	pr.Head.SHA = "deadbeef"
	pr.Base.Ref = "main"
	pr.Base.Repo.Name = "enclii"
	pr.Base.Repo.Owner.Login = "madfam-org"
	return pr
}

// helperReviews returns a set of reviews with the given states.
func helperReviews(specs ...struct {
	login string
	email string
	state string
}) []Review {
	reviews := make([]Review, len(specs))
	for i, s := range specs {
		reviews[i] = Review{
			ID:          int64(i + 1),
			User:        User{Login: s.login, Email: s.email},
			State:       s.state,
			SubmittedAt: time.Date(2026, 3, 10, 12, i, 0, 0, time.UTC),
		}
	}
	return reviews
}

// helperStatus returns a CheckStatus with the given state and check contexts.
func helperStatus(state string, checks map[string]string) *CheckStatus {
	cs := &CheckStatus{
		State:      state,
		TotalCount: len(checks),
	}
	for ctx, st := range checks {
		cs.Statuses = append(cs.Statuses, struct {
			State   string `json:"state"`
			Context string `json:"context"`
		}{State: st, Context: ctx})
	}
	return cs
}

func TestGenerateReceipt(t *testing.T) {
	tests := []struct {
		name            string
		reviews         []Review
		status          *CheckStatus
		changeTicket    string
		policyCompliant bool
		wantApprovals   int
		wantCIState     string
		wantVersion     string
	}{
		{
			name: "two approvals with passing CI",
			reviews: helperReviews(
				struct{ login, email, state string }{"alice", "alice@example.com", "APPROVED"},
				struct{ login, email, state string }{"bob", "bob@example.com", "APPROVED"},
			),
			status:          helperStatus("success", map[string]string{"ci/lint": "success", "ci/test": "success"}),
			changeTicket:    "https://jira.example.com/TICKET-1",
			policyCompliant: true,
			wantApprovals:   2,
			wantCIState:     "success",
			wantVersion:     "1.0",
		},
		{
			name: "mixed review states only counts approved",
			reviews: helperReviews(
				struct{ login, email, state string }{"alice", "alice@example.com", "APPROVED"},
				struct{ login, email, state string }{"bob", "bob@example.com", "CHANGES_REQUESTED"},
				struct{ login, email, state string }{"carol", "carol@example.com", "COMMENTED"},
			),
			status:          helperStatus("success", map[string]string{"ci/lint": "success"}),
			changeTicket:    "",
			policyCompliant: false,
			wantApprovals:   1,
			wantCIState:     "success",
			wantVersion:     "1.0",
		},
		{
			name:            "no reviews yields empty approvals slice",
			reviews:         []Review{},
			status:          helperStatus("pending", map[string]string{}),
			changeTicket:    "",
			policyCompliant: false,
			wantApprovals:   0,
			wantCIState:     "pending",
			wantVersion:     "1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := helperPR()

			receipt, err := GenerateReceipt(
				"deploy-001", "switchyard-api", "production",
				"v1.2.3", "deadbeef", "ghcr.io/madfam-org/api:v1.2.3",
				pr, tt.reviews, tt.status, tt.changeTicket,
				tt.policyCompliant, map[string]interface{}{"min_approvals": 2},
			)

			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, receipt.Version)
			assert.Equal(t, "deploy-001", receipt.DeploymentID)
			assert.Equal(t, "switchyard-api", receipt.ServiceName)
			assert.Equal(t, "production", receipt.Environment)
			assert.Equal(t, "v1.2.3", receipt.ReleaseVersion)
			assert.Equal(t, "deadbeef", receipt.GitCommitSHA)
			assert.Equal(t, "ghcr.io/madfam-org/api:v1.2.3", receipt.ImageURI)
			assert.Equal(t, tt.policyCompliant, receipt.PolicyCompliant)
			assert.Equal(t, tt.changeTicket, receipt.ChangeTicket)

			// Approval count
			assert.Len(t, receipt.Approvals, tt.wantApprovals)
			for _, a := range receipt.Approvals {
				assert.Equal(t, "APPROVED", a.State)
			}

			// CI evidence
			assert.Equal(t, tt.wantCIState, receipt.CIStatus.State)

			// PR evidence
			assert.Equal(t, 42, receipt.PullRequest.Number)
			assert.Equal(t, "madfam-org/enclii", receipt.PullRequest.Repository)
			assert.Equal(t, "main", receipt.PullRequest.BaseBranch)

			// Signature must be non-empty
			assert.NotEmpty(t, receipt.Signature)
		})
	}
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name      string
		tamperFn  func(r *ComplianceReceipt) // mutates the receipt after generation
		wantValid bool
	}{
		{
			name:      "unmodified receipt verifies successfully",
			tamperFn:  nil,
			wantValid: true,
		},
		{
			name: "tampered deployment ID fails verification",
			tamperFn: func(r *ComplianceReceipt) {
				r.DeploymentID = "tampered-id"
			},
			wantValid: false,
		},
		{
			name: "tampered policy compliance flag fails verification",
			tamperFn: func(r *ComplianceReceipt) {
				r.PolicyCompliant = false
			},
			wantValid: false,
		},
		{
			name: "tampered approvals list fails verification",
			tamperFn: func(r *ComplianceReceipt) {
				r.Approvals = append(r.Approvals, ApprovalEvidence{
					Approver: "mallory",
					State:    "APPROVED",
				})
			},
			wantValid: false,
		},
		{
			name: "empty signature fails verification",
			tamperFn: func(r *ComplianceReceipt) {
				r.Signature = ""
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := helperPR()
			reviews := helperReviews(
				struct{ login, email, state string }{"alice", "alice@example.com", "APPROVED"},
			)
			status := helperStatus("success", map[string]string{"ci/test": "success"})

			receipt, err := GenerateReceipt(
				"deploy-verify", "api", "staging",
				"v2.0.0", "aabbcc", "ghcr.io/org/api:v2.0.0",
				pr, reviews, status, "", true,
				map[string]interface{}{"min_approvals": 1},
			)
			require.NoError(t, err)

			if tt.tamperFn != nil {
				tt.tamperFn(receipt)
			}

			valid, err := receipt.Verify()
			require.NoError(t, err)
			assert.Equal(t, tt.wantValid, valid)
		})
	}
}

func TestToJSON_FromJSON_Roundtrip(t *testing.T) {
	pr := helperPR()
	reviews := helperReviews(
		struct{ login, email, state string }{"alice", "alice@example.com", "APPROVED"},
		struct{ login, email, state string }{"bob", "bob@example.com", "APPROVED"},
	)
	status := helperStatus("success", map[string]string{"ci/lint": "success"})

	original, err := GenerateReceipt(
		"deploy-rt", "svc", "production",
		"v3.0.0", "112233", "ghcr.io/org/svc:v3.0.0",
		pr, reviews, status, "TICKET-99", true,
		map[string]interface{}{"min_approvals": 2},
	)
	require.NoError(t, err)

	// Serialize
	jsonStr, err := original.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonStr)

	// Verify it is valid JSON
	assert.True(t, json.Valid([]byte(jsonStr)))

	// Deserialize
	restored, err := FromJSON(jsonStr)
	require.NoError(t, err)

	// Structural equality
	assert.Equal(t, original.Version, restored.Version)
	assert.Equal(t, original.DeploymentID, restored.DeploymentID)
	assert.Equal(t, original.ServiceName, restored.ServiceName)
	assert.Equal(t, original.Signature, restored.Signature)
	assert.Equal(t, original.PolicyCompliant, restored.PolicyCompliant)
	assert.Len(t, restored.Approvals, 2)

	// Restored receipt should also verify
	valid, err := restored.Verify()
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestFromJSON_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "not json", input: "this is not json"},
		{name: "json array", input: "[]"},
		{name: "malformed json", input: `{"version": "1.0"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receipt, err := FromJSON(tt.input)
			assert.Error(t, err)
			assert.Nil(t, receipt)
		})
	}
}

func TestGetApproverEmails(t *testing.T) {
	tests := []struct {
		name       string
		approvals  []ApprovalEvidence
		wantEmails []string
	}{
		{
			name:       "no approvals returns empty slice",
			approvals:  nil,
			wantEmails: []string{},
		},
		{
			name: "filters out empty emails",
			approvals: []ApprovalEvidence{
				{Approver: "alice", ApproverEmail: "alice@example.com", State: "APPROVED"},
				{Approver: "bot", ApproverEmail: "", State: "APPROVED"},
				{Approver: "carol", ApproverEmail: "carol@example.com", State: "APPROVED"},
			},
			wantEmails: []string{"alice@example.com", "carol@example.com"},
		},
		{
			name: "all have emails",
			approvals: []ApprovalEvidence{
				{Approver: "alice", ApproverEmail: "alice@example.com", State: "APPROVED"},
			},
			wantEmails: []string{"alice@example.com"},
		},
		{
			name: "all empty emails returns empty slice",
			approvals: []ApprovalEvidence{
				{Approver: "bot1", ApproverEmail: "", State: "APPROVED"},
				{Approver: "bot2", ApproverEmail: "", State: "APPROVED"},
			},
			wantEmails: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receipt := &ComplianceReceipt{Approvals: tt.approvals}
			emails := receipt.GetApproverEmails()
			assert.Equal(t, tt.wantEmails, emails)
		})
	}
}

func TestGetApprovalSummary(t *testing.T) {
	tests := []struct {
		name      string
		approvals []ApprovalEvidence
		want      string
	}{
		{
			name:      "no approvals",
			approvals: nil,
			want:      "No approvals",
		},
		{
			name:      "empty approvals slice",
			approvals: []ApprovalEvidence{},
			want:      "No approvals",
		},
		{
			name: "single approver",
			approvals: []ApprovalEvidence{
				{Approver: "alice", State: "APPROVED"},
			},
			want: "Approved by alice",
		},
		{
			name: "two approvers",
			approvals: []ApprovalEvidence{
				{Approver: "alice", State: "APPROVED"},
				{Approver: "bob", State: "APPROVED"},
			},
			want: "Approved by 2 reviewers: [alice bob]",
		},
		{
			name: "three approvers",
			approvals: []ApprovalEvidence{
				{Approver: "alice", State: "APPROVED"},
				{Approver: "bob", State: "APPROVED"},
				{Approver: "carol", State: "APPROVED"},
			},
			want: "Approved by 3 reviewers: [alice bob carol]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receipt := &ComplianceReceipt{Approvals: tt.approvals}
			assert.Equal(t, tt.want, receipt.GetApprovalSummary())
		})
	}
}

func TestGenerateReceipt_CIChecksMap(t *testing.T) {
	// Verify that status checks are correctly mapped into the receipt.
	pr := helperPR()
	checks := map[string]string{
		"ci/lint": "success",
		"ci/test": "failure",
		"ci/sec":  "pending",
	}
	status := helperStatus("failure", checks)

	receipt, err := GenerateReceipt(
		"deploy-ci", "api", "staging",
		"v1.0.0", "aabb", "ghcr.io/org/api:v1.0.0",
		pr, nil, status, "", false,
		map[string]interface{}{},
	)
	require.NoError(t, err)

	assert.Equal(t, "failure", receipt.CIStatus.State)
	assert.Equal(t, 3, receipt.CIStatus.TotalChecks)
	assert.Equal(t, "success", receipt.CIStatus.Checks["ci/lint"])
	assert.Equal(t, "failure", receipt.CIStatus.Checks["ci/test"])
	assert.Equal(t, "pending", receipt.CIStatus.Checks["ci/sec"])
}

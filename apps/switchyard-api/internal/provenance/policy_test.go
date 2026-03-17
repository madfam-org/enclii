package provenance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDefaultPolicy(t *testing.T) {
	policy := GetDefaultPolicy()

	require.NotNil(t, policy.Production)
	require.NotNil(t, policy.Staging)
	require.NotNil(t, policy.Development)

	// Production
	assert.Equal(t, 2, policy.Production.MinApprovals)
	assert.True(t, policy.Production.RequireCIPassing)
	assert.True(t, policy.Production.RequireMerged)
	assert.True(t, policy.Production.RequireChangeTicket)
	assert.False(t, policy.Production.AllowSelfApproval)
	assert.Contains(t, policy.Production.BlockedApprovers, "dependabot")
	assert.Contains(t, policy.Production.BlockedApprovers, "renovate")
	assert.Contains(t, policy.Production.BlockedApprovers, "github-actions")

	// Staging
	assert.Equal(t, 1, policy.Staging.MinApprovals)
	assert.True(t, policy.Staging.RequireCIPassing)
	assert.True(t, policy.Staging.RequireMerged)
	assert.False(t, policy.Staging.RequireChangeTicket)
	assert.False(t, policy.Staging.AllowSelfApproval)

	// Development
	assert.Equal(t, 0, policy.Development.MinApprovals)
	assert.False(t, policy.Development.RequireCIPassing)
	assert.False(t, policy.Development.RequireMerged)
	assert.False(t, policy.Development.RequireChangeTicket)
	assert.True(t, policy.Development.AllowSelfApproval)
	assert.Empty(t, policy.Development.BlockedApprovers)
}

func TestGetPolicyForEnvironment(t *testing.T) {
	policy := GetDefaultPolicy()

	tests := []struct {
		name          string
		envName       string
		wantMinAppr   int
		wantTicket    bool
		wantCIPassing bool
	}{
		{name: "production exact", envName: "production", wantMinAppr: 2, wantTicket: true, wantCIPassing: true},
		{name: "production uppercase", envName: "PRODUCTION", wantMinAppr: 2, wantTicket: true, wantCIPassing: true},
		{name: "production mixed case", envName: "Production", wantMinAppr: 2, wantTicket: true, wantCIPassing: true},
		{name: "prod prefix", envName: "prod", wantMinAppr: 2, wantTicket: true, wantCIPassing: true},
		{name: "prod-us-east", envName: "prod-us-east", wantMinAppr: 2, wantTicket: true, wantCIPassing: true},
		{name: "staging exact", envName: "staging", wantMinAppr: 1, wantTicket: false, wantCIPassing: true},
		{name: "staging uppercase", envName: "STAGING", wantMinAppr: 1, wantTicket: false, wantCIPassing: true},
		{name: "stage prefix", envName: "stage", wantMinAppr: 1, wantTicket: false, wantCIPassing: true},
		{name: "stage-eu", envName: "stage-eu", wantMinAppr: 1, wantTicket: false, wantCIPassing: true},
		{name: "development exact", envName: "development", wantMinAppr: 0, wantTicket: false, wantCIPassing: false},
		{name: "dev", envName: "dev", wantMinAppr: 0, wantTicket: false, wantCIPassing: false},
		{name: "preview falls to dev", envName: "preview", wantMinAppr: 0, wantTicket: false, wantCIPassing: false},
		{name: "feature-branch falls to dev", envName: "feature-branch", wantMinAppr: 0, wantTicket: false, wantCIPassing: false},
		{name: "empty string falls to dev", envName: "", wantMinAppr: 0, wantTicket: false, wantCIPassing: false},
		{name: "unknown falls to dev", envName: "sandbox", wantMinAppr: 0, wantTicket: false, wantCIPassing: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := policy.GetPolicyForEnvironment(tt.envName)
			require.NotNil(t, p)
			assert.Equal(t, tt.wantMinAppr, p.MinApprovals)
			assert.Equal(t, tt.wantTicket, p.RequireChangeTicket)
			assert.Equal(t, tt.wantCIPassing, p.RequireCIPassing)
		})
	}
}

func TestValidateApprovalCount(t *testing.T) {
	now := time.Now()

	makeReview := func(login, state string) Review {
		return Review{
			User:        User{Login: login},
			State:       state,
			SubmittedAt: now,
		}
	}

	tests := []struct {
		name           string
		policy         ApprovalPolicy
		reviews        []Review
		prAuthor       string
		wantViolations int
	}{
		{
			name: "enough approvals passes",
			policy: ApprovalPolicy{
				MinApprovals: 2,
			},
			reviews: []Review{
				makeReview("alice", "APPROVED"),
				makeReview("bob", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
		{
			name: "not enough approvals fails",
			policy: ApprovalPolicy{
				MinApprovals: 2,
			},
			reviews: []Review{
				makeReview("alice", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 1,
		},
		{
			name: "zero min approvals always passes",
			policy: ApprovalPolicy{
				MinApprovals: 0,
			},
			reviews:        []Review{},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
		{
			name: "non-approved reviews not counted",
			policy: ApprovalPolicy{
				MinApprovals: 1,
			},
			reviews: []Review{
				makeReview("alice", "CHANGES_REQUESTED"),
				makeReview("bob", "COMMENTED"),
			},
			prAuthor:       "charlie",
			wantViolations: 1,
		},
		{
			name: "self approval rejected when not allowed",
			policy: ApprovalPolicy{
				MinApprovals:      1,
				AllowSelfApproval: false,
			},
			reviews: []Review{
				makeReview("charlie", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 1,
		},
		{
			name: "self approval accepted when allowed",
			policy: ApprovalPolicy{
				MinApprovals:      1,
				AllowSelfApproval: true,
			},
			reviews: []Review{
				makeReview("charlie", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
		{
			name: "blocked approver not counted",
			policy: ApprovalPolicy{
				MinApprovals:     1,
				BlockedApprovers: []string{"dependabot"},
			},
			reviews: []Review{
				makeReview("dependabot", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 1,
		},
		{
			name: "blocked approver combined with valid approver",
			policy: ApprovalPolicy{
				MinApprovals:     1,
				BlockedApprovers: []string{"dependabot"},
			},
			reviews: []Review{
				makeReview("dependabot", "APPROVED"),
				makeReview("alice", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
		{
			name: "allowed approvers whitelist enforced",
			policy: ApprovalPolicy{
				MinApprovals:     1,
				AllowedApprovers: []string{"alice", "bob"},
			},
			reviews: []Review{
				makeReview("carol", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 1,
		},
		{
			name: "allowed approvers whitelist passes with valid user",
			policy: ApprovalPolicy{
				MinApprovals:     1,
				AllowedApprovers: []string{"alice", "bob"},
			},
			reviews: []Review{
				makeReview("alice", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
		{
			name: "empty allowed list permits any user",
			policy: ApprovalPolicy{
				MinApprovals:     1,
				AllowedApprovers: []string{},
			},
			reviews: []Review{
				makeReview("anyone", "APPROVED"),
			},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
		{
			name: "combination: self + blocked + whitelist",
			policy: ApprovalPolicy{
				MinApprovals:      2,
				AllowSelfApproval: false,
				BlockedApprovers:  []string{"bot"},
				AllowedApprovers:  []string{"alice", "bob"},
			},
			reviews: []Review{
				makeReview("charlie", "APPROVED"), // self, skipped
				makeReview("bot", "APPROVED"),     // blocked, skipped
				makeReview("alice", "APPROVED"),   // valid
				makeReview("carol", "APPROVED"),   // not in whitelist, skipped
				makeReview("bob", "APPROVED"),     // valid
			},
			prAuthor:       "charlie",
			wantViolations: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := tt.policy.ValidateApprovalCount(tt.reviews, tt.prAuthor)
			assert.Len(t, violations, tt.wantViolations)
			if tt.wantViolations > 0 {
				assert.Equal(t, "min_approvals", violations[0].Rule)
			}
		})
	}
}

func TestValidateCIStatus(t *testing.T) {
	tests := []struct {
		name             string
		requireCIPassing bool
		statusState      string
		wantViolations   int
	}{
		{name: "CI not required passes", requireCIPassing: false, statusState: "failure", wantViolations: 0},
		{name: "CI required and passing", requireCIPassing: true, statusState: "success", wantViolations: 0},
		{name: "CI required but failing", requireCIPassing: true, statusState: "failure", wantViolations: 1},
		{name: "CI required but pending", requireCIPassing: true, statusState: "pending", wantViolations: 1},
		{name: "CI required but error", requireCIPassing: true, statusState: "error", wantViolations: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &ApprovalPolicy{RequireCIPassing: tt.requireCIPassing}
			status := &CheckStatus{State: tt.statusState}
			violations := policy.ValidateCIStatus(status)
			assert.Len(t, violations, tt.wantViolations)
			if tt.wantViolations > 0 {
				assert.Equal(t, "ci_passing", violations[0].Rule)
				assert.Contains(t, violations[0].Message, tt.statusState)
			}
		})
	}
}

func TestValidatePRMerged(t *testing.T) {
	tests := []struct {
		name           string
		requireMerged  bool
		prState        string
		mergedAt       time.Time
		wantViolations int
	}{
		{name: "merge not required passes", requireMerged: false, prState: "open", mergedAt: time.Time{}, wantViolations: 0},
		{name: "merged PR passes", requireMerged: true, prState: "closed", mergedAt: time.Now(), wantViolations: 0},
		{name: "open PR fails", requireMerged: true, prState: "open", mergedAt: time.Time{}, wantViolations: 1},
		{name: "closed but not merged fails", requireMerged: true, prState: "closed", mergedAt: time.Time{}, wantViolations: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &ApprovalPolicy{RequireMerged: tt.requireMerged}
			pr := &PullRequest{State: tt.prState, MergedAt: tt.mergedAt}
			violations := policy.ValidatePRMerged(pr)
			assert.Len(t, violations, tt.wantViolations)
			if tt.wantViolations > 0 {
				assert.Equal(t, "pr_merged", violations[0].Rule)
			}
		})
	}
}

func TestValidateChangeTicket(t *testing.T) {
	tests := []struct {
		name            string
		requireTicket   bool
		changeTicketURL string
		wantViolations  int
	}{
		{name: "ticket not required empty URL passes", requireTicket: false, changeTicketURL: "", wantViolations: 0},
		{name: "ticket not required with URL passes", requireTicket: false, changeTicketURL: "https://jira.example.com/T-1", wantViolations: 0},
		{name: "ticket required with URL passes", requireTicket: true, changeTicketURL: "https://jira.example.com/T-1", wantViolations: 0},
		{name: "ticket required without URL fails", requireTicket: true, changeTicketURL: "", wantViolations: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &ApprovalPolicy{RequireChangeTicket: tt.requireTicket}
			violations := policy.ValidateChangeTicket(tt.changeTicketURL)
			assert.Len(t, violations, tt.wantViolations)
			if tt.wantViolations > 0 {
				assert.Equal(t, "change_ticket", violations[0].Rule)
			}
		})
	}
}

func TestValidate_FullPolicyCheck(t *testing.T) {
	now := time.Now()

	makeReview := func(login, state string) Review {
		return Review{
			User:        User{Login: login},
			State:       state,
			SubmittedAt: now,
		}
	}

	tests := []struct {
		name             string
		policy           ApprovalPolicy
		prState          string
		mergedAt         time.Time
		ciState          string
		reviews          []Review
		changeTicket     string
		wantViolationCnt int
		wantRules        []string
	}{
		{
			name: "all checks pass for production",
			policy: ApprovalPolicy{
				MinApprovals:        2,
				RequireCIPassing:    true,
				RequireMerged:       true,
				RequireChangeTicket: true,
			},
			prState:          "closed",
			mergedAt:         now,
			ciState:          "success",
			reviews:          []Review{makeReview("alice", "APPROVED"), makeReview("bob", "APPROVED")},
			changeTicket:     "TICKET-1",
			wantViolationCnt: 0,
		},
		{
			name: "all checks fail for production",
			policy: ApprovalPolicy{
				MinApprovals:        2,
				RequireCIPassing:    true,
				RequireMerged:       true,
				RequireChangeTicket: true,
			},
			prState:          "open",
			mergedAt:         time.Time{},
			ciState:          "failure",
			reviews:          []Review{},
			changeTicket:     "",
			wantViolationCnt: 4,
			wantRules:        []string{"min_approvals", "ci_passing", "pr_merged", "change_ticket"},
		},
		{
			name: "development policy passes with nothing",
			policy: ApprovalPolicy{
				MinApprovals:        0,
				RequireCIPassing:    false,
				RequireMerged:       false,
				RequireChangeTicket: false,
				AllowSelfApproval:   true,
			},
			prState:          "open",
			mergedAt:         time.Time{},
			ciState:          "pending",
			reviews:          []Review{},
			changeTicket:     "",
			wantViolationCnt: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &PullRequest{State: tt.prState, MergedAt: tt.mergedAt}
			pr.Head.SHA = "testauthor"
			status := &CheckStatus{State: tt.ciState}

			violations := tt.policy.Validate(pr, tt.reviews, status, tt.changeTicket)
			assert.Len(t, violations, tt.wantViolationCnt)

			if tt.wantRules != nil {
				rules := make([]string, len(violations))
				for i, v := range violations {
					rules[i] = v.Rule
				}
				assert.ElementsMatch(t, tt.wantRules, rules)
			}
		})
	}
}

func TestPolicyViolation_Error(t *testing.T) {
	v := PolicyViolation{Rule: "ci_passing", Message: "CI checks failed"}
	assert.Equal(t, "[ci_passing] CI checks failed", v.Error())
}

func TestPolicyViolations_Error(t *testing.T) {
	tests := []struct {
		name       string
		violations PolicyViolations
		wantPrefix string
	}{
		{
			name:       "empty violations",
			violations: PolicyViolations{},
			wantPrefix: "no violations",
		},
		{
			name: "single violation",
			violations: PolicyViolations{
				{Rule: "ci_passing", Message: "CI failed"},
			},
			wantPrefix: "policy violations:",
		},
		{
			name: "multiple violations",
			violations: PolicyViolations{
				{Rule: "ci_passing", Message: "CI failed"},
				{Rule: "pr_merged", Message: "PR not merged"},
			},
			wantPrefix: "policy violations:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.violations.Error()
			assert.Contains(t, result, tt.wantPrefix)
		})
	}
}

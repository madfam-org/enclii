package provisioning

import (
	"reflect"
	"testing"
)

func TestDiffUserlist(t *testing.T) {
	tests := []struct {
		name          string
		pgLoginRoles  []string
		userlistUsers []string
		wantMissing   []string
		wantStale     []string
	}{
		{
			name:          "in sync",
			pgLoginRoles:  []string{"fortuna", "janua", "dhanam"},
			userlistUsers: []string{"fortuna", "janua", "dhanam"},
			wantMissing:   nil,
			wantStale:     nil,
		},
		{
			// The 2026-08-23 outage: a hand-applied userlist dropped these four
			// login roles. This is the case the detector exists to catch.
			name:          "the outage — four login roles dropped from userlist",
			pgLoginRoles:  []string{"fortuna", "bloom", "ceq", "autoswarm", "janua", "dhanam"},
			userlistUsers: []string{"janua", "dhanam", "placeholder"},
			wantMissing:   []string{"autoswarm", "bloom", "ceq", "fortuna"},
			wantStale:     []string{"placeholder"},
		},
		{
			name:          "ignored roles never flagged as missing",
			pgLoginRoles:  []string{"postgres", "pgbouncer_admin", "janua"},
			userlistUsers: []string{"janua"},
			wantMissing:   nil,
			wantStale:     nil,
		},
		{
			name:          "ignored roles never flagged as stale",
			pgLoginRoles:  []string{"janua"},
			userlistUsers: []string{"janua", "postgres", "pgbouncer_admin"},
			wantMissing:   nil,
			wantStale:     nil,
		},
		{
			name:          "missing only",
			pgLoginRoles:  []string{"tezca", "karafiel"},
			userlistUsers: []string{"tezca"},
			wantMissing:   []string{"karafiel"},
			wantStale:     nil,
		},
		{
			name:          "stale only",
			pgLoginRoles:  []string{"tezca"},
			userlistUsers: []string{"tezca", "retired_svc"},
			wantMissing:   nil,
			wantStale:     []string{"retired_svc"},
		},
		{
			name:          "empty userlist — every login role missing",
			pgLoginRoles:  []string{"fortuna", "janua"},
			userlistUsers: nil,
			wantMissing:   []string{"fortuna", "janua"},
			wantStale:     nil,
		},
		{
			name:          "output is sorted regardless of input order",
			pgLoginRoles:  []string{"zulu", "alpha", "mike"},
			userlistUsers: nil,
			wantMissing:   []string{"alpha", "mike", "zulu"},
			wantStale:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffUserlist(tt.pgLoginRoles, tt.userlistUsers)
			if !reflect.DeepEqual(got.MissingFromUserlist, tt.wantMissing) {
				t.Errorf("MissingFromUserlist = %v, want %v", got.MissingFromUserlist, tt.wantMissing)
			}
			if !reflect.DeepEqual(got.StaleInUserlist, tt.wantStale) {
				t.Errorf("StaleInUserlist = %v, want %v", got.StaleInUserlist, tt.wantStale)
			}
		})
	}
}

func TestUserlistDriftPredicates(t *testing.T) {
	if (UserlistDrift{}).HasMissing() {
		t.Error("empty drift should not report HasMissing")
	}
	if !(UserlistDrift{MissingFromUserlist: []string{"fortuna"}}).HasMissing() {
		t.Error("drift with a missing role should report HasMissing")
	}
	if !(UserlistDrift{}).Empty() {
		t.Error("zero-value drift should be Empty")
	}
	if (UserlistDrift{StaleInUserlist: []string{"x"}}).Empty() {
		t.Error("drift with a stale entry is not Empty")
	}
}

func TestParseUserlistUsernames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "standard two-field lines",
			input: "\"fortuna\" \"secret1\"\n\"janua\" \"secret2\"\n",
			want:  []string{"fortuna", "janua"},
		},
		{
			name:  "blank lines skipped",
			input: "\"fortuna\" \"secret1\"\n\n\"janua\" \"secret2\"\n",
			want:  []string{"fortuna", "janua"},
		},
		{
			name:  "malformed lines skipped, valid ones kept",
			input: "\"fortuna\" \"secret1\"\nnot-a-userlist-line\n\"janua\" \"secret2\"",
			want:  []string{"fortuna", "janua"},
		},
		{
			name:  "unterminated quote skipped",
			input: "\"fortuna\n\"janua\" \"secret2\"",
			want:  []string{"janua"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUserlistUsernames(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseUserlistUsernames() = %v, want %v", got, tt.want)
			}
		})
	}
}

package argocdbackup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func fixtureID(n int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", n, n) }
func fixtureCapture(topology string) Capture {
	scope, err := fixedScope(topology)
	if err != nil {
		panic(err)
	}
	c := Capture{Topology: topology, NamespaceUID: fixtureID(1), Groups: map[string]ReadList{}}
	for n, k := range scope {
		uid := fixtureID(n + 10)
		if k.Kind == "Namespace" {
			uid = c.NamespaceUID
		}
		c.Fixed = append(c.Fixed, Object{k, uid, "7"})
	}
	n := 1000
	for kind, spec := range protectedGroups {
		l := ReadList{Listed: true}
		for _, o := range c.Fixed {
			if o.Kind == kind {
				l.Items = append(l.Items, o)
			}
		}
		if len(l.Items) == 0 {
			l.Items = append(l.Items, Object{Key{spec.API, kind, "argocd", "fixture-item"}, fixtureID(n), "9"})
			n++
		}
		c.Groups[kind] = l
	}
	return c
}
func cloneCapture(c Capture) Capture {
	raw, _ := json.Marshal(c)
	var out Capture
	_ = json.Unmarshal(raw, &out)
	return out
}
func fixtureContext() Context {
	return Context{EnvelopeProfile, fixtureID(20), fixtureID(1), fixtureID(21), "NON_HA_LAYOUT_OBSERVED"}
}
func fixtureClaim() Claim {
	return Claim{fixtureID(20), fixtureID(30), fixtureID(1), fixtureID(21), fixtureID(40), "NON_HA_LAYOUT_OBSERVED"}
}
func mutationObject(name string) Object {
	return Object{Key{"apps/v1", "Deployment", "argocd", name}, fixtureID(99), "42"}
}
func mustHold(t *testing.T, err error) {
	t.Helper()
	if err != ErrHold {
		t.Fatal("expected closed hold")
	}
}

func TestFrozenScopesAndBothCompleteSelections(t *testing.T) {
	for topology, count := range map[string]int{"NON_HA_LAYOUT_OBSERVED": 60, "HA_LAYOUT_OBSERVED": 71} {
		t.Run(topology, func(t *testing.T) {
			c := fixtureCapture(topology)
			if len(c.Fixed) != count {
				t.Fatal("fixed scope count")
			}
			if _, err := Selection(c); err != nil {
				t.Fatal("complete selection refused")
			}
			if err := StableSelection(c, cloneCapture(c)); err != nil {
				t.Fatal("stable selection refused")
			}
		})
	}
}
func TestSelectionRefusesIncompleteAmbiguousAndForeignMetadata(t *testing.T) {
	cases := map[string]func(*Capture){
		"topology-unselected":    func(c *Capture) { c.Topology = "" },
		"unknown-topology":       func(c *Capture) { c.Topology = "UNKNOWN_MIXED_REDIS_LAYOUT" },
		"namespace-uid":          func(c *Capture) { c.NamespaceUID = fixtureID(999) },
		"missing-fixed-object":   func(c *Capture) { c.Fixed = c.Fixed[1:] },
		"duplicate-fixed-object": func(c *Capture) { c.Fixed[1] = c.Fixed[0] },
		"foreign-fixed-object":   func(c *Capture) { c.Fixed[0].Name = "foreign-target" },
		"missing-uid":            func(c *Capture) { c.Fixed[0].UID = "" },
		"zero-uid":               func(c *Capture) { c.Fixed[0].UID = "00000000-0000-0000-0000-000000000000" },
		"missing-rv":             func(c *Capture) { c.Fixed[0].ResourceVersion = "" },
		"missing-group":          func(c *Capture) { delete(c.Groups, "Secret") },
		"extra-group":            func(c *Capture) { c.Groups["Pod"] = ReadList{Listed: true} },
		"unlisted":               func(c *Capture) { l := c.Groups["Secret"]; l.Listed = false; c.Groups["Secret"] = l },
		"continuation":           func(c *Capture) { l := c.Groups["Secret"]; l.Continue = "opaque-fixture-token"; c.Groups["Secret"] = l },
		"remaining":              func(c *Capture) { l := c.Groups["Secret"]; l.Remaining = 1; c.Groups["Secret"] = l },
		"negative-remaining":     func(c *Capture) { l := c.Groups["Secret"]; l.Remaining = -1; c.Groups["Secret"] = l },
		"empty-group":            func(c *Capture) { l := c.Groups["Application"]; l.Items = nil; c.Groups["Application"] = l },
		"duplicate-group-object": func(c *Capture) {
			l := c.Groups["Secret"]
			l.Items = append(l.Items, l.Items[0])
			c.Groups["Secret"] = l
		},
		"foreign-group-namespace": func(c *Capture) {
			l := c.Groups["Application"]
			l.Items[0].Namespace = "production-fixture"
			c.Groups["Application"] = l
		},
		"wrong-group-gvk": func(c *Capture) {
			l := c.Groups["Application"]
			l.Items[0].APIVersion = "foreign.example/v1"
			c.Groups["Application"] = l
		},
		"group-fixed-identity-mismatch": func(c *Capture) {
			l := c.Groups["ConfigMap"]
			l.Items[0].UID = fixtureID(999)
			c.Groups["ConfigMap"] = l
		},
		"oversized-group": func(c *Capture) {
			l := c.Groups["Application"]
			for i := 0; i < 513; i++ {
				l.Items = append(l.Items, Object{Key{"argoproj.io/v1alpha1", "Application", "argocd", fmt.Sprintf("fixture-%d", i)}, fixtureID(2000 + i), "8"})
			}
			c.Groups["Application"] = l
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := fixtureCapture("NON_HA_LAYOUT_OBSERVED")
			mutate(&c)
			_, err := Selection(c)
			mustHold(t, err)
		})
	}
}
func TestStableSelectionRejectsIdentityOrListDrift(t *testing.T) {
	c := fixtureCapture("NON_HA_LAYOUT_OBSERVED")
	for _, kind := range []string{"uid", "rv", "new-credential", "topology"} {
		t.Run(kind, func(t *testing.T) {
			b := cloneCapture(c)
			switch kind {
			case "uid":
				b.Fixed[0].UID = fixtureID(987)
			case "rv":
				b.Fixed[0].ResourceVersion = "987"
			case "new-credential":
				l := b.Groups["Secret"]
				l.Items = append(l.Items, Object{Key{"v1", "Secret", "argocd", "credential-fixture-extra"}, fixtureID(988), "8"})
				b.Groups["Secret"] = l
			case "topology":
				b = fixtureCapture("HA_LAYOUT_OBSERVED")
			}
			mustHold(t, StableSelection(c, b))
		})
	}
}
func TestEnvelopeRoundtripFreshNonceAndAuthenticatedContext(t *testing.T) {
	key := bytes.Repeat([]byte{0x5a}, 32)
	plain := []byte("PRIVATE_TEST_CANARY_ONLY")
	c := fixtureContext()
	first, err := Seal(key, plain, c)
	if err != nil {
		t.Fatal("seal failed")
	}
	second, err := Seal(key, plain, c)
	if err != nil {
		t.Fatal("seal failed")
	}
	if bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("nonce reused")
	}
	got, err := Open(key, first.Bytes(), c)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("roundtrip failed")
	}
	verified, err := Authenticate(key, first.Bytes(), c)
	if err != nil || !bytes.Equal(verified.Bytes(), first.Bytes()) {
		t.Fatal("stored envelope auth failed")
	}
	exported := first.Bytes()
	exported[0] ^= 1
	if bytes.Equal(exported, first.Bytes()) {
		t.Fatal("mutable export aliased envelope")
	}
}
func TestEnvelopeRejectsReplayWrongKeyAndTampering(t *testing.T) {
	key := bytes.Repeat([]byte{0x5a}, 32)
	c := fixtureContext()
	sealed, _ := Seal(key, []byte("PRIVATE_TEST_CANARY_ONLY"), c)
	for name, mutate := range map[string]func(*Context){"operation": func(c *Context) { c.OperationID = fixtureID(22) }, "namespace": func(c *Context) { c.NamespaceUID = fixtureID(23) }, "backup": func(c *Context) { c.BackupID = fixtureID(24) }, "topology": func(c *Context) { c.Topology = "HA_LAYOUT_OBSERVED" }, "profile": func(c *Context) { c.Profile = "foreign" }} {
		t.Run(name, func(t *testing.T) {
			other := c
			mutate(&other)
			_, err := Authenticate(key, sealed.Bytes(), other)
			mustHold(t, err)
		})
	}
	bad := sealed.Bytes()
	bad[len(bad)-1] ^= 1
	_, err := Authenticate(key, bad, c)
	mustHold(t, err)
	_, err = Authenticate(bytes.Repeat([]byte{0x3c}, 32), sealed.Bytes(), c)
	mustHold(t, err)
	_, err = Authenticate(key, sealed.Bytes()[:12], c)
	mustHold(t, err)
	_, err = Seal([]byte("too-short"), []byte("fixture"), c)
	mustHold(t, err)
	_, err = Seal(key, nil, c)
	mustHold(t, err)
	_, err = Seal(key, make([]byte, (32<<20)+1), c)
	mustHold(t, err)
}
func TestPublicReceiptContainsOnlyCiphertextDigestAndStaysNotReady(t *testing.T) {
	key := bytes.Repeat([]byte{0x5a}, 32)
	plain := []byte("PRIVATE_TEST_CANARY_ONLY")
	c := fixtureContext()
	sealed, _ := Seal(key, plain, c)
	r, err := Receipt(sealed)
	if err != nil || r.NativeReady || r.Phase != "PREPARATION_ONLY" {
		t.Fatal("receipt overstated readiness")
	}
	raw, _ := json.Marshal(r)
	plainDigest := sha256.Sum256(plain)
	if bytes.Contains(raw, plain) || bytes.Contains(raw, []byte(hex.EncodeToString(plainDigest[:]))) || bytes.Contains(raw, key) || bytes.Contains(sealed.Bytes(), plain) {
		t.Fatal("private canary leaked")
	}
	sum := sha256.Sum256(sealed.Bytes())
	if r.CiphertextSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("not ciphertext digest")
	}
	_, err = Receipt(Sealed{})
	mustHold(t, err)
}
func TestClaimCannotChangeIntentActorOrOperation(t *testing.T) {
	c := fixtureClaim()
	m := ModelLedger{}
	if m.Claim(c) != nil || m.Claim(c) != nil {
		t.Fatal("claim failed")
	}
	for _, field := range []string{"operation", "actor", "namespace", "backup", "candidate", "topology"} {
		t.Run(field, func(t *testing.T) {
			b := c
			switch field {
			case "operation":
				b.OperationID = fixtureID(100)
			case "actor":
				b.ActorID = fixtureID(100)
			case "namespace":
				b.NamespaceUID = fixtureID(100)
			case "backup":
				b.BackupID = fixtureID(100)
			case "candidate":
				b.CandidateID = fixtureID(100)
			case "topology":
				b.Topology = "HA_LAYOUT_OBSERVED"
			}
			mustHold(t, m.Claim(b))
		})
	}
	bad := fixtureClaim()
	bad.OperationID = "not-a-uuid"
	mustHold(t, (&ModelLedger{}).Claim(bad))
}
func TestStaleAndForeignAttemptsCreateNoAttempt(t *testing.T) {
	c := fixtureClaim()
	expected := mutationObject("argocd-server")
	for _, name := range []string{"uid", "rv", "namespace", "target", "secret", "unclaimed"} {
		t.Run(name, func(t *testing.T) {
			m := ModelLedger{}
			if name != "unclaimed" {
				_ = m.Claim(c)
			}
			observed := expected
			switch name {
			case "uid":
				observed.UID = fixtureID(100)
			case "rv":
				observed.ResourceVersion = "100"
			case "namespace":
				expected.Namespace = "foreign-fixture"
				observed = expected
			case "target":
				expected.Name = "foreign-fixture"
				observed = expected
			case "secret":
				expected.Key = Key{"v1", "Secret", "argocd", "argocd-secret"}
				observed = expected
			}
			mustHold(t, m.Begin(c, expected, observed))
			if len(m.attempts) != 0 {
				t.Fatal("failed precondition recorded an attempt")
			}
			expected = mutationObject("argocd-server")
		})
	}
}
func TestUncertainAttemptBlocksAllFurtherWritesAndNewKeys(t *testing.T) {
	c := fixtureClaim()
	m := ModelLedger{}
	_ = m.Claim(c)
	first := mutationObject("argocd-server")
	second := mutationObject("argocd-repo-server")
	if m.Begin(c, first, first) != nil {
		t.Fatal("begin failed")
	}
	mustHold(t, m.Begin(c, first, first))
	mustHold(t, m.Begin(c, second, second))
	other := c
	other.OperationID = fixtureID(777)
	mustHold(t, m.Claim(other))
	observed := first
	observed.ResourceVersion = "44"
	mustHold(t, m.Qualified(c, observed, []byte("EXPECTED_PRIVATE_FIXTURE"), []byte("CHANGED_PRIVATE_FIXTURE")))
	if m.attempts[first.Key].Outcome != "ATTEMPT_IN_FLIGHT" {
		t.Fatal("unknown state was cleared")
	}
	observed.UID = fixtureID(888)
	mustHold(t, m.Qualified(c, observed, []byte("PRIVATE_FIXTURE"), []byte("PRIVATE_FIXTURE")))
	observed.UID = first.UID
	if m.Qualified(c, observed, []byte("PRIVATE_FIXTURE"), []byte("PRIVATE_FIXTURE")) != nil {
		t.Fatal("qualified poststate failed")
	}
	mustHold(t, m.Begin(c, first, first))
	if m.Begin(c, second, second) != nil {
		t.Fatal("next object refused after exact recovery")
	}
}

// Test-only in-memory journal. Production persistence and locking remain absent.
type ModelLedger struct {
	claim    *Claim
	attempts map[Key]Attempt
}

func (m *ModelLedger) Claim(c Claim) error {
	if !c.valid() || (m.claim != nil && *m.claim != c) {
		return ErrHold
	}
	m.claim = &c
	if m.attempts == nil {
		m.attempts = map[Key]Attempt{}
	}
	return nil
}
func (m *ModelLedger) Begin(c Claim, expected, observed Object) error {
	if m.claim == nil {
		return ErrHold
	}
	prior := make([]Attempt, 0, len(m.attempts))
	for _, a := range m.attempts {
		prior = append(prior, a)
	}
	proposed, err := ProposeAttempt(*m.claim, c, prior, expected, observed)
	if err != nil {
		return err
	}
	m.attempts[expected.Key] = proposed
	return nil
}
func (m *ModelLedger) Qualified(c Claim, actual Object, want, got []byte) error {
	if m.claim == nil {
		return ErrHold
	}
	prior, exists := m.attempts[actual.Key]
	if !exists {
		return ErrHold
	}
	proposed, err := QualifyAttempt(*m.claim, c, prior, actual, want, got)
	if err != nil {
		return err
	}
	m.attempts[actual.Key] = proposed
	return nil
}

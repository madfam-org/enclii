// Package argocdbackup contains unwired protected-backup primitives. It has no
// route, credential provider, durable store, Kubernetes client or upgrade writer.
// Keys must come from a separately qualified native SecretIntake profile.
// Selection and local event validation never establish native backup readiness.
package argocdbackup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
)

var ErrHold = errors.New("ARGOCD_PREPARATION_HOLD")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var rvPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)

func validUUID(s string) bool {
	return uuidPattern.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}

type Key struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
}
type Object struct {
	Key
	UID, ResourceVersion string
}

func validObject(o Object) bool {
	return validUUID(o.UID) && rvPattern.MatchString(o.ResourceVersion) && namePattern.MatchString(o.Name)
}

type ReadList struct {
	Listed    bool
	Continue  string
	Remaining int64
	Items     []Object
}
type Capture struct {
	Topology, NamespaceUID string
	Fixed                  []Object
	Groups                 map[string]ReadList
}

var protectedGroups = map[string]struct {
	API string
	Cap int
}{
	"Secret": {"v1", 128}, "ConfigMap": {"v1", 128},
	"Application":    {"argoproj.io/v1alpha1", 512},
	"ApplicationSet": {"argoproj.io/v1alpha1", 128},
	"AppProject":     {"argoproj.io/v1alpha1", 128},
}

// Selection validates only fixed-source identities and bounded complete list
// observations. It does not prove protected payload capture, extension coverage,
// writer quiescence, an atomic snapshot, encryption custody or restore readiness.
func Selection(c Capture) (map[Key]Object, error) {
	scope, err := fixedScope(c.Topology)
	if err != nil || !validUUID(c.NamespaceUID) || len(c.Fixed) != len(scope) || len(c.Groups) != len(protectedGroups) {
		return nil, ErrHold
	}
	expected := map[Key]bool{}
	for _, k := range scope {
		expected[k] = true
	}
	out := map[Key]Object{}
	for _, o := range c.Fixed {
		if !expected[o.Key] || !validObject(o) || out[o.Key].UID != "" {
			return nil, ErrHold
		}
		if o.Kind == "Namespace" && o.UID != c.NamespaceUID {
			return nil, ErrHold
		}
		out[o.Key] = o
	}
	for kind, spec := range protectedGroups {
		list, ok := c.Groups[kind]
		if !ok || !list.Listed || list.Continue != "" || list.Remaining != 0 || len(list.Items) == 0 || len(list.Items) > spec.Cap {
			return nil, ErrHold
		}
		seen := map[Key]bool{}
		for _, o := range list.Items {
			if o.APIVersion != spec.API || o.Kind != kind || o.Namespace != "argocd" || !validObject(o) || seen[o.Key] {
				return nil, ErrHold
			}
			seen[o.Key] = true
			if old, exists := out[o.Key]; exists && old != o {
				return nil, ErrHold
			}
			out[o.Key] = o
		}
		// Every fixed install object of this kind must also be present in its list.
		for _, k := range scope {
			if k.Kind == kind && !seen[k] {
				return nil, ErrHold
			}
		}
	}
	return out, nil
}

// StableSelection is deliberately conservative: RV churn holds. Real capture
// must separately qualify protected payload equality and actual snapshot scope.
func StableSelection(before, after Capture) error {
	a, e := Selection(before)
	if e != nil {
		return ErrHold
	}
	b, e := Selection(after)
	if e != nil || before.Topology != after.Topology || before.NamespaceUID != after.NamespaceUID || len(a) != len(b) {
		return ErrHold
	}
	for k, v := range a {
		if b[k] != v {
			return ErrHold
		}
	}
	return nil
}

const EnvelopeProfile = "enclii-argocd-protected-backup-v1"
const MaxPlaintextBytes = 32 << 20
const MaxCiphertextBytes = MaxPlaintextBytes + 12 + 16

type Context struct{ Profile, OperationID, NamespaceUID, BackupID, Topology string }

func (c Context) aad() ([]byte, error) {
	if c.Profile != EnvelopeProfile || !validUUID(c.OperationID) || !validUUID(c.NamespaceUID) || !validUUID(c.BackupID) {
		return nil, ErrHold
	}
	if _, err := fixedScope(c.Topology); err != nil {
		return nil, ErrHold
	}
	return json.Marshal(c)
}
func aead(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrHold
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrHold
	}
	return cipher.NewGCM(block)
}

// The key is a backend-only in-memory input from native SecretIntake custody.
// This package cannot generate, retrieve, persist, rotate or qualify that key.
// No plaintext digest is made. Production custody integration remains unwired.
type Sealed struct {
	blob    []byte
	context Context
}

func (s Sealed) Bytes() []byte { return bytes.Clone(s.blob) }
func Seal(key, plaintext []byte, c Context) (Sealed, error) {
	aad, err := c.aad()
	if err != nil || len(plaintext) == 0 || len(plaintext) > MaxPlaintextBytes {
		return Sealed{}, ErrHold
	}
	a, err := aead(key)
	if err != nil {
		return Sealed{}, ErrHold
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return Sealed{}, ErrHold
	}
	return Sealed{a.Seal(nonce, nonce, plaintext, aad), c}, nil
}
func Open(key, ciphertext []byte, c Context) ([]byte, error) {
	aad, err := c.aad()
	if err != nil {
		return nil, ErrHold
	}
	a, err := aead(key)
	if err != nil || len(ciphertext) < a.NonceSize()+a.Overhead() || len(ciphertext) > MaxCiphertextBytes {
		return nil, ErrHold
	}
	raw, err := a.Open(nil, ciphertext[:a.NonceSize()], ciphertext[a.NonceSize():], aad)
	if err != nil {
		return nil, ErrHold
	}
	return raw, nil
}

type PublicReceipt struct {
	Schema           string `json:"schema"`
	Phase            string `json:"phase"`
	OperationID      string `json:"operation_id"`
	BackupID         string `json:"backup_id"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
	Bytes            int    `json:"ciphertext_bytes"`
	NativeReady      bool   `json:"native_ready"`
}

// Receipt accepts only a sealed envelope from this package, never arbitrary
// plaintext bytes from a caller. A stored envelope must first authenticate.
func Authenticate(key, ciphertext []byte, c Context) (Sealed, error) {
	raw, err := Open(key, ciphertext, c)
	if err != nil {
		return Sealed{}, ErrHold
	}
	clear(raw)
	return Sealed{bytes.Clone(ciphertext), c}, nil
}
func Receipt(s Sealed) (PublicReceipt, error) {
	if _, err := s.context.aad(); err != nil || len(s.blob) < 29 || len(s.blob) > MaxCiphertextBytes {
		return PublicReceipt{}, ErrHold
	}
	sum := sha256.Sum256(s.blob)
	return PublicReceipt{"enclii-argocd-backup-preparation-v1", "PREPARATION_ONLY", s.context.OperationID, s.context.BackupID, hex.EncodeToString(sum[:]), len(s.blob), false}, nil
}

type Claim struct{ OperationID, ActorID, NamespaceUID, BackupID, CandidateID, Topology string }

func (c Claim) valid() bool {
	_, err := fixedScope(c.Topology)
	return err == nil && validUUID(c.OperationID) && validUUID(c.ActorID) && validUUID(c.NamespaceUID) && validUUID(c.BackupID) && validUUID(c.CandidateID)
}
func (c Claim) eligible(k Key) bool {
	if k.Kind != "Deployment" && k.Kind != "StatefulSet" && k.Kind != "CustomResourceDefinition" {
		return false
	}
	keys, err := fixedScope(c.Topology)
	if err != nil {
		return false
	}
	for _, want := range keys {
		if want == k {
			return true
		}
	}
	return false
}

type Attempt struct {
	Expected Object
	Outcome  string
}

// ProposeAttempt validates a local event transition only. The caller must
// atomically claim and durably append it before any separately qualified native
// transport. No SQL persistence, lock, API precondition or mutation is supplied.
func ProposeAttempt(retained, current Claim, prior []Attempt, expected, observed Object) (Attempt, error) {
	if !retained.valid() || retained != current || !current.eligible(expected.Key) || !validObject(expected) || expected != observed {
		return Attempt{}, ErrHold
	}
	for _, a := range prior {
		if a.Outcome != "APPLIED_POSTSTATE_QUALIFIED" || a.Expected.Key == expected.Key {
			return Attempt{}, ErrHold
		}
	}
	return Attempt{expected, "ATTEMPT_IN_FLIGHT"}, nil
}

// QualifyAttempt compares the complete caller-qualified private poststate in
// memory. It returns a proposed event only; it cannot release a durable hold,
// rerun a transport or prove the completeness of the supplied native snapshot.
func QualifyAttempt(retained, current Claim, prior Attempt, actual Object, expectedPrivate, observedPrivate []byte) (Attempt, error) {
	if !retained.valid() || retained != current || !validObject(actual) || !current.eligible(actual.Key) || prior.Outcome != "ATTEMPT_IN_FLIGHT" ||
		prior.Expected.Key != actual.Key || prior.Expected.UID != actual.UID || len(expectedPrivate) == 0 || !bytes.Equal(expectedPrivate, observedPrivate) {
		return Attempt{}, ErrHold
	}
	prior.Outcome = "APPLIED_POSTSTATE_QUALIFIED"
	return prior, nil
}

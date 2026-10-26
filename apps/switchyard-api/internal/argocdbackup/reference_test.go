package argocdbackup

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealedReferenceUsesFixedContextKeyAndExactCiphertext(t *testing.T) {
	c := fixtureContext()
	sealed, err := Seal(bytes.Repeat([]byte{0x5a}, 32), []byte("PRIVATE_TEST_CANARY_ONLY"), c)
	if err != nil {
		t.Fatal("seal failed")
	}
	ref, err := sealed.Reference()
	if err != nil {
		t.Fatal("reference failed")
	}
	key, err := ref.ObjectKey()
	if err != nil || key != "argocd-backups/v1/"+c.NamespaceUID+"/"+c.OperationID+"/"+c.BackupID+".aes256gcm" {
		t.Fatal("not a fixed context key")
	}
	if ref.Size() != int64(len(sealed.Bytes())) || !ref.Matches(sealed.Bytes()) || ref.ChecksumBase64() == "" {
		t.Fatal("ciphertext reference mismatch")
	}
	changed := sealed.Bytes()
	changed[0] ^= 1
	if ref.Matches(changed) || ref.Matches(append(sealed.Bytes(), 0)) {
		t.Fatal("changed content accepted")
	}
	r, _ := Receipt(sealed)
	reloaded, err := ReferenceFromReceipt(c, r)
	if err != nil || reloaded != ref {
		t.Fatal("receipt roundtrip failed")
	}
}
func TestReferenceRejectsUntrustedMetadataAndSelectors(t *testing.T) {
	c := fixtureContext()
	sealed, _ := Seal(bytes.Repeat([]byte{0x5a}, 32), []byte("fixture"), c)
	r, _ := Receipt(sealed)
	for name, mutate := range map[string]func(*PublicReceipt){
		"schema": func(r *PublicReceipt) { r.Schema = "foreign" }, "phase": func(r *PublicReceipt) { r.Phase = "READY" }, "ready": func(r *PublicReceipt) { r.NativeReady = true },
		"operation": func(r *PublicReceipt) { r.OperationID = fixtureID(500) }, "backup": func(r *PublicReceipt) { r.BackupID = fixtureID(501) },
		"zero-size": func(r *PublicReceipt) { r.Bytes = 0 }, "short": func(r *PublicReceipt) { r.Bytes = 28 }, "oversize": func(r *PublicReceipt) { r.Bytes = MaxCiphertextBytes + 1 },
		"invalid-digest": func(r *PublicReceipt) { r.CiphertextSHA256 = strings.Repeat("g", 64) }, "uppercase-digest": func(r *PublicReceipt) { r.CiphertextSHA256 = strings.ToUpper(r.CiphertextSHA256) },
	} {
		t.Run(name, func(t *testing.T) {
			other := r
			mutate(&other)
			_, err := ReferenceFromReceipt(c, other)
			mustHold(t, err)
		})
	}
	for name, mutate := range map[string]func(*Context){"namespace-path": func(c *Context) { c.NamespaceUID = "../../foreign" }, "profile": func(c *Context) { c.Profile = "foreign" }, "unknown-topology": func(c *Context) { c.Topology = "" }} {
		t.Run(name, func(t *testing.T) {
			other := c
			mutate(&other)
			_, err := ReferenceFromReceipt(other, r)
			mustHold(t, err)
		})
	}
	var zero Reference
	if _, err := zero.ObjectKey(); err != ErrHold || zero.Size() != 0 || zero.ChecksumBase64() != "" || zero.Matches(sealed.Bytes()) {
		t.Fatal("zero reference accepted")
	}
}

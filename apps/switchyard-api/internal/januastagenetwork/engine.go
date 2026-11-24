package januastagenetwork

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

type Native interface {
	Guard(context.Context, Plan, Receipt) error
	Create(context.Context, Object) (runtime.Object, error)
	Get(context.Context, Object) (runtime.Object, error)
}
type Kernel struct {
	Native  Native
	Journal Journal
}

// Apply is single-attempt. An existing durable claim is never resumed, even if
// its objects look identical. GET inspection is the only post-uncertainty path.
func (k Kernel) Apply(ctx context.Context, p Plan, expected, reason string) (Receipt, error) {
	r := Receipt{Plan: p}
	if p.Validate() != nil || expected != p.SHA256 || k.Native == nil || k.Journal == nil {
		return r, ErrRefused
	}
	close, e := k.Journal.Lock(ctx, p)
	if e != nil {
		return r, ErrRefused
	}
	defer close()
	if k.Native.Guard(ctx, p, r) != nil {
		return r, ErrRefused
	}
	if e = k.Journal.Claim(ctx, p, reason); e != nil {
		return r, e
	}
	for index, object := range p.Objects() {
		if k.Native.Guard(ctx, p, r) != nil {
			return r, ErrUnknown
		}
		if k.Journal.Intent(ctx, p, index, object) != nil {
			return r, ErrUnknown
		}
		r.Attempted++
		actual, createErr := k.Native.Create(ctx, object)
		if createErr != nil || actual == nil {
			return r, ErrUnknown
		}
		// Retain the first actual UID even when admission changed the desired
		// configuration. Such a receipt blocks completion, not historical custody.
		m, accessErr := meta.Accessor(actual)
		if accessErr != nil || m.GetUID() == "" || m.GetResourceVersion() == "" {
			return r, ErrUnknown
		}
		_, _, sha, projectionErr := Projection(actual)
		if projectionErr != nil {
			sha = Hash(actual)
		}
		observed := ObjectReceipt{Index: index, Key: object.Key, UID: string(m.GetUID()), ResourceVersion: m.GetResourceVersion(), SHA256: sha}
		if k.Journal.Record(ctx, p, observed) != nil {
			return r, ErrUnknown
		}
		r.Objects = append(r.Objects, observed)
		if projectionErr != nil || !Matches(object.Body, actual) {
			return r, ErrUnknown
		}
		readback, readErr := k.Native.Get(ctx, object)
		if readErr != nil {
			return r, ErrUnknown
		}
		uid, _, current, e := Projection(readback)
		if e != nil || uid != observed.UID || current != observed.SHA256 || !Matches(object.Body, readback) {
			return r, ErrUnknown
		}
	}
	if k.Native.Guard(ctx, p, r) != nil || k.Journal.Complete(ctx, p) != nil {
		return r, ErrUnknown
	}
	r.Complete = true
	return r, nil
}

// Observe never changes the operation or native objects. Missing receipts,
// replaced UIDs, policy drift and an interrupted operation cannot qualify it.
func (k Kernel) Observe(ctx context.Context, r Receipt) error {
	if r.Plan.Validate() != nil || k.Native == nil || k.Journal == nil {
		return ErrRefused
	}
	if k.Native.Guard(ctx, r.Plan, r) != nil {
		return ErrUnknown
	}
	if !r.Complete || r.Attempted != len(r.Plan.Objects()) || len(r.Objects) != r.Attempted {
		return ErrUnknown
	}
	return nil
}

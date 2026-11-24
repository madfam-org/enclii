package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	ktypes "k8s.io/apimachinery/pkg/types"
)

// This is an internal dependency on the still-missing dedicated registry
// custody adapter, not an HTTP body, label, boolean or shared registry fallback.
// Its reader must prove the retained Secret UID/version, ownership and exact
// Janua pull scope in the database namespace. The source observer neither
// creates custody nor qualifies the Job's separate registry/network gates.
type januaMigrationRegistryLease struct {
	AuthorityID uuid.UUID
	Namespace   string
	SecretName  string
	SecretUID   ktypes.UID
	Version     string
	User        string
	Password    string
}

type januaMigrationRegistryReader func(context.Context, januastage.Authority) (januaMigrationRegistryLease, error)

type januaMigrationHeldSource struct {
	Authority januastage.Authority
	Plan      januastage.Plan
	Files     []checks.ManifestFile
}

type januaMigrationSourceIO struct {
	Client       *http.Client
	GitHubToken  string
	ReadHeld     func(context.Context, uuid.UUID) (januaMigrationHeldSource, error)
	ReadRegistry januaMigrationRegistryReader
	Discover     func(context.Context, string, string, string) (januaMigrationCertificateHint, error)
	Signature    func(context.Context, string, string, string, string, string) error
}

// No routes or kernel constructors call this reader yet. A missing dedicated
// custody reader fails before any registry/source request; h.config's shared
// RegistryUsername/RegistryPassword are deliberately never read here.
func (h *Handler) januaMigrationSourceObserver(registry januaMigrationRegistryReader) januastagemigrate.SourceObserver {
	return func(ctx context.Context, id uuid.UUID) (januastagemigrate.SourceObservation, error) {
		if h == nil || h.config == nil || h.config.GitHubToken == "" || registry == nil {
			return januastagemigrate.SourceObservation{}, januastagemigrate.ErrRefused
		}
		a := januaMigrationSourceIO{
			Client:       &http.Client{Timeout: 25 * time.Second},
			GitHubToken:  h.config.GitHubToken,
			ReadRegistry: registry,
			Signature:    verifyJanuaStageMigrationSignature,
			Discover:     discoverJanuaMigrationCertificate,
			ReadHeld: func(ctx context.Context, id uuid.UUID) (januaMigrationHeldSource, error) {
				authority, err := h.readJanuaInertAuthority(ctx, id)
				if err != nil {
					return januaMigrationHeldSource{}, err
				}
				plan, retained, err := (januastage.Store{DB: h.repos.DB()}).Read(ctx, id)
				if err != nil || !sameJanuaMigrationAuthority(authority, retained) {
					return januaMigrationHeldSource{}, januastagemigrate.ErrRefused
				}
				files, err := h.fetchManifestFiles(ctx, "madfam-org", "janua", januastage.ManifestPath, authority.SourceRevision)
				return januaMigrationHeldSource{Authority: authority, Plan: plan, Files: files}, err
			},
		}
		return a.observe(ctx, id)
	}
}

func sameJanuaMigrationAuthority(a, b januastage.Authority) bool {
	a.ObservedAt, b.ObservedAt = "", ""
	return a == b
}

func januaMigrationHeldImage(s januaMigrationHeldSource, id uuid.UUID) (string, string, error) {
	a, p := s.Authority, s.Plan
	stamp, err := time.Parse(time.RFC3339Nano, a.ObservedAt)
	if id == uuid.Nil || a.ID != id || p.ID != id || januastage.ValidateAuthorityShape(a) != nil || januastage.ValidatePlan(p) != nil ||
		a.PlanSHA256 != p.SHA256 || a.SourceRevision != p.SourceRevision || a.SourceSHA256 != p.SourceSHA256 ||
		a.ProjectID != p.ProjectID || a.EnvironmentID != p.EnvironmentID || a.RepositoryRegistrationID != p.RepositoryRegistrationID ||
		a.HeldIntentID != p.HeldIntentID || a.HeldServiceID != p.HeldServiceID || a.NamespaceUID != p.NamespaceUID ||
		err != nil || time.Since(stamp) < 0 || time.Since(stamp) > 30*time.Second {
		return "", "", januastagemigrate.ErrRefused
	}
	files := append([]checks.ManifestFile(nil), s.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) != 5 || januastage.Digest(files) != a.SourceSHA256 {
		return "", "", januastagemigrate.ErrRefused
	}
	pin := ""
	for _, file := range files {
		if file.Path == "kustomization.yaml" {
			pin = file.Content
		}
	}
	entries, err := bootstrapPinEntries(pin, januastagemigrate.ImageRepository)
	if err != nil || len(entries) != 1 || exactBootstrapPin(entries, januastagemigrate.ImageRepository, entries[0].Digest) != nil || !bootstrapDigest.MatchString(entries[0].Digest) {
		return "", "", januastagemigrate.ErrRefused
	}
	image := januastagemigrate.ImageRepository + "@" + entries[0].Digest
	matched := 0
	for _, object := range p.Objects {
		if object.Kind != "Deployment" || object.Name != januastage.Deployment {
			continue
		}
		containers, ok, e := unstructuredContainers(object.Body.Object)
		if e != nil || !ok || len(containers) != 1 || containers[0] != image {
			return "", "", januastagemigrate.ErrRefused
		}
		matched++
	}
	if matched != 1 {
		return "", "", januastagemigrate.ErrRefused
	}
	return image, pin, nil
}

// Decode only the container image projection, without accepting caller input
// as an authority: the enclosing retained Plan and native read were qualified.
func unstructuredContainers(object map[string]any) ([]string, bool, error) {
	b, e := json.Marshal(object)
	if e != nil {
		return nil, false, e
	}
	var d struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct{ Image string }
				}
			}
		}
	}
	if e = json.Unmarshal(b, &d); e != nil {
		return nil, false, e
	}
	var images []string
	for _, c := range d.Spec.Template.Spec.Containers {
		images = append(images, c.Image)
	}
	return images, len(images) != 0, nil
}

func validJanuaMigrationLease(l januaMigrationRegistryLease, a januastage.Authority) bool {
	return l.AuthorityID == a.ID && l.Namespace == januastagedb.Namespace(a.ProjectID) && l.SecretName == januastagemigrate.PullSecret && l.SecretUID != "" && l.Version != "" && l.User != "" && l.Password != ""
}

func (a januaMigrationSourceIO) observe(ctx context.Context, id uuid.UUID) (januastagemigrate.SourceObservation, error) {
	refused := januastagemigrate.SourceObservation{}
	if a.Client == nil || a.GitHubToken == "" || a.ReadHeld == nil || a.ReadRegistry == nil || a.Signature == nil || a.Discover == nil {
		return refused, januastagemigrate.ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	client := *a.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	held, err := a.ReadHeld(ctx, id)
	image, pin, planErr := januaMigrationHeldImage(held, id)
	if err != nil || planErr != nil {
		return refused, januastagemigrate.ErrRefused
	}
	lease, err := a.ReadRegistry(ctx, held.Authority)
	if err != nil || !validJanuaMigrationLease(lease, held.Authority) {
		return refused, januastagemigrate.ErrRefused
	}
	// The current Janua API image has no OCI revision/source labels. Discover
	// only from successful bounded cosign output, then independently constrain
	// the actual certificate SHA/event on the SAME immutable digest. The hint
	// alone, an image tag or an OCI annotation never establishes authority.
	hint, err := a.Discover(ctx, image, lease.User, lease.Password)
	if err != nil || !bootstrapSHA.MatchString(hint.Revision) || (hint.Event != "push" && hint.Event != "workflow_dispatch") || a.Signature(ctx, image, hint.Revision, hint.Event, lease.User, lease.Password) != nil {
		return refused, januastagemigrate.ErrRefused
	}
	producer := bootstrapProducerIO{Client: &client, GitHubToken: a.GitHubToken, User: lease.User, Password: lease.Password} // pragma: allowlist secret -- typed dedicated runtime custody references, never literal credentials
	if verifyJanuaMigrationImage(ctx, producer, image, hint) != nil || verifyJanuaMigrationCI(ctx, producer, hint.Revision) != nil {
		return refused, januastagemigrate.ErrRefused
	}
	files, err := producer.readJanuaMigrationSource(ctx, hint.Revision)
	if err != nil {
		return refused, januastagemigrate.ErrRefused
	}
	descriptor, err := januastagemigrate.BuildDescriptor(image, hint.Revision, files)
	if err != nil {
		return refused, januastagemigrate.ErrRefused
	}
	// All final observations must fit the kernel's freshness window; a slow
	// native or custody reader cannot relabel older CI evidence as fresh.
	finalCtx, finalCancel := context.WithTimeout(ctx, 30*time.Second)
	defer finalCancel()
	ctx = finalCtx
	if verifyJanuaMigrationImage(ctx, producer, image, hint) != nil || verifyJanuaMigrationCI(ctx, producer, hint.Revision) != nil {
		return refused, januastagemigrate.ErrRefused
	}
	// Slow external reads never turn an earlier native/registry observation into
	// fresh authority. Repeat both readers and compare the retained projections.
	current, err := a.ReadHeld(ctx, id)
	currentImage, currentPin, currentErr := januaMigrationHeldImage(current, id)
	if err != nil || currentErr != nil || currentImage != image || currentPin != pin || !sameJanuaMigrationAuthority(held.Authority, current.Authority) || !reflect.DeepEqual(held.Plan, current.Plan) {
		return refused, januastagemigrate.ErrRefused
	}
	currentLease, err := a.ReadRegistry(ctx, current.Authority)
	if err != nil || currentLease != lease || !validJanuaMigrationLease(currentLease, current.Authority) {
		return refused, januastagemigrate.ErrRefused
	}
	stamp, err := time.Parse(time.RFC3339Nano, current.Authority.ObservedAt)
	if err != nil || time.Since(stamp) < 0 || time.Since(stamp) > 30*time.Second || ctx.Err() != nil {
		return refused, januastagemigrate.ErrRefused
	}
	return januastagemigrate.SourceObservation{AuthorityID: id, ManifestSourceRevision: current.Authority.SourceRevision, ManifestSourceSHA256: current.Authority.SourceSHA256, Descriptor: descriptor, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

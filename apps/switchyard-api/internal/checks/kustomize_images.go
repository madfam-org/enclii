package checks

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Kustomize `images:` transformer support for the onboarding image gates.
//
// The gates fetch RAW manifest files from GitHub — the API pod has no
// checkout and deliberately does not shell out to `kustomize`. But the house
// convention (docs/guides/ONBOARDING_GUIDE.md, .github/workflows/build-publish.yml)
// is that deployment YAML carries a BARE image name and CI writes the digest
// into kustomization.yaml with `kustomize edit set image`, so the digest stays
// a reviewable one-line diff and the deployment YAML stays static.
//
// Judging the raw deployment file therefore judged a value that is never
// deployed: it rejected repos that ARE digest-pinned, and the fix it implied
// — hardcoding a digest into the Deployment — would have broken the CI pin
// flow the gate exists to protect.
//
// So we implement the `images:` transformer, and ONLY that transformer,
// directly:
//
//	images:
//	  - name:    <image name exactly as written in the workload manifest>
//	    newName: <replacement name>
//	    newTag:  <replacement tag>     # mutually exclusive with digest
//	    digest:  sha256:<hex>          # mutually exclusive with newTag
//
// Everything else in a kustomization (resources, patches, overlays,
// configMapGenerator, name prefixes, …) is out of scope. In particular the
// gate only ever sees the single directory it fetched, so a `resources: [../base]`
// parent is invisible: a digest supplied by a PARENT overlay is not resolved
// here and the gate reports the image as unpinned. That is the conservative
// direction — we never certify an image we could not resolve.
//
// Failure to interpret a kustomization is a gate FAILURE, never a
// pass-through. If we cannot tell what would actually deploy, we must not
// certify it.

// kustomizationFileNames are the file names kustomize recognises as a
// kustomization, in kustomize's own preference order.
var kustomizationFileNames = []string{
	"kustomization.yaml",
	"kustomization.yml",
	"Kustomization",
}

// KustomizeImage is one entry of a kustomization `images:` list.
type KustomizeImage struct {
	Name    string `yaml:"name" json:"name"`
	NewName string `yaml:"newName" json:"new_name,omitempty"`
	NewTag  string `yaml:"newTag" json:"new_tag,omitempty"`
	Digest  string `yaml:"digest" json:"digest,omitempty"`
}

// kustomizationDoc is the (deliberately tiny) subset of a kustomization we
// parse. Images is a POINTER so "no images: key at all" stays distinguishable
// from "images: []" — ImageResolutionStats reports both, so a silently-empty
// transformer cannot masquerade as a clean pass.
type kustomizationDoc struct {
	Images *[]KustomizeImage `yaml:"images"`
}

// KustomizeImagesError is returned when a kustomization's `images:` block
// cannot be interpreted. Callers report it as its own gate so the operator
// knows to fix kustomization.yaml rather than the Deployment.
type KustomizeImagesError struct {
	File   string
	Reason string
}

func (e *KustomizeImagesError) Error() string {
	return fmt.Sprintf("kustomization %s %s", e.File, e.Reason)
}

// KustomizeImageResolver applies a kustomization's `images:` transformer to
// image references read from workload manifests in the same directory.
//
// The zero value is a valid no-op resolver (no kustomization present), which
// is what "resolve nothing, judge the manifest value as written" means.
type KustomizeImageResolver struct {
	file    string
	entries []KustomizeImage
	found   bool
}

// NewKustomizeImageResolver locates the kustomization inside a fetched
// manifest set and prepares its `images:` transformer.
//
// Returns an error (never a permissive resolver) when the kustomization is
// unparseable, ambiguous, or carries an images[] entry whose semantics we
// would have to guess.
func NewKustomizeImageResolver(manifests []ManifestFile) (*KustomizeImageResolver, error) {
	var file, content string
	for _, mf := range manifests {
		if !IsKustomizationFile(mf.Path) {
			continue
		}
		if file != "" {
			return nil, &KustomizeImagesError{
				File: file,
				Reason: fmt.Sprintf(
					"is ambiguous: the directory also contains %s, and kustomize refuses to build a directory holding more than one kustomization",
					mf.Path,
				),
			}
		}
		file, content = mf.Path, mf.Content
	}
	if file == "" {
		return &KustomizeImageResolver{}, nil
	}

	var doc kustomizationDoc
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, &KustomizeImagesError{
			File:   file,
			Reason: fmt.Sprintf("cannot be parsed as YAML: %v", err),
		}
	}
	resolver := &KustomizeImageResolver{file: file, found: true}
	if doc.Images == nil {
		return resolver, nil
	}
	if err := validateKustomizeImages(file, *doc.Images); err != nil {
		return nil, err
	}
	resolver.entries = *doc.Images
	return resolver, nil
}

// IsKustomizationFile reports whether a manifest file name is one kustomize
// would load as the directory's kustomization.
func IsKustomizationFile(filePath string) bool {
	base := path.Base(filePath)
	for _, name := range kustomizationFileNames {
		if base == name {
			return true
		}
	}
	return false
}

// File returns the kustomization's file name, or "" when the manifest set
// contains none.
func (r *KustomizeImageResolver) File() string {
	if r == nil {
		return ""
	}
	return r.file
}

// Found reports whether a kustomization file was present at all. This is NOT
// the same as "has images: entries" — see EntryCount.
func (r *KustomizeImageResolver) Found() bool {
	return r != nil && r.found
}

// EntryCount returns the number of images[] entries the transformer will
// consider. Zero with Found()==true means the kustomization declared no
// image overrides.
func (r *KustomizeImageResolver) EntryCount() int {
	if r == nil {
		return 0
	}
	return len(r.entries)
}

// ResolvedImage is the effective image reference for a single container after
// the transformer has run.
type ResolvedImage struct {
	// Image is the effective reference — what would actually deploy.
	Image string
	// Original is the reference exactly as written in the workload manifest.
	Original string
	// Entry names the images[] entries that matched, comma-joined. Empty when
	// no entry matched and the manifest value stands unchanged.
	Entry string
}

// Overridden reports whether a kustomization entry changed the reference.
func (r ResolvedImage) Overridden() bool {
	return r.Entry != ""
}

// Resolve applies every matching images[] entry, in file order, to the given
// manifest image reference.
//
// Entries are applied to the running value rather than the original, which is
// what kustomize does: a later entry keyed on an already-rewritten name can
// chain. Duplicate entry names are rejected at parse time precisely because
// that chaining makes them ambiguous.
func (r *KustomizeImageResolver) Resolve(image string) ResolvedImage {
	out := ResolvedImage{Image: image, Original: image}
	if r == nil || image == "" {
		return out
	}
	var matched []string
	for _, entry := range r.entries {
		if !isImageMatched(out.Image, entry.Name) {
			continue
		}
		out.Image = entry.apply(out.Image)
		matched = append(matched, entry.Name)
	}
	out.Entry = strings.Join(matched, ", ")
	return out
}

// apply rewrites one image reference per kustomize's image transformer:
// newName replaces the name, newTag replaces the tag, digest replaces the
// tag with "@<digest>". Fields left empty preserve the original value, so an
// entry with only newName keeps whatever tag or digest the manifest had.
func (e KustomizeImage) apply(image string) string {
	name, suffix := splitImageNameTag(image)
	if e.NewName != "" {
		name = e.NewName
	}
	if e.NewTag != "" {
		suffix = ":" + e.NewTag
	}
	if e.Digest != "" {
		suffix = "@" + e.Digest
	}
	return name + suffix
}

// isImageMatched reports whether an image reference names the image an
// images[] entry targets. Mirrors kustomize's matching rule: the entry name
// must be a PREFIX of the reference, and the remainder must be empty, a ":"
// tag, or an "@" digest. Neither tags nor digests may contain "/", so this
// can never match a partial path segment.
//
// Prefix (not suffix) matching is why infra/k8s/base/kustomization.yaml warns
// that a base images transformer prevents the production overlay from
// matching: once the base has rewritten `switchyard-api` to
// `ghcr.io/…/switchyard-api`, an entry named `switchyard-api` no longer
// matches it.
func isImageMatched(image, name string) bool {
	if name == "" || !strings.HasPrefix(image, name) {
		return false
	}
	rest := image[len(name):]
	return rest == "" || rest[0] == ':' || rest[0] == '@'
}

// splitImageNameTag splits an image reference into its name and its
// tag-or-digest suffix, keeping the separator on the suffix (":v1",
// "@sha256:…"). A ":" only separates a tag when it appears after the first
// "/", so a registry:port host is not mis-split; an "@" always wins.
func splitImageNameTag(image string) (name, suffix string) {
	sep := -1
	if slash := strings.Index(image, "/"); slash < 0 {
		sep = strings.LastIndex(image, ":")
	} else if last := strings.LastIndex(image[slash:], ":"); last > 0 {
		sep = slash + last
	}
	if at := strings.LastIndex(image, "@"); at > 0 {
		sep = at
	}
	if sep < 0 {
		return image, ""
	}
	return image[:sep], image[sep:]
}

// validateKustomizeImages rejects entries whose effect we would have to
// guess. Every rejection is a gate failure with the offending index, name and
// value named, because the operator has to find it in a file we are not
// showing them.
func validateKustomizeImages(file string, entries []KustomizeImage) error {
	seen := make(map[string]int, len(entries))
	for i, entry := range entries {
		reason := entryRejection(entry)
		if reason == "" {
			if prev, dup := seen[entry.Name]; dup {
				reason = fmt.Sprintf(
					"repeats the name %q already used by images[%d]; kustomize applies both in order, which makes the effective image ambiguous",
					entry.Name, prev,
				)
			} else {
				seen[entry.Name] = i
			}
		}
		if reason != "" {
			return &KustomizeImagesError{
				File:   file,
				Reason: fmt.Sprintf("has an unusable images[%d] entry: it %s", i, reason),
			}
		}
	}
	return nil
}

// entryRejection returns why a single images[] entry cannot be applied, or ""
// when it is well-formed.
func entryRejection(entry KustomizeImage) string {
	switch {
	case strings.TrimSpace(entry.Name) == "":
		return "has no `name`, so it can never match a workload image"
	case entry.Digest != "" && entry.NewTag != "":
		return fmt.Sprintf(
			"sets both `digest: %s` and `newTag: %s`, which kustomize treats as mutually exclusive — keep the digest and drop newTag",
			entry.Digest, entry.NewTag,
		)
	case strings.ContainsAny(entry.Digest, "@ \t"):
		return fmt.Sprintf(
			"sets `digest: %s`, which must be a bare algorithm:hex value with no '@' separator or whitespace",
			entry.Digest,
		)
	case strings.ContainsAny(entry.NewTag, ":@ \t"):
		return fmt.Sprintf(
			"sets `newTag: %s`, which must be a bare tag with no ':' or '@'",
			entry.NewTag,
		)
	case strings.Contains(entry.NewName, "@"):
		return fmt.Sprintf(
			"sets `newName: %s` containing a digest; put the digest in `digest:` so it stays a one-line CI diff",
			entry.NewName,
		)
	}
	if _, suffix := splitImageNameTag(entry.NewName); entry.NewName != "" && suffix != "" {
		return fmt.Sprintf(
			"sets `newName: %s` containing a tag; put the tag in `newTag:` (or better, pin `digest:`)",
			entry.NewName,
		)
	}
	return ""
}

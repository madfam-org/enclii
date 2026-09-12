package manifest

// Multi-document enclii.yaml.
//
// The house manifest shape is one YAML stream carrying several documents: a
// `kind: Project` document with the promotion policy, then one `kind: Service`
// document per deployed surface. ParseEncliiYAML read that stream with a single
// yaml.Unmarshal, and yaml.Unmarshal decodes the FIRST document and discards
// the rest without an error. Every consumer therefore saw one document:
//
//   - a repo whose Project document came first (nauta, lexidrop, angelia's
//     successors) exposed NO domains, NO network block and NO status entries to
//     onboarding, to `ops domains reconcile`, or to the status regenerate path;
//   - a repo whose Service document came first exposed only THAT surface, so a
//     second Service document's hostnames were invisible — and the hostnames
//     that did get provisioned were bound to whichever service the caller had
//     picked, not to the one whose document declared them.
//
// madfam-org/telesia hit both on 2026-09-12 and had to reorder its manifest and
// merge every `network:` and `status:` entry into one document to get a single
// hostname provisioned (enclii#546). This file decodes the whole stream and
// gives every consumer a per-service view of it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// KindService and KindProject are the two document kinds
// validateEncliiYAMLHeader accepts.
const (
	KindService = "Service"
	KindProject = "Project"
)

// ParseEncliiYAMLDocuments parses every document in an enclii.yaml stream, in
// declaration order.
//
// Empty documents (a bare `---`, a trailing separator, a comment-only tail) are
// skipped rather than rejected: a manifest that ends with `---` is legal YAML
// and was never an error before.
//
// Parse errors name the 1-based document index, counting empty documents, so
// the number matches what an operator counts in the file. Without it, a
// two-word error ("unsupported kind: Deployment") over a five-document manifest
// says nothing about which document to fix.
func ParseEncliiYAMLDocuments(content []byte) ([]*EncliiYAML, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	documents := make([]*EncliiYAML, 0, 4)

	for index := 1; ; index++ {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			return documents, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse enclii.yaml document %d: %w", index, err)
		}
		if isEmptyDocumentNode(&node) {
			continue
		}
		config, err := parseEncliiYAMLDocument(&node)
		if err != nil {
			return nil, fmt.Errorf("failed to parse enclii.yaml document %d: %w", index, err)
		}
		documents = append(documents, config)
	}
}

// isEmptyDocumentNode reports a document that declares nothing. yaml.v3 hands
// a bare `---` back as a DocumentNode whose single child is a null scalar.
func isEmptyDocumentNode(node *yaml.Node) bool {
	if node == nil || node.Kind == 0 {
		return true
	}
	if node.Kind != yaml.DocumentNode {
		return node.Tag == "!!null"
	}
	if len(node.Content) == 0 {
		return true
	}
	child := node.Content[0]
	return child == nil || child.Kind == 0 || child.Tag == "!!null"
}

// IsService reports whether this document declares a deployed surface.
func (c *EncliiYAML) IsService() bool {
	return c != nil && c.Kind == KindService
}

// ServiceDocuments returns the `kind: Service` documents, in declaration order.
func ServiceDocuments(documents []*EncliiYAML) []*EncliiYAML {
	services := make([]*EncliiYAML, 0, len(documents))
	for _, doc := range documents {
		if doc.IsService() {
			services = append(services, doc)
		}
	}
	return services
}

// FirstServiceDocument picks the document a whole-manifest consumer should read
// when it can only read one.
//
// A Service document is preferred because that is where a surface's domains,
// network block and status entries live; the Project document carries the
// promotion policy, which no caller of this function reads. Falling back to the
// first document keeps a Project-only manifest (the `spec.services[]` shape
// normalizeProjectSpec flattens) working exactly as it did.
func FirstServiceDocument(documents []*EncliiYAML) *EncliiYAML {
	for _, doc := range documents {
		if doc.IsService() {
			return doc
		}
	}
	if len(documents) > 0 {
		return documents[0]
	}
	return nil
}

// DocumentForService returns the document that declares a named service.
//
// Two rules, and the second one is load-bearing:
//
//  1. a `kind: Service` document whose metadata.name matches, or
//  2. for a SINGLE-document manifest, that document whatever it is named.
//
// Rule 2 preserves the behaviour every legacy single-document repo depends on.
// janua's manifest is one Service document named `janua` declaring eight
// hostnames while the registered services are janua-api / janua-admin / …, and
// tezca's service `tezca-api` reads a document named `tezca`. Matching those by
// name would answer "no document" and stop reconciling live domains, so a
// single-document manifest keeps answering for whatever service asks.
//
// A multi-document manifest gets no such fallback: there, guessing which
// document a name meant is exactly the mis-binding this package exists to stop.
func DocumentForService(documents []*EncliiYAML, serviceName string) *EncliiYAML {
	wanted := normalizeServiceName(serviceName)
	if wanted != "" {
		for _, doc := range documents {
			if doc.IsService() && normalizeServiceName(doc.Metadata.Name) == wanted {
				return doc
			}
		}
	}
	if len(documents) == 1 {
		return documents[0]
	}
	return nil
}

// DocumentNames lists the documents in a stream as `kind/name`, for an error or
// log line that has to tell an operator what the file actually declares.
func DocumentNames(documents []*EncliiYAML) []string {
	names := make([]string, 0, len(documents))
	for _, doc := range documents {
		if doc == nil {
			continue
		}
		name := doc.Metadata.Name
		if name == "" {
			name = "<unnamed>"
		}
		names = append(names, doc.Kind+"/"+name)
	}
	return names
}

func normalizeServiceName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// MergedNetwork unions the `network:` blocks of every document.
//
// Telesia had to merge its two Service documents' network entries into one
// document by hand to get them generated at all. The union is de-duplicated by
// entry name, first declaration winning, so a document repeated during a
// migration cannot double-generate a policy. Returns nil when no document
// declares a network block, so callers keep their `!= nil` gate.
func MergedNetwork(documents []*EncliiYAML) *EncliiYAMLNetwork {
	merged := &EncliiYAMLNetwork{}
	declared := false
	seenServices := make(map[string]bool)
	seenCustom := make(map[string]bool)

	for _, doc := range documents {
		if doc == nil || doc.Spec.Network == nil {
			continue
		}
		declared = true
		for _, svc := range doc.Spec.Network.Services {
			key := normalizeServiceName(svc.Name)
			if seenServices[key] {
				continue
			}
			seenServices[key] = true
			merged.Services = append(merged.Services, svc)
		}
		for _, rule := range doc.Spec.Network.Custom {
			key := normalizeServiceName(rule.Name)
			if seenCustom[key] {
				continue
			}
			seenCustom[key] = true
			merged.Custom = append(merged.Custom, rule)
		}
	}

	if !declared {
		return nil
	}
	return merged
}

// HasStatusDeclaration reports whether any document declares a `status:` block.
func HasStatusDeclaration(documents []*EncliiYAML) bool {
	for _, doc := range documents {
		if doc != nil && doc.Spec.Status != nil {
			return true
		}
	}
	return false
}

// FetchAndParseDocuments fetches enclii.yaml from a GitHub repo and parses every
// document in it. Returns nil (not an error) when the file does not exist or
// cannot be read — it is optional, and the caller decides what that costs.
func FetchAndParseDocuments(ctx context.Context, logger logging.Logger, githubToken, repoFullName, gitSHA string) []*EncliiYAML {
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		logger.Warn(ctx, "Invalid repository full name for enclii.yaml fetch",
			logging.String("repo", repoFullName))
		return nil
	}
	owner, repo := parts[0], parts[1]

	content, err := fetchGitHubRawFile(ctx, githubToken, owner, repo, "enclii.yaml", gitSHA)
	if err != nil {
		logger.Warn(ctx, "Failed to fetch enclii.yaml from repo",
			logging.String("repo", repoFullName),
			logging.Error("error", err))
		return nil
	}
	if content == nil {
		return nil // File doesn't exist — that's fine
	}

	documents, err := ParseEncliiYAMLDocuments(content)
	if err != nil {
		// Error, not Warn: this discards every domain and every header the
		// manifest declared, which is a deploy-affecting outcome and not a
		// diagnostic curiosity.
		logger.Error(ctx, "Failed to parse enclii.yaml; every domain and header it declares is being ignored for this deploy",
			logging.String("repo", repoFullName),
			logging.String("git_sha", gitSHA),
			logging.Error("error", err))
		return nil
	}
	if len(documents) == 0 {
		logger.Warn(ctx, "enclii.yaml declares no documents",
			logging.String("repo", repoFullName),
			logging.String("git_sha", gitSHA))
		return nil
	}

	if len(documents) > 1 {
		logger.Info(ctx, "Parsed multi-document enclii.yaml",
			logging.String("repo", repoFullName),
			logging.Int("document_count", len(documents)),
			logging.String("documents", strings.Join(DocumentNames(documents), ", ")))
	}

	return documents
}

// FetchAndParseForService fetches enclii.yaml and returns the document that
// declares the named service.
//
// nil with a warning when no document matches: the caller must not fall back to
// another service's document. Binding one surface's hostnames to another
// service is the defect this exists to stop — telesia's three web hostnames
// were created as junctions on telesia-api because the only document any caller
// could see was the one that happened to be first.
func FetchAndParseForService(ctx context.Context, logger logging.Logger, githubToken, repoFullName, gitSHA, serviceName string) *EncliiYAML {
	documents := FetchAndParseDocuments(ctx, logger, githubToken, repoFullName, gitSHA)
	if len(documents) == 0 {
		return nil
	}

	doc := DocumentForService(documents, serviceName)
	if doc == nil {
		logger.Warn(ctx, "enclii.yaml declares no Service document for this service; refusing to read another service's document for it",
			logging.String("repo", repoFullName),
			logging.String("service", serviceName),
			logging.String("documents", strings.Join(DocumentNames(documents), ", ")))
		return nil
	}

	if len(doc.Spec.Domains) > 0 {
		logger.Info(ctx, "Parsed domains from enclii.yaml",
			logging.String("repo", repoFullName),
			logging.String("service", serviceName),
			logging.Int("domain_count", len(doc.Spec.Domains)))
	}

	return doc
}

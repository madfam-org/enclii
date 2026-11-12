package api

import (
	"context"
	"crypto/sha1" // #nosec G505 -- Git protocol object IDs; image/signature and descriptor integrity use SHA-256.
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	"golang.org/x/sync/errgroup"
)

type januaMigrationTreeEntry struct {
	Path, Mode, Type, SHA string
	Size                  int
}

// GitHub's exact Git commit/tree/blob endpoints are used instead of a branch
// checkout, caller file list, download_url or source execution. Truncated trees,
// symlinks/submodules, missing files and mismatched Git blob bytes refuse.
// https://docs.github.com/en/rest/git/trees#get-a-tree
// https://docs.github.com/en/rest/git/blobs#get-a-blob
func (a bootstrapProducerIO) readJanuaMigrationSource(ctx context.Context, revision string) ([]januastagemigrate.SourceFile, error) {
	if a.Client == nil || a.GitHubToken == "" || !bootstrapSHA.MatchString(revision) {
		return nil, januastagemigrate.ErrRefused
	}
	base := "https://api.github.com/repos/madfam-org/janua/git/"
	var commit struct {
		SHA  string
		Tree struct{ SHA string }
	}
	if a.readJSON(ctx, base+"commits/"+revision, a.GitHubToken, &commit) != nil || commit.SHA != revision || !bootstrapSHA.MatchString(commit.Tree.SHA) {
		return nil, januastagemigrate.ErrRefused
	}
	tree := func(sha string, recursive bool) ([]januaMigrationTreeEntry, error) {
		address := base + "trees/" + sha
		if recursive {
			address += "?recursive=1"
		}
		var result struct {
			SHA       string
			Truncated *bool
			Tree      []januaMigrationTreeEntry
		}
		if !bootstrapSHA.MatchString(sha) || a.readJSON(ctx, address, a.GitHubToken, &result) != nil || result.SHA != sha || result.Truncated == nil || *result.Truncated || len(result.Tree) == 0 || len(result.Tree) > 4096 {
			return nil, januastagemigrate.ErrRefused
		}
		seen := map[string]bool{}
		for _, entry := range result.Tree {
			if entry.Path == "" || path.Clean(entry.Path) != entry.Path || strings.HasPrefix(entry.Path, "/") || strings.Contains(entry.Path, "..") || strings.Contains(entry.Path, "\\") || len(entry.Path) > 512 || seen[entry.Path] || !bootstrapSHA.MatchString(entry.SHA) {
				return nil, januastagemigrate.ErrRefused
			}
			seen[entry.Path] = true
		}
		return result.Tree, nil
	}
	root := commit.Tree.SHA
	for _, directory := range []string{"apps", "api"} {
		entries, err := tree(root, false)
		if err != nil {
			return nil, err
		}
		next := ""
		for _, e := range entries {
			if e.Path == directory && e.Type == "tree" && e.Mode == "040000" {
				next = e.SHA
			}
		}
		if next == "" {
			return nil, januastagemigrate.ErrRefused
		}
		root = next
	}
	api, err := tree(root, false)
	if err != nil {
		return nil, err
	}
	var selected []januaMigrationTreeEntry
	found := map[string]bool{}
	for _, e := range api {
		switch e.Path {
		case "alembic.ini":
			if e.Type != "blob" || (e.Mode != "100644" && e.Mode != "100755") {
				return nil, januastagemigrate.ErrRefused
			}
			selected = append(selected, e)
			found[e.Path] = true
		case "app", "alembic":
			if e.Type != "tree" || e.Mode != "040000" {
				return nil, januastagemigrate.ErrRefused
			}
			children, err := tree(e.SHA, true)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if child.Type == "tree" && child.Mode == "040000" {
					continue
				}
				if child.Type != "blob" || (child.Mode != "100644" && child.Mode != "100755") {
					return nil, januastagemigrate.ErrRefused
				}
				if strings.HasSuffix(child.Path, ".py") {
					child.Path = e.Path + "/" + child.Path
					selected = append(selected, child)
				}
			}
			found[e.Path] = true
		}
	}
	if len(found) != 3 || len(selected) < 7 || len(selected) > 1024 {
		return nil, januastagemigrate.ErrRefused
	}
	total := 0
	for _, entry := range selected {
		total += entry.Size
		if entry.Size < 0 || entry.Size > 524288 || total > 12<<20 {
			return nil, januastagemigrate.ErrRefused
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
	files := make([]januastagemigrate.SourceFile, len(selected))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for i, entry := range selected {
		group.Go(func() error {
			var blob struct {
				SHA, Encoding, Content string
				Size                   int
			}
			if a.readJSON(groupCtx, base+"blobs/"+entry.SHA, a.GitHubToken, &blob) != nil || blob.SHA != entry.SHA || blob.Encoding != "base64" || blob.Size != entry.Size {
				return januastagemigrate.ErrRefused
			}
			body, err := base64.StdEncoding.Strict().DecodeString(blob.Content)
			if err != nil || len(body) != entry.Size {
				return januastagemigrate.ErrRefused
			}
			// Git hashes the canonical object header with the bytes, not just
			// the raw file. No content URL can redirect this authenticated read.
			sum := sha1.New() // #nosec G401 -- Required Git blob object-ID algorithm, independently read over authenticated GitHub TLS.
			_, _ = fmt.Fprintf(sum, "blob %d\x00", len(body))
			_, _ = sum.Write(body)
			if hex.EncodeToString(sum.Sum(nil)) != entry.SHA {
				return januastagemigrate.ErrRefused
			}
			files[i] = januastagemigrate.SourceFile{Path: entry.Path, Content: body}
			return nil
		})
	}
	if group.Wait() != nil {
		return nil, januastagemigrate.ErrRefused
	}
	return files, nil
}

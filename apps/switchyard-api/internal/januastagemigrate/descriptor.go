// Package januastagemigrate implements a closed initial Janua Alembic profile.
// It has no HTTP request binding and must not be added to generic *_sim jobs.
package januastagemigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"
)

const Profile = "janua-staging-alembic-v1"
const ImageRepository = "ghcr.io/madfam-org/janua-api"

var ErrRefused = errors.New("fixed Janua staging migration refused")
var revision = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sourcePath = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,512}$`)
var identifier = regexp.MustCompile(`^[A-Za-z0-9_]{1,128}$`)
var revisionLine = regexp.MustCompile(`(?m)^revision(?:\s*:[^=\n]+)?\s*=\s*["']([A-Za-z0-9_]{1,128})["']\s*(?:#.*)?$`)
var parentLine = regexp.MustCompile(`(?m)^down_revision(?:\s*:[^=\n]+)?\s*=\s*(None|["'][A-Za-z0-9_]{1,128}["'])\s*(?:#.*)?$`)

// SourceFile is read from the exact certificate-bound build revision. The
// server must enumerate the complete allowed source tree; missing/extra image
// files are independently refused by the fixed Job before any app import/DDL.
type SourceFile struct {
	Path    string
	Content []byte
}
type FileHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Descriptor struct {
	Profile             string     `json:"profile"`
	Image               string     `json:"image"`
	ImageSourceRevision string     `json:"image_source_revision"`
	Head                string     `json:"head"`
	Revisions           int        `json:"revisions"`
	Files               []FileHash `json:"files"`
	GraphSHA256         string     `json:"graph_sha256"`
}

func allowedFile(p string) bool {
	return sourcePath.MatchString(p) && p == path.Clean(p) && !strings.Contains(p, "\\") && !strings.HasPrefix(p, "/") && !strings.Contains(p, "..") && (p == "alembic.ini" || (strings.HasPrefix(p, "app/") || strings.HasPrefix(p, "alembic/")) && strings.HasSuffix(p, ".py"))
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func validImage(image string) bool {
	return strings.HasPrefix(image, ImageRepository+"@sha256:") && digest.MatchString(strings.TrimPrefix(image, ImageRepository+"@sha256:"))
}

func BuildDescriptor(image, sourceRevision string, files []SourceFile) (Descriptor, error) {
	if !validImage(image) || !revision.MatchString(sourceRevision) || len(files) < 7 || len(files) > 1024 {
		return Descriptor{}, ErrRefused
	}
	d := Descriptor{Profile: Profile, Image: image, ImageSourceRevision: sourceRevision}
	seen := map[string]bool{}
	parents := map[string]string{}
	total := 0
	for _, f := range files {
		total += len(f.Content)
		if !allowedFile(f.Path) || seen[f.Path] || len(f.Content) > 524288 || total > 12<<20 {
			return Descriptor{}, ErrRefused
		}
		seen[f.Path] = true
		s := sha256.Sum256(f.Content)
		d.Files = append(d.Files, FileHash{Path: f.Path, SHA256: hex.EncodeToString(s[:])})
		if strings.HasPrefix(f.Path, "alembic/versions/") && strings.HasSuffix(f.Path, ".py") && path.Base(f.Path) != "__init__.py" {
			if strings.TrimPrefix(f.Path, "alembic/versions/") != path.Base(f.Path) {
				return Descriptor{}, ErrRefused
			}
			r := revisionLine.FindAllSubmatch(f.Content, -1)
			p := parentLine.FindAllSubmatch(f.Content, -1)
			if len(r) != 1 || len(p) != 1 {
				return Descriptor{}, ErrRefused
			}
			id := string(r[0][1])
			if _, duplicate := parents[id]; duplicate {
				return Descriptor{}, ErrRefused
			}
			parent := string(p[0][1])
			if parent == "None" {
				parent = ""
			} else {
				parent = strings.Trim(parent, "\"'")
			}
			parents[id] = parent
		}
	}
	for _, required := range []string{"alembic.ini", "alembic/env.py", "app/config.py", "app/core/database.py", "app/core/database_ssl.py", "app/models/__init__.py"} {
		if !seen[required] {
			return Descriptor{}, ErrRefused
		}
	}
	if len(parents) == 0 {
		return Descriptor{}, ErrRefused
	}
	heads := map[string]bool{}
	roots := 0
	for id := range parents {
		heads[id] = true
	}
	for _, p := range parents {
		if p == "" {
			roots++
		} else {
			if _, exists := parents[p]; !exists {
				return Descriptor{}, ErrRefused
			}
			delete(heads, p)
		}
	}
	if roots != 1 || len(heads) != 1 {
		return Descriptor{}, ErrRefused
	}
	for head := range heads {
		d.Head = head
	}
	visited := map[string]bool{}
	for id := d.Head; id != ""; id = parents[id] {
		if visited[id] {
			return Descriptor{}, ErrRefused
		}
		visited[id] = true
	}
	if len(visited) != len(parents) {
		return Descriptor{}, ErrRefused
	}
	d.Revisions = len(parents)
	sort.Slice(d.Files, func(i, j int) bool { return d.Files[i].Path < d.Files[j].Path })
	d.GraphSHA256 = hash(d)
	return d, nil
}

func (d Descriptor) Validate() error {
	if d.Profile != Profile || !validImage(d.Image) || !revision.MatchString(d.ImageSourceRevision) || !identifier.MatchString(d.Head) || d.Revisions < 1 || d.Revisions > 1024 || len(d.Files) < 7 || len(d.Files) > 1024 || !digest.MatchString(d.GraphSHA256) {
		return ErrRefused
	}
	copy := d
	copy.GraphSHA256 = ""
	if hash(copy) != d.GraphSHA256 {
		return ErrRefused
	}
	seen := map[string]bool{}
	prior := ""
	for _, f := range d.Files {
		if !allowedFile(f.Path) || seen[f.Path] || f.Path <= prior || !digest.MatchString(f.SHA256) {
			return ErrRefused
		}
		seen[f.Path] = true
		prior = f.Path
	}
	for _, required := range []string{"alembic.ini", "alembic/env.py", "app/config.py", "app/core/database.py", "app/core/database_ssl.py", "app/models/__init__.py"} {
		if !seen[required] {
			return ErrRefused
		}
	}
	return nil
}

package gitfs

import (
	"path/filepath"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
)

// repoHandle is one go-git repository, shared by every backend opened against
// the same path regardless of which commit each has pinned.
//
// Opening a repository is per-repository work — go-git reads the config and
// builds pack-index state — while pinning is per-commit. A handle per commit
// duplicated the former for no benefit: two snapshots of one repository each
// carried their own object storage and their own index.
//
// mu moves here from the backend for the same reason. Once the repository is
// shared, serialising per backend is no longer enough: two backends on
// different commits now reach the same unsynchronised go-git storer.
type repoHandle struct {
	mu   sync.Mutex
	repo *git.Repository
}

// backendKey identifies a reusable backend. It carries only what changes how
// objects are read — the repository, the pinned commit, and the two options
// that select or alter the backend. View options (WithSparse,
// WithExtendedStats) stay out deliberately: they shape what a GitFS shows on
// top of a backend, so two GitFS with different sparse sets share one backend
// rather than being forced apart — and, more importantly, neither can inherit
// the other's visibility.
type backendKey struct {
	path             string
	sha              string
	gitBinary        string
	blameFallbackGit string
}

// pinnedBackend is a cache entry: an opened, pinned backend plus the commit
// time pin reported, so reusing it doesn't have to pin again to learn it.
type pinnedBackend struct {
	be      backend
	modTime time.Time
}

var (
	cacheMu      sync.Mutex
	repoCache    = map[string]*repoHandle{}
	backendCache = map[backendKey]pinnedBackend{}
)

// sharedBackend returns a backend for the given repository, commit and
// backend-affecting options, reusing one already opened for the same key. A
// repository is opened at most once per path and a commit pinned at most once
// per key, so opening the same snapshot repeatedly costs a map lookup.
func sharedBackend(repoPath, sha string, cfg config) (backend, time.Time, error) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, time.Time{}, err
	}
	key := backendKey{
		path:             abs,
		sha:              sha,
		gitBinary:        cfg.gitBinary,
		blameFallbackGit: cfg.blameFallbackGit,
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if pb, ok := backendCache[key]; ok {
		return pb.be, pb.modTime, nil
	}

	var be backend
	if cfg.gitBinary != "" {
		be = &execBackend{binary: cfg.gitBinary}
	} else {
		h, ok := repoCache[abs]
		if !ok {
			h = &repoHandle{}
			repoCache[abs] = h
		}
		be = &gogitBackend{h: h, blameGitBinary: cfg.blameFallbackGit}
	}
	if err := be.open(abs); err != nil {
		return nil, time.Time{}, err
	}
	modTime, err := be.pin(sha)
	if err != nil {
		return nil, time.Time{}, err
	}
	backendCache[key] = pinnedBackend{be: be, modTime: modTime}
	return be, modTime, nil
}

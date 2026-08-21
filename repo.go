package gitfs

import (
	"path/filepath"
	"sync"

	git "github.com/go-git/go-git/v5"
)

// repoHandle is one go-git repository, shared by every GitFS opened against
// the same path.
//
// Opening a repository is per-repository work — go-git reads the config and
// builds pack-index state — while pinning a commit is per-GitFS. Opening one
// handle per commit duplicated the former for no benefit: two GitFS on the
// same repository each carried their own object storage and their own copy of
// the index.
//
// The mutex is not incidental. go-git's filesystem storer has no internal
// synchronisation and mutates lazily on read: object.Tree builds its entry
// map on the first FindEntry, and dotgit caches whether a repository has
// incoming objects. Both are write-on-read, so two concurrent readers race
// even when neither is writing to the repository. Sharing a handle without
// serialising access would turn a per-GitFS race into a process-wide one, so
// every read through the shared repository takes mu.
type repoHandle struct {
	mu   sync.Mutex
	repo *git.Repository
}

var (
	repoCacheMu sync.Mutex
	repoCache   = map[string]*repoHandle{}
)

// openRepo returns the shared handle for the repository at path, opening it on
// first use. Handles are keyed by absolute path and live for the process:
// callers hold a GitFS for as long as they need the commit, and a repository
// that has been opened once is almost always opened again.
func openRepo(path string) (*repoHandle, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	repoCacheMu.Lock()
	defer repoCacheMu.Unlock()

	if h, ok := repoCache[abs]; ok {
		return h, nil
	}
	repo, err := git.PlainOpen(abs)
	if err != nil {
		return nil, err
	}
	h := &repoHandle{repo: repo}
	repoCache[abs] = h
	return h, nil
}

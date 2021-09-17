package stdlib

import (
	"io"
	"io/fs"
	"log"
	"path"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// gitFS is a filesystem backed by a git repository.
type gitFS struct {
	repo  *git.Repository
	files map[string]object.TreeEntry
	dirs  map[string]*object.Tree
}

func newGitFS(repo *git.Repository, tree *object.Tree) (fs.FS, error) {
	fsys := &gitFS{
		repo:  repo,
		files: map[string]object.TreeEntry{},
		dirs:  map[string]*object.Tree{},
	}
	fsys.files["."] = object.TreeEntry{
		Name: ".",
		Mode: filemode.Dir,
		Hash: tree.Hash,
	}
	if err := fsys.addEntries(repo, tree, "."); err != nil {
		return nil, err
	}
	return fsys, nil
}

func (fsys *gitFS) addEntries(repo *git.Repository, tree *object.Tree, dir string) error {
	fsys.dirs[dir] = tree
	for _, entry := range tree.Entries {
		epath := path.Join(dir, entry.Name)
		fsys.files[epath] = entry
		if entry.Mode == filemode.Dir {
			subtree, err := repo.TreeObject(entry.Hash)
			if err != nil {
				return err
			}
			if err := fsys.addEntries(repo, subtree, epath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fsys *gitFS) Open(path string) (fs.File, error) {
	entry, ok := fsys.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return newGitFile(fsys.repo, entry)
}

func (fsys *gitFS) ReadDir(name string) ([]fs.DirEntry, error) {
	tree, ok := fsys.dirs[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	es := make([]fs.DirEntry, len(tree.Entries))
	for i := 0; i < len(tree.Entries); i++ {
		file, err := newGitFile(fsys.repo, tree.Entries[i])
		if err != nil {
			log.Println("failed to create file", tree.Entries[i].Name)
			return nil, err
		}
		es[i] = file
	}
	return es, nil
}

// gitFile represents a file from a git repository.
// It implements fs.File, fs.FileInfo, and fs.DirEntry.
type gitFile struct {
	entry  object.TreeEntry
	reader io.ReadCloser
	size   int64
}

func newGitFile(repo *git.Repository, entry object.TreeEntry) (*gitFile, error) {
	file := &gitFile{
		entry: entry,
	}
	switch entry.Mode {
	case filemode.Regular, filemode.Executable:
		blob, err := repo.BlobObject(entry.Hash)
		if err != nil {
			return nil, err
		}
		reader, err := blob.Reader()
		if err != nil {
			return nil, err
		}
		file.reader = reader
		file.size = blob.Size
	}
	return file, nil
}

func (f gitFile) Stat() (fs.FileInfo, error) {
	return f, nil
}

func (f gitFile) Read(b []byte) (int, error) {
	if f.reader == nil {
		return 0, fs.ErrInvalid
	}
	return f.reader.Read(b)
}

func (f gitFile) Close() error {
	if f.reader == nil {
		return fs.ErrInvalid
	}
	return f.reader.Close()
}

func (f gitFile) Name() string {
	return f.entry.Name
}

func (f gitFile) Size() int64 {
	return f.size
}

func (f gitFile) Mode() fs.FileMode {
	switch f.entry.Mode {
	case filemode.Dir:
		return fs.ModeDir
	default:
		return 0644
	}
}

func (f gitFile) ModTime() time.Time {
	return time.Time{}
}

func (f gitFile) IsDir() bool {
	return f.Mode().IsDir()
}

func (f gitFile) Sys() interface{} {
	return nil
}

func (f gitFile) Type() fs.FileMode {
	return f.Mode().Type()
}

func (f gitFile) Info() (fs.FileInfo, error) {
	return f, nil
}

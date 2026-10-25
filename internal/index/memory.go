package index

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

// An encrypted archive's search index lives only in memory (design phase2b §6,
// A23): it is loaded from the record's decrypted image and captured back at each
// commit, and no search.db, -wal, -shm or -journal is ever written. The load route
// is the one proven on the pinned driver: a read-only one-file in-memory VFS
// restored into a private :memory: connection (Deserialize is unsafe at this
// driver version), the image captured with Serialize.

// memoryPragmas: an in-memory rollback journal (OFF would break the per-row
// savepoints Add rolls back to, F49), temporary tables in memory, no mmap.
const memoryPragmas = `PRAGMA journal_mode=MEMORY; PRAGMA temp_store=MEMORY; PRAGMA mmap_size=0;`

// imageName is the one file the load VFS serves.
const imageName = "search.db"

// ErrImage refuses an image that is not a rollback-journal SQLite database: a
// sealed image is always one (§6), so anything else is damage.
var ErrImage = errors.New("index: the sealed search index image is not a database this build writes")

// OpenMemory opens the search index held in image — the record's decrypted index,
// or nil for a new one — in memory, and begins as Open does: the schema made if
// absent, the version resolved, a newer one refused (ErrNewerFormat).
func OpenMemory(image []byte) (*Index, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	// One connection, never recycled: each new :memory: connection is a new, empty
	// database.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if len(image) > 0 {
		if err := restore(db, image); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(memoryPragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("index pragmas: %w", err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || !strings.EqualFold(mode, "memory") {
		db.Close()
		return nil, fmt.Errorf("index pragmas: journal mode %q, want memory (%v)", mode, err)
	}
	hadDocs := tableExists(db, "docs")
	hadMeta := tableExists(db, "meta")
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("index schema: %w", err)
	}
	version, err := resolveIndexVersion(db, hadDocs, hadMeta)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("index version: %w", err)
	}
	if version > indexVersion {
		db.Close()
		return nil, fmt.Errorf("the sealed search index was %w (format version %d; this build understands %d): upgrade this copy of mailarchive", ErrNewerFormat, version, indexVersion)
	}
	return &Index{db: db, version: version, memory: true}, nil
}

// restore loads image into db's one connection through a read-only VFS that serves
// only the image, unregistered afterwards.
// A sealed image is a rollback-journal database; one that is not a database, or one
// written in WAL mode (which does not load from bytes, the spike), fails the restore
// itself and is refused as ErrImage.
func restore(db *sql.DB, image []byte) error {
	name, fsys, err := vfs.New(&imageFS{data: image})
	if err != nil {
		return fmt.Errorf("index load: %w", err)
	}
	defer fsys.Close()
	return withConn(db, func(dc any) error {
		r, ok := dc.(interface {
			NewRestore(string) (*sqlite.Backup, error)
		})
		if !ok {
			return fmt.Errorf("index load: the driver cannot restore (%T)", dc)
		}
		b, err := r.NewRestore("file:" + imageName + "?vfs=" + name + "&mode=ro")
		if err != nil {
			return fmt.Errorf("index load: %w", err)
		}
		more, serr := b.Step(-1)
		ferr := b.Finish()
		switch {
		case serr != nil:
			return fmt.Errorf("%w: %v", ErrImage, serr)
		case more:
			return fmt.Errorf("%w: pages left after a full restore", ErrImage)
		case ferr != nil:
			return fmt.Errorf("index load: %w", ferr)
		}
		return nil
	})
}

// Image commits any pending batch and returns the whole database as bytes — the
// image the record seals. Only for an index opened in memory.
func (ix *Index) Image() ([]byte, error) {
	if !ix.memory {
		return nil, errors.New("index: only an in-memory index has an image")
	}
	if err := ix.ensure(); err != nil {
		return nil, err
	}
	if err := ix.commitPending(); err != nil {
		return nil, err
	}
	var out []byte
	err := withConn(ix.db, func(dc any) error {
		s, ok := dc.(interface{ Serialize() ([]byte, error) })
		if !ok {
			return fmt.Errorf("index image: the driver cannot serialize (%T)", dc)
		}
		var err error
		out, err = s.Serialize()
		return err
	})
	return out, err
}

// Changes is SQLite's count of rows this connection has written since it opened
// (total_changes()), pending batch included: a caller that captured the image at
// one count need not capture it again until the count moves. A lazy index counts
// from its load, and has written nothing before it.
func (ix *Index) Changes() (int64, error) {
	if ix.idle() {
		return 0, nil
	}
	var n int64
	var err error
	if ix.tx != nil {
		err = ix.tx.QueryRow(`SELECT total_changes()`).Scan(&n)
	} else {
		err = ix.db.QueryRow(`SELECT total_changes()`).Scan(&n)
	}
	return n - ix.base, err
}

// withConn hands fn the driver connection of db's one connection.
func withConn(db *sql.DB, fn func(dc any) error) error {
	c, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Raw(fn)
}

// imageFS is a read-only fs.FS of one file, the image.
type imageFS struct{ data []byte }

func (m *imageFS) Open(name string) (fs.File, error) {
	if name != imageName {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &imageFile{r: bytes.NewReader(m.data), size: int64(len(m.data))}, nil
}

type imageFile struct {
	r    *bytes.Reader
	size int64
}

func (f *imageFile) Stat() (fs.FileInfo, error)                { return imageInfo{f.size}, nil }
func (f *imageFile) Read(p []byte) (int, error)                { return f.r.Read(p) }
func (f *imageFile) Seek(off int64, whence int) (int64, error) { return f.r.Seek(off, whence) }
func (f *imageFile) Close() error                              { return nil }

type imageInfo struct{ size int64 }

func (i imageInfo) Name() string       { return imageName }
func (i imageInfo) Size() int64        { return i.size }
func (i imageInfo) Mode() fs.FileMode  { return 0o444 }
func (i imageInfo) ModTime() time.Time { return time.Time{} }
func (i imageInfo) IsDir() bool        { return false }
func (i imageInfo) Sys() any           { return nil }

// Package database manages the storage of documentation.
package database

// See schema.sql for the database schema.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"path"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"github.com/lib/pq"
)

// Package represents a package.
type Package struct {
	ImportPath    string    `json:"import_path"`
	ModulePath    string    `json:"module_path"`
	SeriesPath    string    `json:"-"`
	Version       string    `json:"version"`
	CommitTime    time.Time `json:"commit_time"`
	LatestVersion string    `json:"-"`
	Versions      []string  `json:"-"`
	Name          string    `json:"name"`
	Synopsis      string    `json:"synopsis"`
	Updated       time.Time `json:"-"`
}

// Database stores package documentation.
type Database struct {
	pg *sql.DB
}

// New creates a new database. serverURI is the postgres URI.
func New(serverURI string) (*Database, error) {
	db, err := sql.Open("postgres", serverURI)
	if err != nil {
		return nil, err
	}
	return &Database{pg: db}, nil
}

func (db *Database) withTx(ctx context.Context, opts *sql.TxOptions,
	fn func(tx *sql.Tx) error) error {

	conn, err := db.pg.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, opts)
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
		tx.Commit()
	}()

	err = fn(tx)
	if err != nil {
		tx.Rollback()
	}
	return err
}

// HasModule reports whether the given module is present in the database.
func (db *Database) HasModule(ctx context.Context, modulePath string) (bool, error) {
	exists := false
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT EXISTS (SELECT FROM modules WHERE module_path = $1);`,
			modulePath)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&exists); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return false, err
	}
	return exists, nil
}

// Delete deletes the module with the given module path.
func (db *Database) Delete(ctx context.Context, modulePath string) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM modules WHERE module_path = $1;`, modulePath)
		if err != nil {
			return err
		}
		return nil
	})
}

const searchQuery = `
SELECT
	p.import_path, p.module_path, p.series_path, p.version,
	p.commit_time, p.name, p.synopsis
FROM packages p, modules m
WHERE p.searchtext @@ websearch_to_tsquery('english', $1)
	AND m.module_path = p.module_path AND p.version = m.latest_version
ORDER BY ts_rank(p.searchtext, websearch_to_tsquery('english', $1)) DESC,
	p.score DESC
LIMIT 20;
`

// Search performs a search with the provided query string.
func (db *Database) Search(ctx context.Context, query string) ([]Package, error) {
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, searchQuery, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var pkg Package
			if err := rows.Scan(&pkg.ImportPath, &pkg.ModulePath, &pkg.SeriesPath,
				&pkg.Version, &pkg.CommitTime, &pkg.Name, &pkg.Synopsis); err != nil {
				return err
			}
			packages = append(packages, pkg)
		}
		return rows.Err()
	})
	return packages, err
}

const insertModule = `
INSERT INTO modules (
	module_path, series_path, latest_version, versions, updated
) VALUES ( $1, $2, $3, $4, NOW() )
ON CONFLICT (module_path) DO
UPDATE SET series_path = $2, latest_version = $3, versions = $4, updated = NOW();
`

// PutModule updates the module in the database.
func (db *Database) PutModule(ctx context.Context, modulePath, seriesPath, latestVersion string,
	versions []string) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertModule,
			modulePath, seriesPath, latestVersion, pq.StringArray(versions))
		if err != nil {
			return err
		}
		return nil
	})
}

const packageQuery = `
SELECT
	p.module_path, p.series_path, p.version, p.commit_time, m.latest_version, m.versions,
	p.name, p.synopsis, m.updated
FROM packages p, modules m
WHERE p.import_path = $1 AND p.version = $2 AND m.module_path = p.module_path;
`

const packageLatestQuery = `
SELECT
	p.module_path, p.series_path, p.version, p.commit_time, m.latest_version, m.versions,
	p.name, p.synopsis, m.updated
FROM packages p, modules m
WHERE p.import_path = $1 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

// GetPackage returns information about the given package.
func (db *Database) GetPackage(ctx context.Context, importPath, version string) (Package, bool, error) {
	var pkg Package
	ok := false
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		var rows *sql.Rows
		var err error
		if version == "latest" {
			rows, err = tx.QueryContext(ctx, packageLatestQuery, importPath)
		} else {
			rows, err = tx.QueryContext(ctx, packageQuery, importPath, version)
		}
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&pkg.ModulePath, &pkg.SeriesPath, &pkg.Version,
				&pkg.CommitTime, &pkg.LatestVersion, (*pq.StringArray)(&pkg.Versions),
				&pkg.Name, &pkg.Synopsis, &pkg.Updated); err != nil {
				return err
			}
			pkg.ImportPath = importPath
			ok = true
		}
		return rows.Err()
	})
	if err != nil {
		return Package{}, false, err
	}
	return pkg, ok, nil
}

const documentationQuery = `
SELECT documentation FROM documentation
WHERE import_path = $1 AND version = $2 AND goos = $3 AND goarch = $4;
`

// GetDoc retrieves the documentation for the given package.
func (db *Database) GetDoc(ctx context.Context, importPath, version, goos, goarch string) (*doc.Package, error) {
	var pdoc *doc.Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, documentationQuery,
			importPath, version, goos, goarch)
		if err != nil {
			return err
		}
		defer rows.Close()

		if !rows.Next() {
			return errors.New("failed to get documentation")
		}
		var p []byte
		if err := rows.Scan(&p); err != nil {
			return err
		}
		pdoc = new(doc.Package)
		if err := gob.NewDecoder(bytes.NewReader(p)).Decode(&pdoc); err != nil {
			return err
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return pdoc, nil
}

const insertPackage = `
INSERT INTO packages (
	import_path, module_path, series_path, version, commit_time, name, synopsis, score
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8
);
`

const insertDocumentation = `
INSERT INTO documentation (
	import_path, version, goos, goarch, documentation
) VALUES (
	$1, $2, $3, $4, $5
);
`

const insertImport = `
INSERT INTO imports (
	import_path, version, imported_path
) VALUES (
	$1, $2, $3
);
`

// PutPackage puts the package documentation in the database.
func (db *Database) PutPackage(ctx context.Context, modulePath, seriesPath, version string,
	commitTime time.Time, pkg *doc.Package) error {

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(pkg); err != nil {
		return err
	}

	// Truncate large documents.
	if len(buf.Bytes()) > 1200000 {
		pdocNew := *pkg
		pkg = &pdocNew
		pkg.Truncated = true
		pkg.Consts = nil
		pkg.Types = nil
		pkg.Vars = nil
		pkg.Funcs = nil
		pkg.Examples = nil
		buf.Reset()
		if err := gob.NewEncoder(&buf).Encode(pkg); err != nil {
			return err
		}
	}

	score := searchScore(pkg)

	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertPackage,
			pkg.ImportPath, modulePath, seriesPath, version, commitTime,
			pkg.Name, pkg.Synopsis, score)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, insertDocumentation,
			pkg.ImportPath, version, pkg.GOOS, pkg.GOARCH, buf.Bytes())
		if err != nil {
			return err
		}

		for _, importedPath := range pkg.Imports {
			_, err = tx.ExecContext(ctx, insertImport,
				pkg.ImportPath, version, importedPath)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// HasPackage reports whether the given package is present in the database.
func (db *Database) HasPackage(ctx context.Context, importPath, version string) (bool, error) {
	exists := false
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT EXISTS (SELECT FROM packages WHERE import_path = $1 AND version = $2);`,
			importPath, version)
		if err != nil {
			return err
		}
		if rows.Next() {
			if err := rows.Scan(&exists); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return false, err
	}
	return exists, nil
}

// Block puts a domain, repo or package into the block set, removes all the
// packages under it from the database and prevents future crawling from it.
func (db *Database) Block(ctx context.Context, importPath string) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		// Delete all matching modules
		_, err := tx.ExecContext(ctx,
			`DELETE FROM modules WHERE module_path LIKE $1 || '%';`, importPath)
		if err != nil {
			return err
		}

		// Add the import path to the blocklist
		_, err = tx.ExecContext(ctx,
			`INSERT INTO blocklist (import_path) VALUES ($1);`, importPath)
		if err != nil {
			return err
		}
		return nil
	})
}

// IsBlocked returns whether the package is blocked or belongs to a blocked
// domain/repo.
func (db *Database) IsBlocked(ctx context.Context, importPath string) (bool, error) {
	parts := strings.Split(importPath, "/")
	importPath = ""
	for _, part := range parts {
		importPath = path.Join(importPath, part)
		var blocked bool
		err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx,
				`SELECT EXISTS (SELECT FROM blocklist WHERE import_path = $1);`,
				importPath)
			if err != nil {
				return err
			}
			defer rows.Close()
			if rows.Next() {
				if err := rows.Scan(&blocked); err != nil {
					return err
				}
			}
			return rows.Err()
		})
		if err != nil {
			return false, err
		}
		if blocked {
			return true, nil
		}
	}
	return false, nil
}

// Imports returns a package's imports.
func (db *Database) Imports(ctx context.Context, importPath, version string) ([]string, error) {
	var imports []string
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT imported_path FROM imports WHERE import_path = $1 AND version = $2;`,
			importPath, version)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var imprt string
			if err := rows.Scan(&imprt); err != nil {
				return err
			}
			imports = append(imports, imprt)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return imports, nil
}

const synopsisQuery = `
SELECT p.synopsis
FROM packages p, modules m
WHERE p.import_path = $1 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

func (db *Database) synopsis(ctx context.Context, importPath string) (string, error) {
	var synopsis string
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, synopsisQuery, importPath)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&synopsis); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return "", err
	}
	return synopsis, nil
}

// Packages returns a list of package information for the given import paths.
// Only the ImportPath and Synopsis fields will be populated.
func (db *Database) Packages(ctx context.Context, importPaths []string) ([]Package, error) {
	var packages []Package
	for _, importPath := range importPaths {
		synopsis, err := db.synopsis(ctx, importPath)
		if err != nil {
			return nil, err
		}
		packages = append(packages, Package{
			ImportPath: importPath,
			Synopsis:   synopsis,
		})
	}
	return packages, nil
}

const subpackagesQuery = `
SELECT
	import_path, series_path, commit_time, name, synopsis
FROM packages
WHERE module_path = $1 AND version = $2 AND import_path LIKE $3 || '_%'
AND ($4 OR import_path NOT LIKE '%/internal/%')
ORDER BY import_path;
`

// SubPackages returns the subpackages of the given package.
func (db *Database) SubPackages(ctx context.Context, modulePath, version, importPath string) ([]Package, error) {
	internal := strings.HasSuffix(importPath, "/internal") ||
		strings.Contains(importPath, "/internal/")
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, subpackagesQuery,
			modulePath, version, importPath, internal)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var pkg Package
			if err := rows.Scan(&pkg.ImportPath, &pkg.SeriesPath,
				&pkg.CommitTime, &pkg.Name, &pkg.Synopsis); err != nil {
				return err
			}
			pkg.ModulePath = modulePath
			pkg.Version = version
			packages = append(packages, pkg)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return packages, nil
}

// DepLevel specifies the level of dependencies to show in an import graph.
type DepLevel int

const (
	ShowAllDeps      DepLevel = iota // show all dependencies
	HideStandardDeps                 // don't show dependencies of standard libraries
	HideStandardAll                  // don't show standard libraries at all
)

const importGraphPackageQuery = `
SELECT p.version, p.synopsis
FROM packages p, modules m
WHERE p.import_path = $1 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

func (db *Database) importGraphPackage(ctx context.Context, importPath string) (Package, error) {
	pkg := Package{
		ImportPath: importPath,
	}
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, importGraphPackageQuery, importPath)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&pkg.Version, &pkg.Synopsis); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return Package{}, err
	}
	return pkg, nil
}

// ImportGraph performs a breadth-first traversal of the package's dependencies.
func (db *Database) ImportGraph(ctx context.Context, pdoc *doc.Package, level DepLevel) ([]Package, [][2]int, error) {

	var queue []Package
	nodes := []Package{{ImportPath: pdoc.ImportPath, Synopsis: pdoc.Synopsis}}
	edges := [][2]int{}
	index := map[string]int{pdoc.ImportPath: 0}

	for _, importPath := range pdoc.Imports {
		if level >= HideStandardAll && stdlib.Contains(importPath) {
			continue
		}
		j := len(nodes)
		index[importPath] = j
		edges = append(edges, [2]int{0, j})
		pkg, err := db.importGraphPackage(ctx, importPath)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, pkg)
		queue = append(queue, pkg)
	}

	for i := 1; i < len(nodes); i++ {
		var pkg Package
		pkg, queue = queue[0], queue[1:]
		imports, err := db.Imports(ctx, pkg.ImportPath, pkg.Version)
		if err != nil {
			return nil, nil, err
		}
		for _, importPath := range imports {
			if level >= HideStandardDeps && stdlib.Contains(importPath) {
				continue
			}
			j, ok := index[importPath]
			if !ok {
				j = len(nodes)
				index[importPath] = j
				pkg, err := db.importGraphPackage(ctx, importPath)
				if err != nil {
					return nil, nil, err
				}
				nodes = append(nodes, pkg)
				queue = append(queue, pkg)
			}
			edges = append(edges, [2]int{i, j})
		}
	}
	return nodes, edges, nil
}

const gosourceQuery = `
SELECT project_name, project_url, dir_fmt, file_fmt, line_fmt
FROM gosource
WHERE project_root = $1;
`

// GetMeta returns go-source meta tag information for the given module.
func (db *Database) GetMeta(ctx context.Context, modulePath string) (meta source.Meta, ok bool, err error) {
	err = db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, gosourceQuery, modulePath)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&meta.ProjectName, &meta.ProjectURL,
				&meta.DirFmt, &meta.FileFmt, &meta.LineFmt); err != nil {
				return err
			}
			meta.ProjectRoot = modulePath
			ok = true
		}
		return rows.Err()
	})
	return
}

const insertGosource = `
INSERT INTO gosource (
	project_root, project_name, project_url, dir_fmt, file_fmt, line_fmt
) VALUES (
	$1, $2, $3, $4, $5, $6
) ON CONFLICT (project_root) DO
UPDATE SET project_name = $2, project_url = $3,
dir_fmt = $4, file_fmt = $5, line_fmt = $6;
`

// PutMeta puts go-source meta tag information in the database.
func (db *Database) PutMeta(ctx context.Context, meta source.Meta) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertGosource,
			meta.ProjectRoot, meta.ProjectName, meta.ProjectURL,
			meta.DirFmt, meta.FileFmt, meta.LineFmt)
		if err != nil {
			return err
		}
		return nil
	})
}

// Oldest returns the module path of the oldest module in the database
// (i.e., the module with the smallest updated timestamp).
func (db *Database) Oldest(ctx context.Context) (string, error) {
	var modulePath string
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT module_path FROM modules ORDER BY updated LIMIT 1;`)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&modulePath); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return "", err
	}
	return modulePath, nil
}

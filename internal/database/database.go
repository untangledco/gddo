// Package database manages the storage of documentation.
package database

// See schema.sql for the database schema.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"path"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/meta"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"github.com/lib/pq"
)

// Package contains package information.
type Package struct {
	internal.Module
	ImportPath string
	Imports    []string
	Name       string
	Synopsis   string
	Updated    time.Time
}

// Documentation contains package documentation.
type Documentation struct {
	Filenames []string
	Notes     map[string][]*doc.Note
	Doc       string
	Truncated bool
	Consts    []*doc.Value
	Types     []*doc.Type
	Vars      []*doc.Value
	Funcs     []*doc.Func
	Examples  []*doc.Example
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

const insertModule = `
INSERT INTO modules (
	module_path, series_path, latest_version, versions, updated
) VALUES ( $1, $2, $3, $4, NOW() )
ON CONFLICT (module_path) DO
UPDATE SET series_path = $2, latest_version = $3, versions = $4, updated = NOW();
`

// PutModule stores the module information in the database.
func (db *Database) PutModule(ctx context.Context, mod *internal.Module) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertModule,
			mod.ModulePath, mod.SeriesPath, mod.LatestVersion,
			pq.StringArray(mod.Versions))
		if err != nil {
			return err
		}
		return nil
	})
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
WHERE p.searchtext @@ websearch_to_tsquery('english', $2)
	AND p.platform = $1
	AND m.module_path = p.module_path AND p.version = m.latest_version
ORDER BY ts_rank(p.searchtext, websearch_to_tsquery('english', $2)) DESC,
	p.score DESC
LIMIT 20;
`

// Search performs a search with the provided query string.
func (db *Database) Search(ctx context.Context, platform, query string) ([]Package, error) {
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, searchQuery, platform, query)
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

const packageQuery = `
SELECT
	p.module_path, p.series_path, p.version, p.commit_time, m.latest_version, m.versions,
	p.imports, p.name, p.synopsis, m.updated
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND p.version = $3 AND m.module_path = p.module_path;
`

const packageLatestQuery = `
SELECT
	p.module_path, p.series_path, p.version, p.commit_time, m.latest_version, m.versions,
	p.imports, p.name, p.synopsis, m.updated
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

// GetPackage returns information about the given package.
func (db *Database) GetPackage(ctx context.Context, platform, importPath, version string) (Package, bool, error) {
	var pkg Package
	ok := false
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		var rows *sql.Rows
		var err error
		if version == "latest" {
			rows, err = tx.QueryContext(ctx, packageLatestQuery, platform, importPath)
		} else {
			rows, err = tx.QueryContext(ctx, packageQuery, platform, importPath, version)
		}
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&pkg.ModulePath, &pkg.SeriesPath, &pkg.Version,
				&pkg.CommitTime, &pkg.LatestVersion, (*pq.StringArray)(&pkg.Versions),
				(*pq.StringArray)(&pkg.Imports), &pkg.Name, &pkg.Synopsis, &pkg.Updated); err != nil {
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
SELECT documentation FROM packages
WHERE platform = $1 AND import_path = $2 AND version = $3;
`

// GetDocumentation retrieves the documentation for the given package.
func (db *Database) GetDocumentation(ctx context.Context, platform, importPath, version string) (Documentation, error) {
	var doc Documentation
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, documentationQuery,
			platform, importPath, version)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			var p []byte
			if err := rows.Scan(&p); err != nil {
				return err
			}
			if len(p) == 0 {
				// No documentation
				return nil
			}
			if err := gob.NewDecoder(bytes.NewReader(p)).Decode(&doc); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return Documentation{}, err
	}
	return doc, nil
}

const insertPackage = `
INSERT INTO packages (
	platform, import_path, module_path, series_path, version, commit_time, imports, name, synopsis, score, documentation
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
);
`

// AddPackage adds the package to the database. pkg may be nil.
func (db *Database) AddPackage(ctx context.Context,
	platform, importPath, modulePath, seriesPath, version string,
	commitTime time.Time, pkg *doc.Package) error {

	imports := []string{}
	var name, synopsis string
	var documentation []byte
	var score float64
	if pkg != nil {
		imports = pkg.Imports
		name, synopsis = pkg.Name, pkg.Synopsis

		doc := Documentation{
			Filenames: pkg.Filenames,
			Notes:     pkg.Notes,
			Doc:       pkg.Doc,
			Consts:    pkg.Consts,
			Types:     pkg.Types,
			Vars:      pkg.Vars,
			Funcs:     pkg.Funcs,
			Examples:  pkg.Examples,
		}

		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(doc); err != nil {
			return err
		}

		// Truncate large documents.
		if len(buf.Bytes()) > 1200000 {
			doc.Truncated = true
			doc.Consts = nil
			doc.Types = nil
			doc.Vars = nil
			doc.Funcs = nil
			doc.Examples = nil
			buf.Reset()
			if err := gob.NewEncoder(&buf).Encode(doc); err != nil {
				return err
			}
		}

		documentation = buf.Bytes()
		score = searchScore(importPath, pkg)
	}

	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertPackage,
			platform, importPath, modulePath, seriesPath, version, commitTime,
			pq.StringArray(imports), name, synopsis, score, documentation)
		if err != nil {
			return err
		}
		return nil
	})
}

// searchScore calculates the search score for the provided package documentation.
func searchScore(importPath string, pkg *doc.Package) float64 {
	// Ignore internal packages
	if pkg.Name == "" ||
		strings.HasSuffix(importPath, "/internal") ||
		strings.Contains(importPath, "/internal/") {
		return 0
	}

	r := 1.0
	if pkg.Name == "main" {
		if pkg.Doc == "" {
			// Do not include command in index if it does not have documentation.
			return 0
		}
	} else {
		if len(pkg.Consts) == 0 &&
			len(pkg.Vars) == 0 &&
			len(pkg.Funcs) == 0 &&
			len(pkg.Types) == 0 &&
			len(pkg.Examples) == 0 {
			// Do not include package in index if it does not have exports.
			return 0
		}
		if pkg.Doc == "" {
			// Penalty for no documentation.
			r *= 0.95
		}
	}
	return r
}

// HasPackage reports whether the given package is present in the database.
func (db *Database) HasPackage(ctx context.Context, platform, importPath, version string) (bool, error) {
	exists := false
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT EXISTS (SELECT FROM packages WHERE platform = $1 AND import_path = $2 AND version = $3);`,
			platform, importPath, version)
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

const synopsisQuery = `
SELECT p.synopsis
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

func (db *Database) synopsis(ctx context.Context, platform, importPath string) (string, error) {
	var synopsis string
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, synopsisQuery, platform, importPath)
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
func (db *Database) Packages(ctx context.Context, platform string, importPaths []string) ([]Package, error) {
	var packages []Package
	for _, importPath := range importPaths {
		synopsis, err := db.synopsis(ctx, platform, importPath)
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
WHERE platform = $1 AND module_path = $2 AND version = $3 AND import_path LIKE $4 || '_%'
AND ($5 OR import_path NOT LIKE '%/internal/%')
ORDER BY import_path;
`

// SubPackages returns the subpackages of the given package.
func (db *Database) SubPackages(ctx context.Context, platform, modulePath, version, importPath string) ([]Package, error) {
	internal := strings.HasSuffix(importPath, "/internal") ||
		strings.Contains(importPath, "/internal/")
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, subpackagesQuery,
			platform, modulePath, version, importPath, internal)
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

func (db *Database) imports(ctx context.Context, platform, importPath, version string) ([]string, error) {
	var imports []string
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT imports FROM packages WHERE platform = $1 AND import_path = $2 AND version = $3;`,
			platform, importPath, version)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan((*pq.StringArray)(&imports)); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return imports, nil
}

const importGraphPackageQuery = `
SELECT p.version, p.synopsis
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

func (db *Database) importGraphPackage(ctx context.Context, platform, importPath string) (Package, error) {
	pkg := Package{
		ImportPath: importPath,
	}
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, importGraphPackageQuery, platform, importPath)
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
func (db *Database) ImportGraph(ctx context.Context, platform string, pkg Package, level DepLevel) ([]Package, [][2]int, error) {

	var queue []Package
	nodes := []Package{{ImportPath: pkg.ImportPath, Synopsis: pkg.Synopsis}}
	edges := [][2]int{}
	index := map[string]int{pkg.ImportPath: 0}

	imports, err := db.imports(ctx, platform, pkg.ImportPath, pkg.Version)
	if err != nil {
		return nil, nil, err
	}

	for _, importPath := range imports {
		if level >= HideStandardAll && stdlib.Contains(importPath) {
			continue
		}
		j := len(nodes)
		index[importPath] = j
		edges = append(edges, [2]int{0, j})
		pkg, err := db.importGraphPackage(ctx, platform, importPath)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, pkg)
		queue = append(queue, pkg)
	}

	for i := 1; i < len(nodes); i++ {
		var pkg Package
		pkg, queue = queue[0], queue[1:]
		imports, err := db.imports(ctx, platform, pkg.ImportPath, pkg.Version)
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
				pkg, err := db.importGraphPackage(ctx, platform, importPath)
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
func (db *Database) GetMeta(ctx context.Context, modulePath string) (meta meta.Meta, ok bool, err error) {
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
func (db *Database) PutMeta(ctx context.Context, meta meta.Meta) error {
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
func (db *Database) Oldest(ctx context.Context) (string, time.Time, error) {
	var modulePath string
	var timestamp time.Time
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT module_path, updated FROM modules ORDER BY updated LIMIT 1;`)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&modulePath, &timestamp); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return modulePath, timestamp, nil
}

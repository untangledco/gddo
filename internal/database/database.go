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
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
)

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
	db.SetMaxOpenConns(64)
	return &Database{pg: db}, nil
}

func (db *Database) RegisterMetrics(r prometheus.Registerer) error {
	return r.Register(promcollectors.NewDBStatsCollector(db.pg, "main"))
}

func (db *Database) WithTx(ctx context.Context, opts *sql.TxOptions,
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

// Modules returns the number of modules in the database.
func (db *Database) Modules(ctx context.Context) (int64, error) {
	var count int64
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM modules;")
		if err := row.Scan(&count); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

const insertModule = `
INSERT INTO modules (
	module_path, series_path, latest_version, versions, deprecated, updated
) VALUES ( $1, $2, $3, $4, $5, NOW() )
ON CONFLICT (module_path) DO
UPDATE SET series_path = $2, latest_version = $3, versions = $4, deprecated = $5, updated = NOW();
`

// PutModule stores the module in the database.
func (db *Database) PutModule(tx *sql.Tx, mod *internal.Module) error {
	_, err := tx.Exec(insertModule,
		mod.ModulePath, mod.SeriesPath, mod.LatestVersion,
		pq.StringArray(mod.Versions), mod.Deprecated)
	if err != nil {
		return err
	}
	return nil
}

// TouchModule updates the module's updated timestamp.
// If the module does not exist, TouchModule does nothing.
func (db *Database) TouchModule(ctx context.Context, modulePath string) error {
	return db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE modules SET updated = NOW() WHERE module_path = $1;`, modulePath)
		if err != nil {
			return err
		}
		return nil
	})
}

const searchQuery = `
SELECT
	p.import_path, p.module_path, p.series_path, p.version, p.reference,
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
func (db *Database) Search(ctx context.Context, platform, query string) ([]internal.Package, error) {
	var packages []internal.Package
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, searchQuery, platform, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var pkg internal.Package
			if err := rows.Scan(&pkg.ImportPath, &pkg.ModulePath, &pkg.SeriesPath,
				&pkg.Version, &pkg.Reference, &pkg.CommitTime, &pkg.Name,
				&pkg.Synopsis); err != nil {
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
	p.module_path, p.series_path, p.version, p.reference, p.commit_time,
	m.latest_version, m.versions, m.deprecated, p.imports, p.name, p.synopsis,
	m.updated
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND p.version = $3
	AND m.module_path = p.module_path;
`

const packageLatestQuery = `
SELECT
	p.module_path, p.series_path, p.version, p.reference, p.commit_time,
	m.latest_version, m.versions, m.deprecated, p.imports, p.name, p.synopsis,
	m.updated
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path
	AND p.version = m.latest_version;
`

// Package returns information about the given package.
func (db *Database) Package(ctx context.Context, platform, importPath, version string) (internal.Package, bool, error) {
	var pkg internal.Package
	ok := false
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var rows *sql.Rows
		var err error
		if version == internal.LatestVersion {
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
				&pkg.Reference, &pkg.CommitTime, &pkg.LatestVersion,
				(*pq.StringArray)(&pkg.Versions), &pkg.Deprecated,
				(*pq.StringArray)(&pkg.Imports), &pkg.Name, &pkg.Synopsis,
				&pkg.Updated); err != nil {
				return err
			}
			pkg.ImportPath = importPath
			ok = true
			if pkg.ImportPath != pkg.ModulePath {
				i := 0
				for j := 0; j < len(pkg.Versions); j++ {
					if ok, err := db.HasPackage(ctx, platform, pkg.ImportPath, pkg.Versions[j]); err != nil {
						return err
					} else if !ok {
						continue
					}
					pkg.Versions[i] = pkg.Versions[j]
					i++
				}
				pkg.Versions = pkg.Versions[:i]
			}
		}
		return rows.Err()
	})
	if err != nil {
		return internal.Package{}, false, err
	}
	return pkg, ok, nil
}

const documentationQuery = `
SELECT documentation FROM packages
WHERE platform = $1 AND import_path = $2 AND version = $3;
`

// Documentation retrieves the documentation for the given package.
func (db *Database) Documentation(ctx context.Context, platform, importPath, version string) (doc.Documentation, error) {
	var pkg doc.Documentation
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
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
			if err := gob.NewDecoder(bytes.NewReader(p)).Decode(&pkg); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return doc.Documentation{}, err
	}
	return pkg, nil
}

const insertPackage = `
INSERT INTO packages (
	platform, import_path, module_path, series_path, version, reference,
	commit_time, imports, name, synopsis, score, documentation
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
);
`

// PutPackage stores the package in the database. doc may be nil.
func (db *Database) PutPackage(tx *sql.Tx, platform string, pkg internal.Package,
	doc *doc.Documentation) error {
	var documentation []byte
	var score float64
	if doc != nil {
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
		score = searchScore(pkg.ImportPath, pkg.Name, doc)
	}

	_, err := tx.Exec(insertPackage,
		platform, pkg.ImportPath, pkg.ModulePath, pkg.SeriesPath, pkg.Version,
		pkg.Reference, pkg.CommitTime, pq.StringArray(pkg.Imports), pkg.Name,
		pkg.Synopsis, score, documentation)
	if err != nil {
		return err
	}
	return nil
}

// searchScore calculates the search score for the provided package documentation.
func searchScore(importPath, name string, doc *doc.Documentation) float64 {
	// Ignore internal packages
	if name == "" ||
		strings.HasSuffix(importPath, "/internal") ||
		strings.Contains(importPath, "/internal/") {
		return 0
	}

	r := 1.0
	if name == "main" {
		if doc.Doc == "" {
			// Do not include command in index if it does not have documentation.
			return 0
		}
	} else {
		if len(doc.Consts) == 0 &&
			len(doc.Vars) == 0 &&
			len(doc.Funcs) == 0 &&
			len(doc.Types) == 0 &&
			len(doc.Examples) == 0 {
			// Do not include package in index if it does not have exports.
			return 0
		}
		if doc.Doc == "" {
			// Penalty for no documentation.
			r *= 0.95
		}
	}
	return r
}

// HasPackage reports whether the given package is present in the database.
func (db *Database) HasPackage(ctx context.Context, platform, importPath, version string) (bool, error) {
	exists := false
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT EXISTS (SELECT FROM packages WHERE platform = $1 AND import_path = $2 AND version = $3);`,
			platform, importPath, version)
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

// IsBlocked returns whether the package is blocked or belongs to a blocked
// domain/repo.
func (db *Database) IsBlocked(ctx context.Context, importPath string) (bool, error) {
	parts := strings.Split(importPath, "/")
	importPath = ""
	for _, part := range parts {
		importPath = path.Join(importPath, part)
		var blocked bool
		err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
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
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
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
func (db *Database) Packages(ctx context.Context, platform string, importPaths []string) ([]internal.Package, error) {
	var packages []internal.Package
	for _, importPath := range importPaths {
		synopsis, err := db.synopsis(ctx, platform, importPath)
		if err != nil {
			return nil, err
		}
		packages = append(packages, internal.Package{
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
WHERE platform = $1 AND module_path = $2 AND version = $3
AND (($4 AND import_path != module_path) OR import_path LIKE replace($5, '_', '\_') || '/%')
AND ($6 OR import_path NOT SIMILAR TO '(%/)?internal/%')
ORDER BY import_path;
`

// SubPackages returns the subpackages of the given package.
func (db *Database) SubPackages(ctx context.Context, platform, modulePath, version, importPath string) ([]internal.Package, error) {
	isModule := modulePath == importPath
	isInternal := importPath == "internal" ||
		strings.HasSuffix(importPath, "/internal") ||
		strings.Contains(importPath, "/internal/")
	var packages []internal.Package
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, subpackagesQuery,
			platform, modulePath, version, isModule, importPath, isInternal)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var pkg internal.Package
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

func (db *Database) imports(tx *sql.Tx, platform, importPath, version string) ([]string, error) {
	var imports []string
	rows, err := tx.Query(
		`SELECT imports FROM packages WHERE platform = $1 AND import_path = $2 AND version = $3;`,
		platform, importPath, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan((*pq.StringArray)(&imports)); err != nil {
			return nil, err
		}
	}
	if rows.Err() != nil {
		return nil, err
	}
	return imports, nil
}

const importGraphPackageQuery = `
SELECT p.version, p.synopsis
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

func (db *Database) importGraphPackage(tx *sql.Tx, platform, importPath string) (internal.Package, error) {
	pkg := internal.Package{
		ImportPath: importPath,
	}

	rows, err := tx.Query(importGraphPackageQuery, platform, importPath)
	if err != nil {
		return internal.Package{}, err
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&pkg.Version, &pkg.Synopsis); err != nil {
			return internal.Package{}, err
		}
	}
	if rows.Err() != nil {
		return internal.Package{}, err
	}
	return pkg, nil
}

// ImportGraph performs a breadth-first traversal of the package's dependencies.
func (db *Database) ImportGraph(ctx context.Context, platform string, pkg internal.Package, level DepLevel) ([]internal.Package, [][2]int, error) {
	opts := &sql.TxOptions{
		ReadOnly: true,
	}
	var nodes []internal.Package
	var edges [][2]int
	err := db.WithTx(ctx, opts, func(tx *sql.Tx) error {
		var err error
		nodes, edges, err = db.importGraph(tx, platform, pkg, level)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

func (db *Database) importGraph(tx *sql.Tx, platform string, pkg internal.Package, level DepLevel) ([]internal.Package, [][2]int, error) {
	var queue []internal.Package
	nodes := []internal.Package{{ImportPath: pkg.ImportPath, Synopsis: pkg.Synopsis}}
	edges := [][2]int{}
	index := map[string]int{pkg.ImportPath: 0}

	imports, err := db.imports(tx, platform, pkg.ImportPath, pkg.Version)
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
		pkg, err := db.importGraphPackage(tx, platform, importPath)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, pkg)
		queue = append(queue, pkg)
	}

	for i := 1; i < len(nodes); i++ {
		var pkg internal.Package
		pkg, queue = queue[0], queue[1:]
		imports, err := db.imports(tx, platform, pkg.ImportPath, pkg.Version)
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
				pkg, err := db.importGraphPackage(tx, platform, importPath)
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

const projectQuery = `
SELECT name, url, dir_fmt, file_fmt, line_fmt
FROM projects
WHERE module_path = $1;
`

// Project returns information about the project associated with the given module.
func (db *Database) Project(ctx context.Context, modulePath string) (project meta.Project, ok bool, err error) {
	err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, projectQuery, modulePath)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&project.Name, &project.URL, &project.DirFmt,
				&project.FileFmt, &project.LineFmt); err != nil {
				return err
			}
			project.ModulePath = modulePath
			ok = true
		}
		return rows.Err()
	})
	return
}

const insertProject = `
INSERT INTO projects (
	module_path, name, url, dir_fmt, file_fmt, line_fmt
) VALUES (
	$1, $2, $3, $4, $5, $6
) ON CONFLICT (module_path) DO
UPDATE SET name = $2, url = $3,
dir_fmt = $4, file_fmt = $5, line_fmt = $6;
`

// PutProject puts project information in the database.
func (db *Database) PutProject(tx *sql.Tx, project meta.Project) error {
	_, err := tx.Exec(insertProject,
		project.ModulePath, project.Name, project.URL,
		project.DirFmt, project.FileFmt, project.LineFmt)
	if err != nil {
		return err
	}
	return nil
}

// Oldest returns the module path of the oldest module in the database
// (i.e., the module with the smallest updated timestamp).
func (db *Database) Oldest(ctx context.Context) (string, time.Time, error) {
	var modulePath string
	var timestamp time.Time
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
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

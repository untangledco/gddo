//go:generate go run gen_ast.go

// Package database manages the storage of documentation.
package database

// See schema.sql for the database schema.

import (
	"context"
	"database/sql"
	"errors"
	"go/doc"
	"path"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/meta"
	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
)

// Package contains package information.
type Package struct {
	internal.Module
	ImportPath string
	Imports    []string
	Name       string
	Synopsis   string
}

// Database stores package documentation.
type Database struct {
	pg *sql.DB

	countModules     *sql.Stmt
	insertModule     *sql.Stmt
	touchModule      *sql.Stmt
	searchQuery      *sql.Stmt
	packageQuery     *sql.Stmt
	latestQuery      *sql.Stmt
	insertPackage    *sql.Stmt
	packageExists    *sql.Stmt
	blockExists      *sql.Stmt
	synopsisQuery    *sql.Stmt
	subpackagesQuery *sql.Stmt
	projectQuery     *sql.Stmt
	projectUpdated   *sql.Stmt
	insertProject    *sql.Stmt
	oldestModule     *sql.Stmt
}

// New creates a new database. serverURI is the postgres URI.
func New(serverURI string) (*Database, error) {
	pg, err := sql.Open("postgres", serverURI)
	if err != nil {
		return nil, err
	}
	pg.SetMaxOpenConns(64)

	db := &Database{pg: pg}
	if err := db.prepare(); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *Database) prepare() error {
	var err error
	db.countModules, err = db.pg.Prepare(countModules)
	if err != nil {
		return err
	}
	db.insertModule, err = db.pg.Prepare(insertModule)
	if err != nil {
		return err
	}
	db.touchModule, err = db.pg.Prepare(touchModule)
	if err != nil {
		return err
	}
	db.searchQuery, err = db.pg.Prepare(searchQuery)
	if err != nil {
		return err
	}
	db.packageQuery, err = db.pg.Prepare(packageQuery)
	if err != nil {
		return err
	}
	db.latestQuery, err = db.pg.Prepare(latestQuery)
	if err != nil {
		return err
	}
	db.insertPackage, err = db.pg.Prepare(insertPackage)
	if err != nil {
		return err
	}
	db.packageExists, err = db.pg.Prepare(packageExists)
	if err != nil {
		return err
	}
	db.blockExists, err = db.pg.Prepare(blockExists)
	if err != nil {
		return err
	}
	db.synopsisQuery, err = db.pg.Prepare(synopsisQuery)
	if err != nil {
		return err
	}
	db.subpackagesQuery, err = db.pg.Prepare(subpackagesQuery)
	if err != nil {
		return err
	}
	db.projectQuery, err = db.pg.Prepare(projectQuery)
	if err != nil {
		return err
	}
	db.projectUpdated, err = db.pg.Prepare(projectUpdated)
	if err != nil {
		return err
	}
	db.insertProject, err = db.pg.Prepare(insertProject)
	if err != nil {
		return err
	}
	db.oldestModule, err = db.pg.Prepare(oldestModule)
	if err != nil {
		return err
	}
	return nil
}

func (db *Database) RegisterMetrics(r prometheus.Registerer) error {
	return r.Register(promcollectors.NewDBStatsCollector(db.pg, "main"))
}

func (db *Database) WithTx(ctx context.Context, opts *sql.TxOptions,
	fn func(tx *sql.Tx) error) error {

	tx, err := db.pg.BeginTx(ctx, opts)
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

const countModules = `SELECT COUNT(*) FROM modules;`

// Modules returns the number of modules in the database.
func (db *Database) Modules(ctx context.Context) (int64, error) {
	var count int64
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		row := tx.Stmt(db.countModules).QueryRow()
		if err := row.Scan(&count); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

const insertModule = `
INSERT INTO modules (
	module_path, series_path, latest_version, versions, deprecated, updated
) VALUES (
	$1, $2, $3, $4, $5, NOW()
) ON CONFLICT (module_path) DO
UPDATE SET series_path = $2, latest_version = $3, versions = $4, deprecated = $5, updated = NOW();
`

// PutModule stores the module in the database.
func (db *Database) PutModule(ctx context.Context, mod *internal.Module) error {
	return db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.Stmt(db.insertModule).Exec(
			mod.ModulePath, mod.SeriesPath, mod.LatestVersion,
			pq.StringArray(mod.Versions), mod.Deprecated)
		if err != nil {
			return err
		}
		return nil
	})
}

const touchModule = `UPDATE modules SET updated = NOW() WHERE module_path = $1;`

// TouchModule updates the module's updated timestamp.
// If the module does not exist, TouchModule does nothing.
func (db *Database) TouchModule(ctx context.Context, modulePath string) error {
	return db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.Stmt(db.touchModule).Exec(modulePath)
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
func (db *Database) Search(ctx context.Context, platform, query string) ([]Package, error) {
	var packages []Package
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.Stmt(db.searchQuery).Query(platform, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var pkg Package
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

const latestQuery = `
SELECT
	p.module_path, p.series_path, p.version, p.reference, p.commit_time,
	m.latest_version, m.versions, m.deprecated, m.updated, p.source
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path
	AND p.version = m.latest_version;
`

// Package returns information for the given package. It may return nil if no such package was found.
func (db *Database) Package(ctx context.Context, platform, importPath, version string) (*internal.Module, *internal.Package, error) {
	var mod internal.Module
	var source []byte
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var row *sql.Row
		if version == internal.LatestVersion {
			row = tx.Stmt(db.latestQuery).QueryRow(platform, importPath)
		} else {
			row = tx.Stmt(db.packageQuery).QueryRow(platform, importPath, version)
		}

		if err := row.Scan(&mod.ModulePath, &mod.SeriesPath, &mod.Version,
			&mod.Reference, &mod.CommitTime, &mod.LatestVersion,
			(*pq.StringArray)(&mod.Versions), &mod.Deprecated, &mod.Updated,
			&source); err != nil {
			return err
		}
		if importPath != mod.ModulePath {
			// Filter available versions
			stmt := tx.Stmt(db.packageExists)
			i := 0
			for j := 0; j < len(mod.Versions); j++ {
				exists := false
				row := stmt.QueryRow(platform, importPath, mod.Versions[j])
				if err := row.Scan(&exists); err != nil {
					return err
				}
				if !exists {
					continue
				}
				mod.Versions[i] = mod.Versions[j]
				i++
			}
			mod.Versions = mod.Versions[:i]
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	pkg, err := internal.FastDecodePackage(source)
	if err != nil {
		return nil, nil, err
	}
	return &mod, pkg, nil
}

const insertPackage = `
INSERT INTO packages (
	platform, import_path, module_path, series_path, version, reference,
	commit_time, imports, name, synopsis, score, source
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
);
`

// PutPackage stores the package in the database. doc may be nil.
func (db *Database) PutPackage(tx *sql.Tx, platform string, mod *internal.Module, pkg *doc.Package, source []byte) error {
	synopsis := pkg.Synopsis(pkg.Doc)
	score := searchScore(pkg)

	_, err := tx.Stmt(db.insertPackage).Exec(
		platform, pkg.ImportPath, mod.ModulePath, mod.SeriesPath, mod.Version,
		mod.Reference, mod.CommitTime, pq.StringArray(pkg.Imports), pkg.Name,
		synopsis, score, source)
	if err != nil {
		return err
	}
	return nil
}

// searchScore calculates the search score for the provided package documentation.
func searchScore(pkg *doc.Package) float64 {
	// Ignore internal packages
	if pkg.Name == "" ||
		strings.HasSuffix(pkg.ImportPath, "/internal") ||
		strings.Contains(pkg.ImportPath, "/internal/") {
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

const packageExists = `SELECT EXISTS (SELECT FROM packages WHERE platform = $1 AND import_path = $2 AND version = $3);`

// HasPackage reports whether the given package is present in the database.
func (db *Database) HasPackage(ctx context.Context, platform, importPath, version string) (bool, error) {
	exists := false
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		row := tx.Stmt(db.packageExists).QueryRow(platform, importPath, version)
		if err := row.Scan(&exists); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return exists, nil
}

const blockExists = `SELECT EXISTS (SELECT FROM blocklist WHERE import_path = $1);`

// IsBlocked returns whether the package is blocked or belongs to a blocked
// domain/repo.
func (db *Database) IsBlocked(ctx context.Context, importPath string) (bool, error) {
	parts := strings.Split(importPath, "/")
	blocked := false
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		stmt := tx.Stmt(db.blockExists)
		importPath := ""
		for _, part := range parts {
			importPath = path.Join(importPath, part)
			row := stmt.QueryRow(importPath)
			if err := row.Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				break
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return blocked, nil
}

const synopsisQuery = `
SELECT p.synopsis
FROM packages p, modules m
WHERE p.platform = $1 AND p.import_path = $2 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

// Packages returns a list of package information for the given import paths.
// Only the ImportPath and Synopsis fields will be populated.
func (db *Database) Packages(ctx context.Context, platform string, importPaths []string) ([]Package, error) {
	var packages []Package
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		stmt := tx.Stmt(db.synopsisQuery)
		for _, importPath := range importPaths {
			var synopsis string
			row := stmt.QueryRow(platform, importPath)
			err := row.Scan(&synopsis)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			packages = append(packages, Package{
				ImportPath: importPath,
				Synopsis:   synopsis,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
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
func (db *Database) SubPackages(ctx context.Context, platform, modulePath, version, importPath string) ([]Package, error) {
	isModule := modulePath == importPath
	isInternal := importPath == "internal" ||
		strings.HasSuffix(importPath, "/internal") ||
		strings.Contains(importPath, "/internal/")
	var packages []Package
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.Stmt(db.subpackagesQuery).Query(
			platform, modulePath, version, isModule, importPath, isInternal)
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

const projectQuery = `
SELECT name, url, dir_fmt, file_fmt, line_fmt
FROM projects
WHERE module_path = $1;
`

// Project returns information about the project associated with the given module.
// It may return nil if no project exists.
func (db *Database) Project(ctx context.Context, modulePath string) (*meta.Project, error) {
	var project meta.Project
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		row := tx.Stmt(db.projectQuery).QueryRow(modulePath)
		if err := row.Scan(&project.Name, &project.URL, &project.DirFmt,
			&project.FileFmt, &project.LineFmt); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &project, nil
}

const projectUpdated = `SELECT updated FROM projects WHERE module_path = $1;`

// ProjectUpdated returns the last time the project was updated.
// If no project exists, it returns the zero timestamp.
func (db *Database) ProjectUpdated(ctx context.Context, modulePath string) (time.Time, error) {
	var updated time.Time
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		row := tx.Stmt(db.projectUpdated).QueryRow(modulePath)
		if err := row.Scan(&updated); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return updated, nil
}

const insertProject = `
INSERT INTO projects (
	module_path, name, url, dir_fmt, file_fmt, line_fmt, updated
) VALUES (
	$1, $2, $3, $4, $5, $6, NOW()
) ON CONFLICT (module_path) DO
UPDATE SET name = $2, url = $3, dir_fmt = $4, file_fmt = $5, line_fmt = $6, updated = NOW();
`

// PutProject puts project information in the database.
func (db *Database) PutProject(ctx context.Context, modulePath string, project *meta.Project) error {
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.Stmt(db.insertProject).Exec(
			modulePath, project.Name, project.URL,
			project.DirFmt, project.FileFmt, project.LineFmt)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

const oldestModule = `SELECT module_path, updated FROM modules ORDER BY updated LIMIT 1;`

// Oldest returns the module path of the oldest module in the database
// (i.e., the module with the smallest updated timestamp).
func (db *Database) Oldest(ctx context.Context) (string, time.Time, error) {
	var modulePath string
	var timestamp time.Time
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.Stmt(db.oldestModule).Query()
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

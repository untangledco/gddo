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

	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"github.com/lib/pq"
)

// Module represents a module.
type Module struct {
	ModulePath string    // module path
	SeriesPath string    // series path
	Version    string    // latest version
	Versions   []string  // all versions
	Updated    time.Time // last update time
}

// Package represents a package.
type Package struct {
	ImportPath  string    `json:"import_path"`
	ModulePath  string    `json:"module_path"`
	SeriesPath  string    `json:"-"`
	Version     string    `json:"version"`
	CommitTime  time.Time `json:"commit_time"`
	Name        string    `json:"name"`
	Synopsis    string    `json:"synopsis"`
	ImportCount int       `json:"import_count"`
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

// GetModule returns information about the module with the given path.
func (db *Database) GetModule(ctx context.Context, modulePath string) (mod Module, ok bool, err error) {
	err = db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.Query(
			`SELECT series_path, latest_version, versions, updated FROM modules WHERE module_path = $1;`,
			modulePath)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&mod.SeriesPath, &mod.Version,
				(*pq.StringArray)(&mod.Versions), &mod.Updated); err != nil {
				return err
			}
			mod.ModulePath = modulePath
			ok = true
		}
		return rows.Err()
	})
	return
}

// Delete deletes the module with the given path.
func (db *Database) Delete(ctx context.Context, path string) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM modules WHERE module_path = $1;`, path)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM packages WHERE module_path = $1;`, path)
		if err != nil {
			return err
		}
		return nil
	})
}

// Search performs a search with the provided query string.
func (db *Database) Search(ctx context.Context, q string) ([]Package, error) {
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT
				p.import_path, p.module_path, p.series_path, p.version, p.commit_time, p.name, p.synopsis, i.import_count
			FROM packages p, modules m, import_counts i
			WHERE p.searchtext @@ websearch_to_tsquery('english', $1)
				AND m.module_path = p.module_path AND p.version = m.latest_version
				AND i.import_path = p.import_path
			ORDER BY ts_rank(p.searchtext, websearch_to_tsquery('english', $1)) DESC,
				p.score DESC
			LIMIT 20;
			`, q)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var pkg Package
			if err := rows.Scan(&pkg.ImportPath, &pkg.ModulePath, &pkg.SeriesPath,
				&pkg.Version, &pkg.CommitTime, &pkg.Name, &pkg.Synopsis,
				&pkg.ImportCount); err != nil {
				return err
			}
			packages = append(packages, pkg)
		}
		return rows.Err()
	})
	return packages, err
}

// PutModule updates the module in the database.
func (db *Database) PutModule(ctx context.Context, mod Module) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO modules (
				module_path, series_path, latest_version, versions, updated
			) VALUES ( $1, $2, $3, $4, $5 )
			ON CONFLICT (module_path) DO
			UPDATE SET series_path = $2, latest_version = $3, versions = $4, updated = $5;
			`, mod.ModulePath, mod.SeriesPath, mod.Version, pq.StringArray(mod.Versions), mod.Updated)
		if err != nil {
			return err
		}
		return nil
	})
}

const selectPackage = `
SELECT
	module_path, series_path, version, commit_time, name, synopsis
FROM packages WHERE import_path = $1 AND version = $2;
`

const selectLatestPackage = `
SELECT
	p.module_path, p.series_path, p.version, p.commit_time, p.name, p.synopsis
FROM packages p, modules m
WHERE p.import_path = $1 AND m.module_path = p.module_path AND p.version = m.latest_version;
`

func (db *Database) GetPackage(ctx context.Context, importPath, version string) (pkg Package, ok bool, err error) {
	err = db.withTx(ctx, nil, func(tx *sql.Tx) error {
		var rows *sql.Rows
		var err error
		if version == "latest" {
			rows, err = tx.QueryContext(ctx, selectLatestPackage, importPath)
		} else {
			rows, err = tx.QueryContext(ctx, selectPackage, importPath, version)
		}
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&pkg.ModulePath, &pkg.SeriesPath, &pkg.Version,
				&pkg.CommitTime, &pkg.Name, &pkg.Synopsis); err != nil {
				return err
			}
			pkg.ImportPath = importPath
			ok = true
		}
		return rows.Err()
	})
	return
}

func (db *Database) GetDoc(ctx context.Context, importPath, version, goos, goarch string) (pdoc *doc.Package, ok bool, err error) {
	err = db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT documentation FROM documentation
			WHERE import_path = $1 AND version = $2 AND goos = $3 AND goarch = $4;`,
			importPath, version, goos, goarch)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			var p []byte
			if err := rows.Scan(&p); err != nil {
				return err
			}
			pdoc = new(doc.Package)
			if err := gob.NewDecoder(bytes.NewReader(p)).Decode(&pdoc); err != nil {
				return err
			}
			ok = true
		}
		return rows.Err()
	})
	return
}

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
		_, err := tx.ExecContext(ctx, `
			INSERT INTO packages (
				import_path, module_path, series_path, version, commit_time, name, synopsis, score
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8
			) ON CONFLICT DO NOTHING;
			`, pkg.ImportPath, modulePath, seriesPath, version, commitTime, pkg.Name, pkg.Synopsis, score)
		if err != nil {
			return err
		}

		// Store documentation
		_, err = tx.ExecContext(ctx, `
			INSERT INTO documentation (
				import_path, version, goos, goarch, documentation
			) VALUES (
				$1, $2, $3, $4, $5
			) ON CONFLICT DO NOTHING;
			`, pkg.ImportPath, version, pkg.GOOS, pkg.GOARCH, buf.Bytes())
		if err != nil {
			return err
		}

		// Initialize import count
		_, err = tx.ExecContext(ctx, `
			INSERT INTO import_counts (
				import_path, import_count
			) VALUES ($1, 0) ON CONFLICT DO NOTHING;
		`, pkg.ImportPath)
		if err != nil {
			return err
		}

		for _, importedPath := range pkg.Imports {
			// Update import count
			_, err := tx.ExecContext(ctx, `
				INSERT INTO import_counts (
					import_path, import_count
				) VALUES (
					$1, 1
				)
				ON CONFLICT (import_path) DO
				UPDATE SET import_count = import_counts.import_count+1;
				`, importedPath)
			if err != nil {
				return err
			}

			// Update imports
			_, err = tx.ExecContext(ctx, `
				INSERT INTO imports (
					import_path, version, imported_path
				) VALUES (
					$1, $2, $3
				);
				`, pkg.ImportPath, version, importedPath)
			if err != nil {
				return err
			}

			// Update importers
			_, err = tx.ExecContext(ctx, `
				INSERT INTO importers (import_path, importer_path, importer_version)
				VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;
				`, importedPath, pkg.ImportPath, version)
			if err != nil {
				return err
			}
		}

		return nil
	})
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
		rows, err := tx.QueryContext(ctx, `
			SELECT imported_path FROM imports
			WHERE import_path = $1 AND version = $2;
			`, importPath, version)
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

func (db *Database) Importers(ctx context.Context, importPath string) ([]Package, error) {
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT
				p.import_path, p.module_path, p.series_path, p.version, p.commit_time, p.name, p.synopsis
			FROM packages p, importers i
			WHERE i.import_path = $1 AND
				p.import_path = i.importer_path AND
				p.version = i.importer_version
			ORDER BY p.import_path;
			`, importPath)
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
	if err != nil {
		return nil, err
	}
	return packages, nil
}

func (db *Database) ImportCount(ctx context.Context, importPath string) (int, error) {
	var count int
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT FROM packages p, importers i
			WHERE i.import_path = $1 AND
				p.import_path = i.importer_path AND
				p.version = i.importer_version;
			`, importPath)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			count++
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	return count, nil

}

func (db *Database) Packages(ctx context.Context, importPaths []string) ([]Package, error) {
	var packages []Package
	for _, importPath := range importPaths {
		pkg, ok, err := db.GetPackage(ctx, importPath, "latest")
		if err != nil {
			return nil, err
		}
		if !ok {
			packages = append(packages, Package{
				ImportPath: importPath,
			})
			continue
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func (db *Database) ModulePackages(ctx context.Context, modulePath, version string) ([]Package, error) {
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT
				import_path, series_path, commit_time, name, synopsis
			FROM packages
			WHERE module_path = $1 AND version = $2
			ORDER BY import_path;
			`, modulePath, version)
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

func (db *Database) SubPackages(ctx context.Context, modulePath, version, importPath string) ([]Package, error) {
	var packages []Package
	err := db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT
				import_path, series_path, commit_time, name, synopsis
			FROM packages
			WHERE module_path = $1 AND version = $2 AND import_path LIKE $3 || '_%'
			ORDER BY import_path;
			`, modulePath, version, importPath)
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

// Breadth-first traversal of the package's dependencies.
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
		pkg, ok, err := db.GetPackage(ctx, importPath, "latest")
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			pkg = Package{ImportPath: importPath}
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
				pkg, ok, err := db.GetPackage(ctx, importPath, "latest")
				if err != nil {
					return nil, nil, err
				}
				if !ok {
					pkg = Package{ImportPath: importPath}
				}
				nodes = append(nodes, pkg)
				queue = append(queue, pkg)
			}
			edges = append(edges, [2]int{i, j})
		}
	}
	return nodes, edges, nil
}

func (db *Database) GetMeta(ctx context.Context, projectRoot string) (meta source.Meta, ok bool, err error) {
	err = db.withTx(ctx, nil, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT project_name, project_url, dir_fmt, file_fmt, line_fmt
			FROM gosource
			WHERE project_root = $1;
			`, projectRoot)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			if err := rows.Scan(&meta.ProjectName, &meta.ProjectURL,
				&meta.DirFmt, &meta.FileFmt, &meta.LineFmt); err != nil {
				return err
			}
			meta.ProjectRoot = projectRoot
			ok = true
		}
		return rows.Err()
	})
	return
}

func (db *Database) PutMeta(ctx context.Context, meta source.Meta) error {
	return db.withTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO gosource (
				project_root, project_name, project_url, dir_fmt, file_fmt, line_fmt
			) VALUES (
				$1, $2, $3, $4, $5, $6
			) ON CONFLICT (project_root) DO
			UPDATE SET project_name = $2, project_url = $3,
			dir_fmt = $4, file_fmt = $5, line_fmt = $6;
			`, meta.ProjectRoot, meta.ProjectName, meta.ProjectURL,
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

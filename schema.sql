BEGIN;

-- Stores module information
CREATE TABLE modules (
	module_path text NOT NULL,
	series_path text NOT NULL,
	latest_version text NOT NULL,
	versions text[],
	updated timestamptz NOT NULL,
	PRIMARY KEY (module_path)
);

-- Used for efficient pattern matching of module paths
CREATE INDEX module_path_text_pattern_ops_idx ON modules (module_path text_pattern_ops);

-- Used to retrieve all the modules in a series
CREATE INDEX modules_series_path_idx ON modules (series_path);

-- Stores package information
CREATE TABLE packages (
	import_path text NOT NULL,
	module_path text NOT NULL,
	series_path text NOT NULL,
	version text NOT NULL,
	commit_time timestamptz NOT NULL,
	name text NOT NULL,
	synopsis text NOT NULL,
	score float NOT NULL,
	searchtext tsvector GENERATED ALWAYS AS (
		to_tsvector('english', "name") ||
		to_tsvector('english', coalesce(synopsis, '')) ||
		array_to_tsvector(string_to_array("import_path", '/'))) STORED,
	documentation bytea,
	PRIMARY KEY (import_path, version),
	FOREIGN KEY (module_path) REFERENCES modules (module_path) ON DELETE CASCADE
);

-- Used to speed up retrieval of packages by module path
CREATE INDEX packages_idx ON packages (module_path);

-- Used to search for packages
CREATE INDEX packages_searchtext_idx ON packages USING GIN (searchtext);

-- Used to retrieve all the packages in a series
CREATE INDEX packages_series_path_idx ON packages (series_path);

-- Stores package documentation
CREATE TABLE documentation (
	import_path text NOT NULL,
	version text NOT NULL,
	goos text NOT NULL,
	goarch text NOT NULL,
	documentation bytea,
	PRIMARY KEY (import_path, version, goos, goarch),
	FOREIGN KEY (import_path, version)
		REFERENCES packages (import_path, version) ON DELETE CASCADE
);

-- Stores package imports
CREATE TABLE imports (
	import_path text NOT NULL,
	version text NOT NULL,
	imported_path text NOT NULL,
	PRIMARY KEY (import_path, version, imported_path),
	FOREIGN KEY (import_path, version)
		REFERENCES packages (import_path, version) ON DELETE CASCADE
);

CREATE INDEX imports_idx ON imports (import_path, version);

-- Stores package importers
CREATE TABLE importers (
	import_path text NOT NULL,
	importer_path text NOT NULL,
	importer_version text NOT NULL,
	PRIMARY KEY (import_path, importer_path, importer_version),
	FOREIGN KEY (importer_path, importer_version)
		REFERENCES packages (import_path, version) ON DELETE CASCADE
);

-- Used to speed up retrieving a package's importers.
CREATE INDEX importers_idx ON importers (import_path);

-- Stores blocked import paths
CREATE TABLE blocklist (
	import_path text NOT NULL,
	PRIMARY KEY (import_path)
);

-- Stores go-source meta tag data
CREATE TABLE gosource (
	project_root text NOT NULL,
	project_name text NOT NULL,
	project_url text NOT NULL,
	dir_fmt text NOT NULL,
	file_fmt text NOT NULL,
	line_fmt text NOT NULL,
	PRIMARY KEY (project_root)
);

-- Returns the number of unique importing packages for the given import path
CREATE FUNCTION import_count(import_path text)
RETURNS bigint
LANGUAGE SQL
AS $$
SELECT COUNT(DISTINCT importer_path)
FROM importers i, packages p, modules m
WHERE i.import_path = $1
AND p.import_path = i.importer_path AND p.version = i.importer_version
AND m.module_path = p.module_path AND i.importer_version = m.latest_version;
$$;

COMMIT;

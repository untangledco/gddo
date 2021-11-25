BEGIN;

-- Stores module information
CREATE TABLE modules (
	module_path text NOT NULL,
	series_path text NOT NULL,
	latest_version text NOT NULL,
	versions text[],
	deprecated text NOT NULL,
	updated timestamptz NOT NULL,
	PRIMARY KEY (module_path)
);

-- Used for efficient pattern matching of module paths
CREATE INDEX module_path_text_pattern_ops_idx ON modules (module_path text_pattern_ops);

-- Used to retrieve all the modules in a series
CREATE INDEX modules_series_path_idx ON modules (series_path);

-- Stores package information
CREATE TABLE packages (
	platform text NOT NULL,
	import_path text NOT NULL,
	module_path text NOT NULL,
	series_path text NOT NULL,
	version text NOT NULL,
	commit_time timestamptz NOT NULL,
	name text NOT NULL,
	synopsis text NOT NULL,
	score float NOT NULL,
	imports text[],
	documentation bytea NOT NULL,
	searchtext tsvector GENERATED ALWAYS AS (
		to_tsvector('english', "name") ||
		to_tsvector('english', coalesce(synopsis, '')) ||
		array_to_tsvector(string_to_array("import_path", '/'))) STORED,
	PRIMARY KEY (platform, import_path, version),
	FOREIGN KEY (module_path) REFERENCES modules (module_path) ON DELETE CASCADE
);

-- Used to speed up retrieval of packages by module path
CREATE INDEX packages_idx ON packages (module_path);

-- Used to search for packages
CREATE INDEX packages_searchtext_idx ON packages USING GIN (searchtext);

-- Used to retrieve all the packages in a series
CREATE INDEX packages_series_path_idx ON packages (series_path);

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

COMMIT;

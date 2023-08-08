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

-- Stores package information
CREATE TABLE packages (
	platform text NOT NULL,
	import_path text NOT NULL,
	module_path text NOT NULL,
	series_path text NOT NULL,
	version text NOT NULL,
	reference text NOT NULL,
	commit_time timestamptz NOT NULL,
	name text NOT NULL,
	synopsis text NOT NULL,
	score float NOT NULL,
	imports text[],
	source bytea,
	searchtext tsvector GENERATED ALWAYS AS (
		to_tsvector('english', "name") ||
		to_tsvector('english', coalesce(synopsis, '')) ||
		array_to_tsvector(string_to_array("import_path", '/'))) STORED,
	PRIMARY KEY (platform, import_path, version),
	FOREIGN KEY (module_path) REFERENCES modules (module_path) ON DELETE CASCADE
);

-- Used to speed up retrieval of packages by module path
CREATE INDEX packages_idx ON packages (module_path);

-- Used to speed up retrieval of packages by import path
CREATE INDEX packages_import_path_idx ON packages (import_path);

-- Used to search for packages
CREATE INDEX packages_searchtext_idx ON packages USING GIN (searchtext);

-- Used to store project information
CREATE TABLE projects (
	module_path text NOT NULL,
	summary text NOT NULL,
	dir text NOT NULL,
	file text NOT NULL,
	rawfile text NOT NULL,
	line text NOT NULL,
	updated timestamptz NOT NULL,
	PRIMARY KEY (module_path),
	FOREIGN KEY (module_path) REFERENCES modules (module_path) ON DELETE CASCADE
);

-- Stores blocked import paths
CREATE TABLE blocklist (
	import_path text NOT NULL,
	PRIMARY KEY (import_path)
);

COMMIT;

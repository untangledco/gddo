BEGIN;
CREATE TABLE packages (
	id varchar PRIMARY KEY,
	name varchar NOT NULL,
	path varchar NOT NULL,
	import_count integer NOT NULL,
	synopsis varchar,
	fork bool NOT NULL,
	stars int,
	score int,
	searchtext tsvector GENERATED ALWAYS AS (
		to_tsvector('english', coalesce(synopsis, '')) ||
		to_tsvector('english', "name") ||
		array_to_tsvector(string_to_array("path", '/'))) STORED
);

CREATE INDEX packages_searchtext_idx ON packages USING GIN (searchtext);
COMMIT;

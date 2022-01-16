# gddo

[![Go Documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo?status.svg)](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo)

gddo is a maintained fork of the software that once powered godoc.org, and you
can use it to browse documentation for Go packages.

A hosted instance is available at [godocs.io](https://godocs.io).

## Installation

First install the dependencies:

- Go 1.16 or above

Then compile and install:

	$ make
	# make install

## Running with a database

gddo can optionally be used with a PostgreSQL database. Note that the following
features require a database:

- Package search
- Import graphs

Install the runtime dependencies:

- PostgreSQL 13
- Graphviz (required for import graphs)

Initialize the PostgreSQL database:

	psql -f schema.sql

Then run:

	gddo \
		--db "postgres://localhost" \
		--http :8080 \
		--goproxy "https://proxy.golang.org"

## Running in standalone mode

gddo also supports standalone operation, which is best for viewing documentation
locally. To use it, simply run:

	gddo --http :8080

gddo will then begin serving documentation for modules in your local [Go module
cache](https://go.dev/ref/mod#module-cache). To add a module to the cache, run:

	go mod download example.com/module@latest

See `go help mod download` for more information.

## Gemini support

gddo can also serve documentation over the
[Gemini protocol](https://gemini.circumlunar.space).

To serve documentation over Gemini only, run:

	gddo --gemini :1965 --certs /var/lib/gemini/certs --hostname example.com

See the [documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo) for
more information.

## Questions? Patches?

Send them to the [mailing list](https://lists.sr.ht/~sircmpwn/godocs.io).

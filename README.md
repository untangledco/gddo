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

## Viewing documentation locally

gddo can be used to view documentation locally. Simply run:

	gddo --http :8080

Then navigate your web browser to <http://localhost:8080>.

gddo will look for a Go module in the current directory and serve documentation
for it, allowing you to preview documentation locally. gddo will also serve
documentation for standard library packages in `GOROOT`, as well as modules in
your local [Go module cache]. To add a module to the cache, run:

	go mod download example.com/module@latest

You can then type the import path into the search bar to view its documentation.

See `go help mod download` for more information.

[Go module cache]: https://go.dev/ref/mod#module-cache

## Using a database

gddo can optionally be used with a PostgreSQL database. Note that package search
requires a database.

Install the runtime dependencies:

- PostgreSQL 13

Initialize the PostgreSQL database:

	psql -f schema.sql

Then run:

	gddo --db "postgres://localhost" --http :8080

## Go module proxy

gddo can also fetch and serve documentation from a [Go module proxy]:

	gddo \
		--db "postgres://localhost" \
		--http :8080 \
		--goproxy "https://proxy.golang.org"

When viewing documentation from a Go module proxy, it is recommended to use a
database as well since fetching modules from the proxy can be slow.

[Go module proxy]: https://go.dev/ref/mod#module-proxy

## Gemini support

gddo can also serve documentation over the
[Gemini protocol](https://gemini.circumlunar.space).

To serve documentation over Gemini only, run:

	gddo --gemini :1965 --certs /var/lib/gemini/certs --hostname example.com

See the [documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo) for
more information.

## Questions? Patches?

Send them to the [mailing list](https://lists.sr.ht/~sircmpwn/godocs.io).

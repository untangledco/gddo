# godocs.io

[godocs.io](https://godocs.io) is a maintained fork of the now-defunct
godoc.org, and you can use it to browse documentation for Go packages.

## How to use

Visit [godocs.io](https://godocs.io) and type the import path for your package
into the search box. To add a badge to your project like this:

[![godocs.io](https://godocs.io/git.sr.ht/~sircmpwn/dowork?status.svg)](https://godocs.io/git.sr.ht/~sircmpwn/dowork)

Click on "tools" for package owners at the bottom of the page.

## Installation

First install the dependencies:

- Go 1.16 or above
- PostgreSQL 13
- Graphviz (required for import graphs)

Then compile and install:

	$ make
	# make install

## Running

Initialize the PostgreSQL database:

	psql -f schema.sql

Then run:

	gddo-server \
		--db "postgres://localhost" \
		--http ":8080" \
		--goproxy "https://proxy.golang.org"

See the [documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo-server)
for more information.

## Questions? Patches?

Send them to the [godocs.io list](https://lists.sr.ht/~sircmpwn/godocs.io).

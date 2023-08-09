# gddo

[![Go Documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo?status.svg)](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo)

gddo is a maintained fork of the software that once powered godoc.org, and you
can use it to browse documentation for Go packages.

A hosted instance is available at [godocs.io](https://godocs.io).

## Installation

First install the dependencies:

- Go 1.19 or above

Then compile and install:

	$ make
	# make install

## Running

Initialize the PostgreSQL database:

	psql -f schema.sql

Then run:

	gddo \
		--db "postgres://localhost" \
		--http :8080 \
		--goproxy "https://proxy.golang.org"

## Gemini support

gddo can also serve documentation over the
[Gemini protocol](https://gemini.circumlunar.space).

To serve documentation over Gemini only, run:

	gddo --gemini :1965 --certs /var/lib/gemini/certs --hostname example.com

See the [documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo) for
more information.

## Questions? Patches?

Send them to the [mailing list](https://lists.sr.ht/~sircmpwn/godocs.io).

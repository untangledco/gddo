# gddo

[![Go Documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo?status.svg)](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo)

gddo is a maintained fork of the software that once powered godoc.org, and you
can use it to browse documentation for Go packages.

A hosted instance is available at [godocs.io](https://godocs.io).

## Installation

	go install ./cmd/gddo/

## Running

Initialize the PostgreSQL database:

	psql -f schema.sql

Then run:

	gddo \
		--db "postgres://localhost" \
		--http :8080

See `gddo --help` for all available flags.

See the [documentation](https://godocs.io/git.sr.ht/~sircmpwn/gddo/cmd/gddo) for
more information.

## Questions? Patches?

Send them to the [mailing list](https://lists.sr.ht/~sircmpwn/godocs.io).

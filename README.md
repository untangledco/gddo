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

- Go 1.13 or above
- PostgreSQL 13

Initialize the PostgreSQL database:

	psql -f schema.sql

Then run:

	go run ./gddo-server \
		--db "postgres://localhost" \
		--http ":8080" \
		--goproxy "https://proxy.golang.org"

## Questions? Patches?

Send them to the [godocs.io list](https://lists.sr.ht/~sircmpwn/godocs.io).

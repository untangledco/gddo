package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"

	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
)

var (
	ErrBlocked     = errors.New("blocked import path")
	ErrMismatch    = errors.New("import paths don't match")
	ErrNoPackages  = errors.New("no packages found")
	ErrBadVersion  = errors.New("invalid version")
	ErrInvalidPath = errors.New("invalid import path")
)

func shouldDisplayError(err error) bool {
	return !errors.Is(err, ErrBlocked) && !errors.Is(err, proxy.ErrNotFound)
}

func errorMessage(err error) (string, int) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "This package is being fetched in the background. Feel free to refresh while we're working on it.", http.StatusNotFound
	case errors.Is(err, ErrMismatch):
		return "Error fetching module: The provided import path doesn't match the module path present in the go.mod file.", http.StatusNotFound
	case errors.Is(err, ErrNoPackages):
		return "Error fetching module: The requested module doesn't contain any packages.", http.StatusNotFound
	case errors.Is(err, ErrInvalidPath):
		return "Error fetching module: Invalid import path.", http.StatusNotFound
	case errors.Is(err, ErrBadVersion):
		return "Error fetching module: Invalid version.", http.StatusNotFound
	case errors.Is(err, platforms.ErrInvalid):
		return "Error fetching module: Invalid platform.", http.StatusNotFound
	case errors.Is(err, proxy.ErrNotFound), errors.Is(err, ErrBlocked):
		// No error message
		return "", http.StatusInternalServerError
	}
	return "Internal server error.", http.StatusInternalServerError
}

func logPanic(url *url.URL, rv interface{}) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Error serving %s: handler panic\n", url)
	fmt.Fprintln(&buf, rv)
	buf.Write(debug.Stack())
	log.Print(buf.String())
}

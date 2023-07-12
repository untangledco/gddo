package server

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"

	"git.sr.ht/~sircmpwn/gddo/internal"
)

var (
	ErrBlocked    = errors.New("blocked import path")
	ErrNoPackages = errors.New("no packages found")
	ErrFetching   = errors.New("fetch in progress")

	ErrInvalidPlatform = errors.New("invalid platform")
)

// ErrMismatch represents the case where the import path is different from the
// module path in the go.mod file.
type ErrMismatch struct {
	ExpectedPath string
	ActualPath   string
}

func (e ErrMismatch) Error() string {
	return fmt.Sprintf("import paths don't match: expected %q, got %q", e.ExpectedPath, e.ActualPath)
}

func shouldDisplayError(err error) bool {
	return !errors.Is(err, ErrBlocked) && !errors.Is(err, internal.ErrNotFound)
}

func errorMessage(err error) (string, int) {
	switch {
	case errors.Is(err, ErrFetching):
		return "This package is being fetched in the background. Feel free to refresh while we're working on it.", http.StatusNotFound
	case errors.Is(err, ErrNoPackages):
		return "Error fetching module: The requested module doesn't contain any packages.", http.StatusNotFound
	case errors.Is(err, internal.ErrInvalidPath):
		return "Error fetching module: Invalid import path.", http.StatusNotFound
	case errors.Is(err, internal.ErrInvalidVersion):
		return "Error fetching module: Invalid version.", http.StatusNotFound
	case errors.Is(err, ErrInvalidPlatform):
		return "Error fetching module: Invalid platform.", http.StatusNotFound
	case errors.Is(err, internal.ErrNotFound), errors.Is(err, ErrBlocked):
		// No error message
		return "", http.StatusNotFound
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

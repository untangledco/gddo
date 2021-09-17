package meta

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

var ErrMetaNotFound = errors.New("no go-source meta tag found")

// Meta represents the values in a go-source meta tag.
type Meta struct {
	ProjectRoot string
	ProjectName string
	ProjectURL  string
	DirFmt      string
	FileFmt     string
	LineFmt     string
}

// Dir returns a link to the provided directory.
func (m *Meta) Dir(dir string) string {
	dir, slashDir := processDir(dir)
	return fmt.Sprintf(m.DirFmt, dir, slashDir)
}

// File returns a link to the provided file.
func (m *Meta) File(dir, file string) string {
	dir, slashDir := processDir(dir)
	return fmt.Sprintf(m.FileFmt, dir, slashDir, file)
}

// Line returns a link to the provided line.
func (m *Meta) Line(dir, file string, line int) string {
	dir, slashDir := processDir(dir)
	return fmt.Sprintf(m.LineFmt, dir, slashDir, file, line)
}

func processDir(s string) (dir, slashDir string) {
	dir = strings.Trim(s, "/")
	if dir != "" {
		slashDir = "/" + dir
	}
	return
}

func processDirTemplate(s string) string {
	s = strings.Replace(s, "%", "%%", -1)
	s = strings.Replace(s, "{dir}", "%[1]s", -1)
	s = strings.Replace(s, "{/dir}", "%[2]s", -1)
	return s
}

func processFileTemplate(s string) string {
	s = processDirTemplate(s)
	s = strings.Replace(s, "{file}", "%[3]s", -1)
	// Cut point is right after last {file} section.
	cut := strings.LastIndex(s, "{file}")
	if cut != -1 {
		cut += len("{file}")
	}
	switch hash := strings.Index(s, "#"); {
	// If there's no '#', place cut at the end.
	case hash == -1:
		cut = len(s)
	// If a '#' comes after last {file}, use it as cut point.
	case hash > cut:
		cut = hash
	case cut == -1:
		cut = len(s)
	}
	return s[:cut]
}

func processLineTemplate(s string) string {
	s = processDirTemplate(s)
	s = strings.Replace(s, "{file}", "%[3]s", -1)
	s = strings.Replace(s, "{line}", "%[4]d", -1)
	return s
}

const (
	stdlibDirFmt  = "https://github.com/golang/go/tree/master/src/%s{/dir}"
	stdlibFileFmt = "https://github.com/golang/go/blob/master/src/%s{/dir}/{file}"
	stdlibLineFmt = "https://github.com/golang/go/blob/master/src/%s{/dir}/{file}#L{line}"
)

// FetchMeta fetches the go-source meta tag for the provided module path.
func FetchMeta(ctx context.Context, client *http.Client, modulePath, userAgent string) (*Meta, error) {
	// Special case for stdlib
	if stdlib.Contains(modulePath) {
		return &Meta{
			ProjectRoot: modulePath,
			ProjectName: "Go",
			ProjectURL:  "/std",
			DirFmt:      processDirTemplate(fmt.Sprintf(stdlibDirFmt, modulePath)),
			FileFmt:     processFileTemplate(fmt.Sprintf(stdlibFileFmt, modulePath)),
			LineFmt:     processLineTemplate(fmt.Sprintf(stdlibLineFmt, modulePath)),
		}, nil
	}

	uri := modulePath
	if !strings.Contains(uri, "/") {
		// Add slash for root of domain.
		uri += "/"
	}
	uri += "?go-get=1"

	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if err == nil {
			resp.Body.Close()
		}
		req.URL.Scheme = "http"
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	meta := parseMeta(resp.Body)
	if meta == nil {
		return nil, ErrMetaNotFound
	}
	return meta, nil
}

// parseMeta parses a go-source meta tag from the provided response body.
// It returns nil if no valid meta tag was found.
func parseMeta(body io.Reader) *Meta {
	d := xml.NewDecoder(body)
	d.Strict = false

scan:
	for {
		t, err := d.Token()
		if err != nil {
			break scan
		}
		switch t := t.(type) {
		case xml.EndElement:
			if strings.EqualFold(t.Name.Local, "head") {
				break scan
			}
		case xml.StartElement:
			if strings.EqualFold(t.Name.Local, "body") {
				break scan
			}
			if !strings.EqualFold(t.Name.Local, "meta") {
				continue scan
			}
			nameAttr := attrValue(t.Attr, "name")
			if nameAttr != "go-source" {
				continue scan
			}

			fields := strings.Fields(attrValue(t.Attr, "content"))
			if len(fields) != 4 {
				continue scan
			}
			return &Meta{
				ProjectRoot: fields[0],
				ProjectName: path.Base(fields[0]),
				ProjectURL:  fields[1],
				DirFmt:      processDirTemplate(fields[2]),
				FileFmt:     processFileTemplate(fields[3]),
				LineFmt:     processLineTemplate(fields[3]),
			}
		}
	}
	return nil
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

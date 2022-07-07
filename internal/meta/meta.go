// Package meta contains functions for processing forge meta tags.
package meta

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

var ErrNoInfo = errors.New("no project information found")

// Project contains information about a project.
type Project struct {
	ModulePath string
	Name       string
	URL        string
	DirFmt     string
	FileFmt    string
	LineFmt    string
}

// Dir returns a link to the provided directory.
func (p *Project) Dir(ref, dir string) string {
	return fmt.Sprintf(p.DirFmt, ref, dir)
}

// File returns a link to the provided file.
func (p *Project) File(ref, dir, file string) string {
	return fmt.Sprintf(p.FileFmt, ref, path.Join(dir, file))
}

// Line returns a link to the provided line.
func (p *Project) Line(ref, dir, file, line string) string {
	return fmt.Sprintf(p.LineFmt, ref, path.Join(dir, file), line)
}

const (
	stdlibDirFmt  = "https://github.com/golang/go/tree/{ref}/src/{path}"
	stdlibFileFmt = "https://github.com/golang/go/blob/{ref}/src/{path}"
	stdlibLineFmt = "https://github.com/golang/go/blob/{ref}/src/{path}#L{line}"
)

// Fetch fetches project information for the provided module path.
func Fetch(ctx context.Context, client *http.Client, modulePath, userAgent string) (*Project, error) {
	// Special case for stdlib
	if stdlib.Contains(modulePath) {
		return &Project{
			ModulePath: modulePath,
			Name:       "Go",
			URL:        "/std",
			DirFmt:     processTemplate(stdlibDirFmt),
			FileFmt:    processTemplate(stdlibFileFmt),
			LineFmt:    processLineTemplate(stdlibLineFmt),
		}, nil
	}

	uri := modulePath
	if !strings.Contains(uri, "/") {
		// Add slash for root of domain.
		uri += "/"
	}

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

	// Parse body for forge meta tags
	d := xml.NewDecoder(resp.Body)
	d.Strict = false

	project := &Project{
		ModulePath: modulePath,
		Name:       path.Base(modulePath),
	}
	ok := false

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
			if !strings.HasPrefix(nameAttr, "forge:") {
				continue scan
			}
			switch nameAttr {
			case "forge:summary":
				project.URL = attrValue(t.Attr, "content")
			case "forge:dir":
				project.DirFmt = processTemplate(attrValue(t.Attr, "content"))
			case "forge:file":
				project.FileFmt = processTemplate(attrValue(t.Attr, "content"))
			case "forge:line":
				project.LineFmt = processLineTemplate(attrValue(t.Attr, "content"))
			default:
				continue scan
			}
			// Found at least one meta tag
			ok = true
		}
	}

	if ok {
		return project, nil
	}
	return nil, ErrNoInfo
}

func processTemplate(s string) string {
	s = strings.Replace(s, "%", "%%", -1)
	s = strings.Replace(s, "{ref}", "%[1]s", -1)
	s = strings.Replace(s, "{path}", "%[2]s", -1)
	return s
}

func processLineTemplate(s string) string {
	s = processTemplate(s)
	s = strings.Replace(s, "{line}", "%[3]s", -1)
	return s
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

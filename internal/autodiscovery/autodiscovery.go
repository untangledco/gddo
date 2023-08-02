// Package autodiscovery implements the [VCS Autodiscovery RFC].
//
// [VCS Autodiscovery RFC]: https://git.sr.ht/~ancarda/vcs-autodiscovery-rfc/tree/trunk/RFC.md
package autodiscovery

import (
	"context"
	"encoding/xml"
	"net/http"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

// Project facilitates linking to project source code.
type Project struct {
	Summary string
	Dir     string
	File    string
	RawFile string
	Line    string
}

// DirURL returns a URL for the given directory.
func (p *Project) DirURL(ref, dir string) string {
	return strings.NewReplacer(
		"{ref}", ref,
		"{path}", dir,
	).Replace(p.Dir)
}

// FileURL returns a URL for the given file.
func (p *Project) FileURL(ref, dir, file string) string {
	return strings.NewReplacer(
		"{ref}", ref,
		"{path}", path.Join(dir, file),
	).Replace(p.File)
}

// RawFileURL returns a URL for the raw contents of the given file.
func (p *Project) RawFileURL(ref, dir, file string) string {
	return strings.NewReplacer(
		"{ref}", ref,
		"{path}", path.Join(dir, file),
	).Replace(p.RawFile)
}

// LineURL returns a link to the provided line.
func (p *Project) LineURL(ref, dir, file, line string) string {
	return strings.NewReplacer(
		"{ref}", ref,
		"{path}", path.Join(dir, file),
		"{line}", line,
	).Replace(p.Line)
}

func stdlibProject() *Project {
	return &Project{
		Summary: "/std",
		Dir:     "https://github.com/golang/go/tree/{ref}/src/{path}",
		File:    "https://github.com/golang/go/blob/{ref}/src/{path}",
		RawFile: "https://github.com/golang/go/raw/{ref}/src/{path}",
		Line:    "https://github.com/golang/go/blob/{ref}/src/{path}#L{line}",
	}
}

// Fetch fetches project information for the provided module series path.
// It returns nil if no project information was found.
func Fetch(ctx context.Context, client *http.Client, seriesPath, userAgent string) (*Project, error) {
	// Special case for stdlib
	if stdlib.Contains(seriesPath) {
		return stdlibProject(), nil
	}

	uri := seriesPath
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

	p := &Project{}
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
				p.Summary = attrValue(t.Attr, "content")
			case "forge:dir":
				p.Dir = attrValue(t.Attr, "content")
			case "forge:file":
				p.File = attrValue(t.Attr, "content")
			case "forge:rawfile":
				p.RawFile = attrValue(t.Attr, "content")
			case "forge:line":
				p.Line = attrValue(t.Attr, "content")
			default:
				continue scan
			}
			// Found at least one meta tag
			ok = true
		}
	}
	if !ok {
		return nil, nil
	}
	return p, nil
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
)

const jsonMIMEType = "application/json; charset=utf-8"

func (s *Server) serveAPISearch(resp http.ResponseWriter, req *http.Request) error {
	q := strings.TrimSpace(req.Form.Get("q"))

	var pkgs []database.Package

	pkg, ok, err := s.db.GetPackage(req.Context(), q, proxy.LatestVersion)
	if err == nil && ok {
		pkgs = []database.Package{pkg}
	}

	if pkgs == nil {
		var err error
		pkgs, err = s.db.Search(req.Context(), q)
		if err != nil {
			return err
		}
	}

	var data = struct {
		Results []database.Package `json:"results"`
	}{
		pkgs,
	}
	resp.Header().Set("Content-Type", jsonMIMEType)
	return json.NewEncoder(resp).Encode(&data)
}

func (s *Server) serveAPIImporters(resp http.ResponseWriter, req *http.Request) error {
	importPath := strings.TrimPrefix(req.URL.Path, "/importers/")
	pkgs, err := s.db.Importers(req.Context(), importPath)
	if err != nil {
		return err
	}
	data := struct {
		Results []database.Package `json:"results"`
	}{
		pkgs,
	}
	resp.Header().Set("Content-Type", jsonMIMEType)
	return json.NewEncoder(resp).Encode(&data)
}

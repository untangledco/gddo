package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang/gddo/database"
	"github.com/golang/gddo/gosrc"
)

const jsonMIMEType = "application/json; charset=utf-8"

func (s *server) serveAPISearch(resp http.ResponseWriter, req *http.Request) error {
	q := strings.TrimSpace(req.Form.Get("q"))

	var pkgs []database.Package

	if gosrc.IsValidRemotePath(q) || (strings.Contains(q, "/") && gosrc.IsGoRepoPath(q)) {
		pdoc, _, err := s.getDoc(req.Context(), q, apiRequest)
		if e, ok := err.(gosrc.NotFoundError); ok && e.Redirect != "" {
			pdoc, _, err = s.getDoc(req.Context(), e.Redirect, robotRequest)
		}
		if err == nil && pdoc != nil {
			pkgs = []database.Package{{Path: pdoc.ImportPath, Synopsis: pdoc.Synopsis}}
		}
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

func (s *server) serveAPIImporters(resp http.ResponseWriter, req *http.Request) error {
	importPath := strings.TrimPrefix(req.URL.Path, "/importers/")
	pkgs, err := s.db.Importers(importPath)
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

func (s *server) serveAPIImports(resp http.ResponseWriter, req *http.Request) error {
	importPath := strings.TrimPrefix(req.URL.Path, "/imports/")
	pdoc, _, err := s.getDoc(req.Context(), importPath, robotRequest)
	if err != nil {
		return err
	}
	if pdoc == nil || pdoc.Name == "" {
		return &httpError{status: http.StatusNotFound}
	}
	imports, err := s.db.Packages(pdoc.Imports)
	if err != nil {
		return err
	}
	testImports, err := s.db.Packages(pdoc.TestImports)
	if err != nil {
		return err
	}
	data := struct {
		Imports     []database.Package `json:"imports"`
		TestImports []database.Package `json:"testImports"`
	}{
		imports,
		testImports,
	}
	resp.Header().Set("Content-Type", jsonMIMEType)
	return json.NewEncoder(resp).Encode(&data)
}

func serveAPIHome(resp http.ResponseWriter, req *http.Request) error {
	return &httpError{status: http.StatusNotFound}
}

func handleAPIError(resp http.ResponseWriter, req *http.Request, status int, err error) {
	var data struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	data.Error.Message = http.StatusText(status)
	resp.Header().Set("Content-Type", jsonMIMEType)
	resp.WriteHeader(status)
	json.NewEncoder(resp).Encode(&data)
}

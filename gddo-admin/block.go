// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package main

import (
	"context"
	"log"
	"os"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
)

var blockCommand = &command{
	name:  "block",
	run:   block,
	usage: "block path",
}

func block(c *command) {
	if len(c.flag.Args()) != 1 {
		c.printUsage()
		os.Exit(1)
	}
	db, err := database.New(*pgServer)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	if err := db.Block(ctx, c.flag.Args()[0]); err != nil {
		log.Fatal(err)
	}
}

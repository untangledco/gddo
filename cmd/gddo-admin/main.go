// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

// Command gddo-admin is the command line administration tool for gddo-server.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type command struct {
	name  string
	run   func(c *command)
	flag  flag.FlagSet
	usage string
}

func (c *command) printUsage() {
	fmt.Fprintf(os.Stderr, "%s %s\n", os.Args[0], c.usage)
	c.flag.PrintDefaults()
}

var (
	pgServer = flag.String("db", "postgres://localhost", "PostgreSQL database URL")
)

var commands = []*command{
	blockCommand,
	deleteCommand,
}

func printUsage() {
	var n []string
	for _, c := range commands {
		n = append(n, c.name)
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", os.Args[0], strings.Join(n, "|"))
	flag.PrintDefaults()
	for _, c := range commands {
		c.printUsage()
	}
}

func main() {
	flag.Usage = printUsage
	flag.Parse()
	args := flag.Args()

	if len(args) >= 1 {
		for _, c := range commands {
			if args[0] == c.name {
				c.flag.Usage = func() {
					c.printUsage()
					os.Exit(2)
				}
				c.flag.Parse(args[1:])
				c.run(c)
				return
			}
		}
	}
	printUsage()
	os.Exit(2)
}

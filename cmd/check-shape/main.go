// Command check-shape is the gate for the app-shape record.
//
// A tree declares which product it is in `.gogogo/shape.yml`; this command
// proves the declaration agrees with the tree before anything is built: the
// shape is one of the closed enum, every entrypoint it keeps is a `package
// main` that exists, and every path it says it dropped is gone. A tree with no
// file is `full`, so existing checkouts pass without one.
//
// The build proof is deliberately not here. CI builds the declared
// entrypoints; a gate that compiles cannot run on every commit.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/calionauta/gogogo/internal/shape"
)

func main() {
	dir := flag.String("dir", ".", "tree to check")
	quiet := flag.Bool("quiet", false, "print only failures")
	flag.Parse()

	problems, err := shape.Check(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-shape: "+err.Error())
		os.Exit(1)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "check-shape: %d problem(s); the record lives in %s\n",
			len(problems), shape.Path)
		os.Exit(1)
	}
	if !*quiet {
		c, _ := shape.Load(*dir)
		fmt.Printf("check-shape: %s — entrypoints %v, %d removed path(s)\n",
			c.Shape, c.Entrypoints, len(c.Removed))
	}
}

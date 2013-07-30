// program dbgp2dbg implements a dbgp to gdb proxy
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/traviscline/dbgp/gdbproxy"
)

var dial = flag.String("dial", "localhost:9000", "DBGP host/port to conenct to")
var target string

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "No target specified")
		flag.Usage()
		os.Exit(1)
	}
	target = flag.Args()[0]

	p, err := gdbproxy.NewProxy(*dial, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error creating proxy:", err)
		os.Exit(1)
	}
	if err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error running proxy:", err)
		os.Exit(1)
	}
}

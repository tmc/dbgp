// Program dbgp2dbg implements a dbgp to gdb proxy
//
// dbg2dbg (gdb target)
//
// note: invoke with the following options to debug: -v=2 -logtostderr
package main

import (
	"flag"
	"fmt"
	"github.com/traviscline/dbgp"
	"github.com/traviscline/dbgp/gdbproxy"
	"log"
	"net"
	"os"
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

	log.Println("dialing", *dial)
	c, err := net.Dial("tcp", *dial)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error connecting to IDE:", err)
		os.Exit(1)
	}

	ideKey, session := os.Getenv("DBGP_IDEKEY"), os.Getenv("DBGP_COOKIE")
	p, err := gdbproxy.New(target, ideKey, session)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error creating proxy:", err)
		os.Exit(1)
	}

	conn := dbgp.NewConn(c, p)
	if err := conn.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error running proxy:", err)
		os.Exit(1)
	}
}

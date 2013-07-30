// package proxy implements a dbgp to gdb proxy interface
package proxy

import (
	"bufio"

	"io"
	"log"
	"net"
	"os"
	"os/exec"

	"github.com/traviscline/dbgp"
)

type Proxy struct {
	c      net.Conn
	gdbCmd *exec.Cmd

	stdout, stderr io.Reader
	stdin          io.Writer
}

// Create a proxy for the given port and gdb target
func NewProxy(dial, target string) (*Proxy, error) {
	log.Println("dialing", dial)
	p := &Proxy{}
	c, err := net.Dial("tcp", dial)
	if err != nil {
		return nil, err
	}
	p.c = c

	p.gdbCmd = exec.Command("gdb", target)
	log.Println("invoking", "gdb", target)

	if p.stdout, err = p.gdbCmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if p.stderr, err = p.gdbCmd.StderrPipe(); err != nil {
		return nil, err
	}
	if p.stdin, err = p.gdbCmd.StdinPipe(); err != nil {
		return nil, err
	}

	if err := p.gdbCmd.Start(); err != nil {
		return nil, err
	}

	return p, nil
}

// Starts proxy process
func (p *Proxy) Run() error {
	stdout := bufio.NewReader(p.stdout)
	stderr := bufio.NewReader(p.stderr)

	// start collection loops
	go io.Copy(p.stdin, os.Stdin)

	errors := make(chan error)
	goRead(stdout, errors)
	goRead(stderr, errors)

	client := dbgp.NewClient(p.c)

	if err := client.Init(); err != nil {
		return err
	}
	for {
		cmd, err := client.Next()
		if err != nil {
			log.Println("dbgp err:", err)
		} else {
			log.Println("dbgp cmd:", cmd)
		}
		if err := <-errors; err != nil {
			log.Println("got error:", err)
		}
	}
}

func goRead(r *bufio.Reader, errc chan error) {
	go func() {
		for {
			s, err := r.ReadString('\n')
			log.Print("read:", s)
			if err != nil {
				log.Println("error:", err)
				errc <- err
				return
			}
		}
	}()
}

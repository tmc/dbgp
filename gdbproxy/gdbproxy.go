// package gdbproxy implements a dbgp.DBGPClient that is backed by a gdb session
package gdbproxy

import (
	"bufio"
	"github.com/traviscline/dbgp"
	"io"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var defaultWait = 100 * time.Millisecond

type GDB struct {
	ideKey, session string

	cmd *exec.Cmd

	stdoutCh, stderrCh <-chan string
	stdinCh            chan<- string

	stdout, stderr io.Reader
	stdin          io.Writer

	errChan chan error
}

// Starts proxy process
func (g *GDB) Init() dbgp.InitResponse {
	// consume header
	g.consumeLines(g.stdoutCh, defaultWait)

	fileName, lang := g.currentFilenameAndLang()

	return dbgp.InitResponse{
		AppId:    "gdbproxy",
		IdeKey:   g.ideKey,
		Session:  g.session,
		Thread:   "1",
		Language: lang,
		FileURI:  "file://" + fileName,
	}
}

func (g *GDB) StepInto() (status, reason string) {
	g.stdinCh <- "b 1"
	g.stdinCh <- "run"
	log.Println("[gdbproxy] b 1; run:", g.stdoutLines())
	return "break", "ok"
}

func (g *GDB) StackDepth() int {
	log.Println("gdb: step into")
	return 2
}

func (g *GDB) StackGet(depth int) []dbgp.Stack {
	log.Println("gdb: stack get")
	fn, _ := g.currentFilenameAndLang()
	return []dbgp.Stack{
		{
			Filename: "file://" + fn,
			Lineno:   1,
			Where:    "{main}",
		},
	}
}

func (g *GDB) ContextNames(depth int) []dbgp.Context {
	return []dbgp.Context{{"Local", 0}}
}

func (g *GDB) ContextGet(depth, context int) []dbgp.Property {
	// @todo consider depth, context
	g.stdinCh <- "info locals"
	lines1 := g.stdoutLines()
	g.stdinCh <- "info args"
	lines := append(lines1, g.stdoutLines()...)

	properties := make([]dbgp.Property, 0)
	re := regexp.MustCompile("(.+) = (.+) ?(.+)?")

	log.Println("ContextNames a:", lines1)
	log.Println("ContextNames b:", lines)
	for _, l := range lines {
		matches := re.FindStringSubmatch(l)
		log.Println("ContextNames matches", l, matches)
		if len(matches) != 4 {
			log.Println("unexpected number of matches (wanted 4):", matches)
		}
		properties = append(properties, dbgp.Property{
			Name:     matches[1],
			Fullname: matches[1],
			Address:  matches[2],
			Type:     g.getType(matches[1]),
		})
	}

	return properties
}

// creates a new GDB DBGP Proxy for the specified targert
func New(target, ideKey, session string) (*GDB, error) {
	cmd := exec.Command("gdb", target)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// start io goroutines
	errChan := make(chan error)
	return &GDB{
		ideKey:   ideKey,
		session:  session,
		cmd:      cmd,
		stdout:   stdout,
		stderr:   stderr,
		stdin:    stdin,
		stdoutCh: scanReaderToChan(stdout, errChan),
		stderrCh: scanReaderToChan(stderr, errChan),
		stdinCh:  stringChanToWriter(stdin, errChan),
		errChan:  errChan,
	}, nil
}

// get the type for a symbol
func (g *GDB) getType(symbol string) string {
	g.stdinCh <- "ptype " + symbol
	typeInfo := g.stdoutLines()
	if len(typeInfo) == 0 {
		return "unknown"
	}
	ti := typeInfo[0]
	ti = strings.Replace(ti, "type = struct ", "", 1)
	ti = strings.Replace(ti, " {", "", 1)
	return ti
}

// Obtain the current filename and language via "info source"
func (g *GDB) currentFilenameAndLang() (fileName, lang string) {
	//go io.Copy(g.stdin, os.Stdin) // @todo consider user stdin
	g.stdinCh <- "list 1"
	// not interested in list output, needed for "info source"
	g.stdoutLines()
	g.stdinCh <- "info source"

	sourceInfo := g.stdoutLines()
	info := strings.Join(sourceInfo, "\n")
	log.Println("source info:", info)

	// extract meaningful things
	fileNameRe := regexp.MustCompile("Current source file is (.+)")
	fileNameMatches := fileNameRe.FindStringSubmatch(info)
	if len(fileNameMatches) != 2 {
		log.Println("Unexpected match (wanted 2):", fileNameMatches)
	}

	langRe := regexp.MustCompile("Source language is (.+).")
	langMatches := langRe.FindStringSubmatch(info)
	if len(langMatches) != 2 {
		log.Println("Unexpected match (wanted 2):", langMatches)
	}

	// select the first group from each regex match result
	return fileNameMatches[1], langMatches[1]
}

// Get stdout lines, waiting defaultWait
func (g *GDB) stdoutLines() []string {
	result := g.consumeLines(g.stdoutCh, defaultWait)
	for i, l := range result {
		result[i] = strings.Replace(l, "(gdb) ", "", 1)
	}
	return result
}

// consumes c until it doesn't respond in maxWait time
func (g *GDB) consumeLines(c <-chan string, maxWait time.Duration) []string {
	result := make([]string, 0, 10)
	for {
		select {
		case line := <-c:
			result = append(result, line)
			log.Println("[gdbproxy]", line)
		case err := <-g.errChan:
			log.Println("error while consuming:", err)
		case <-time.After(maxWait): // give other end of the channel some time
			return result
		}
	}
	return result
}

// Consumes the reader and generates a string for every newline read
func scanReaderToChan(r io.Reader, errChan chan<- error) <-chan string {
	c := make(chan string)
	scanner := bufio.NewScanner(r)
	go func() {
		for scanner.Scan() {
			c <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errChan <- err
		}
	}()
	return c
}

// Provides a writable channel of strings as the interface to a writer. Newlines
// are automatically appended
func stringChanToWriter(w io.Writer, errChan chan<- error) chan<- string {
	c := make(chan string)
	bw := bufio.NewWriter(w)
	go func() {
		for {
			s := <-c
			_, err := bw.WriteString(s)
			if err != nil {
				errChan <- err
			}
			_, err = bw.WriteString("\n")
			if err != nil {
				errChan <- err
			}
			log.Println("[gdbproxy]", "wrote", s)
			bw.Flush()
		}
	}()
	return c
}

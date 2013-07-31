// package gdbproxy implements a dbgp.DBGPClient that is backed by a gdb session
package gdbproxy

import (
	"bufio"
	"fmt"
	"github.com/traviscline/dbgp"
	"io"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var defaultWait = 100 * time.Millisecond

type GDB struct {
	status          string // ("starting", "stopping", "running", "break")
	ideKey, session string
	features        dbgp.Features

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

	lineNumber, lang := g.currentFilenameAndLang()
	g.features.Language_name = lang

	return dbgp.InitResponse{
		AppId:    "gdbproxy",
		IdeKey:   g.ideKey,
		Session:  g.session,
		Thread:   "1",
		Language: lang,
		FileURI:  "file://" + lineNumber,
	}
}

func (g *GDB) Status() string {
	return g.status
}

func (g *GDB) Features() dbgp.Features {
	return g.features
}

func (g *GDB) start() {
	g.stdinCh <- "b 1"
	g.stdinCh <- "run"
	g.status = "break"
	log.Println("[gdbproxy] start:", g.stdoutLines())
}

func (g *GDB) StepInto() (status, reason string) {
	if g.status == "starting" {
		g.start()
	}
	g.stdinCh <- "s"
	log.Println("[gdbproxy] StepInto:", g.stdoutLines())
	return "break", "ok"
}

func (g *GDB) StepOver() (status, reason string) {
	if g.status == "starting" {
		g.start()
	}
	g.stdinCh <- "n"
	log.Println("[gdbproxy] StepOver:", g.stdoutLines())
	return "break", "ok"
}

func (g *GDB) StackDepth() int {
	log.Println("gdb: step into")
	return 2 // @todo make accurate
}

func (g *GDB) StackGet(depth int) []dbgp.Stack {
	log.Println("gdb: stack get")
	fn, _ := g.currentFilenameAndLang()
	line, _ := g.currentLineNumber()
	return []dbgp.Stack{
		{
			Filename: "file://" + fn,
			Type:     "file",
			Lineno:   line,
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

func (g *GDB) SetBreakpoint(bpType, fileName string, lineNumber int) dbgp.Breakpoint {
	if bpType != "line" {
		log.Println("[dbgproxy] only supports line breakpoints currently.")
	}

	cmd := fmt.Sprintf("b %s:%d", stripAbsFilePrefix(fileName), lineNumber)
	g.stdinCh <- "set breakpoint pending on"
	g.stdinCh <- cmd

	bpNumberRe := regexp.MustCompile("Breakpoint ([0-9]+) ")
	bpLines := strings.Join(g.stdoutLines(), "\n")
	matches := bpNumberRe.FindStringSubmatch(bpLines)
	if len(matches) != 2 {
		log.Println("unexpected number of matches (wanted 2):", matches)
		log.Println("unexpected number of matches (wanted 2):", bpLines)
	}
	bpNum, err := strconv.Atoi(matches[1])
	if err != nil {
		log.Println("[dbgproxy] could not Atoi:", matches[1])
	}

	return dbgp.Breakpoint{bpNum, "enabled"}
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
		status:   "starting",
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
		features: dbgp.Features{},
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
func (g *GDB) currentFilenameAndLang() (lineNumber, lang string) {
	//go io.Copy(g.stdin, os.Stdin) // @todo consider user stdin
	g.stdinCh <- "list 1"
	// not interested in list output, needed for "info source"
	g.stdoutLines()
	g.stdinCh <- "info source"

	sourceInfo := g.stdoutLines()
	info := strings.Join(sourceInfo, "\n")
	//log.Println("source info:", info)

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

// Obtain the current line number
func (g *GDB) currentLineNumber() (int, error) {
	//go io.Copy(g.stdin, os.Stdin) // @todo consider user stdin
	g.stdinCh <- "where"
	lineInfo := g.stdoutLines()
	parts := strings.Join(lineInfo, "\n")

	whereRe := regexp.MustCompile("at (.+):([0-9]+)")

	matches := whereRe.FindStringSubmatch(parts)

	if len(matches) != 3 {
		return 0, fmt.Errorf("unexpected match length, expected 3: %s", fmt.Sprint(matches))
	}
	return strconv.Atoi(matches[2])
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
			log.Println("(gdb) ", line)
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

// Strips "file://" from the beginning of a string
func stripAbsFilePrefix(lineNumber string) string {
	return strings.TrimPrefix(lineNumber, "file://")
}

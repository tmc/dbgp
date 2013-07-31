// Package gdbproxy implements a dbgp.DBGPClient that is backed by a gdb session
package gdbproxy

import (
	"bufio"
	"fmt"
	"github.com/golang/glog"
	"github.com/traviscline/dbgp"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var defaultWait = 100 * time.Millisecond

// GDB iconnmplements the dbgp.DBGPClient protocol and manages an execution of gdb
type GDB struct {
	status          string // ("starting", "stopping", "running", "break")
	ideKey, session string
	features        dbgp.Features

	cmd *exec.Cmd

	stdout, stderr <-chan string
	stdin          chan<- string

	errChan chan error
}

// Init is invoked to begin the session with the upstream IDE or proxy
func (g *GDB) Init() dbgp.InitResponse {
	g.consumeLines(g.stdout, defaultWait)
	lineNumber, lang, _ := g.currentFilenameAndLang()
	g.features.Language_name = lang

	return dbgp.InitResponse{
		AppID:    "gdbproxy",
		IDeKey:   g.ideKey,
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
	g.stdin <- "b 1"
	g.stdin <- "run"
	g.status = "break"
	glog.V(1).Infoln("[gdbproxy] start:", g.stdoutLines())
}

func (g *GDB) StepInto() (status, reason string) {
	if g.status == "starting" {
		g.start()
	}
	g.stdin <- "s"
	glog.V(2).Infoln("[gdbproxy] StepInto:", g.stdoutLines())
	return "break", "ok"
}

func (g *GDB) StepOver() (status, reason string) {
	if g.status == "starting" {
		g.start()
	}
	g.stdin <- "n"
	glog.V(2).Infoln("[gdbproxy] StepOver:", g.stdoutLines())
	return "break", "ok"
}

func (g *GDB) StackDepth() int {
	return 2 // @todo make accurate
}

func (g *GDB) StackGet(depth int) ([]dbgp.Stack, error) {
	fn, _, err := g.currentFilenameAndLang()
	if err != nil {
		return nil, err
	}
	line, err := g.currentLineNumber()
	if err != nil {
		return nil, err
	}
	return []dbgp.Stack{
		{
			Filename: "file://" + fn,
			Type:     "file",
			Lineno:   line,
			Where:    "{main}",
		},
	}, nil
}

func (g *GDB) ContextNames(depth int) ([]dbgp.Context, error) {
	return []dbgp.Context{{"Local", 0}}, nil
}

func (g *GDB) ContextGet(depth, context int) ([]dbgp.Property, error) {
	// @todo consider depth, context
	g.stdin <- "info locals"
	lines := g.stdoutLines()
	g.stdin <- "info args"
	lines = append(lines, g.stdoutLines()...)

	properties := make([]dbgp.Property, 0)

	for _, l := range lines {
		matches, err := reExtract("(.+) = (.+) ?(.+)?", l, 1, 2)
		if err != nil {
			return nil, err
		}
		name, address := matches[0], matches[1]
		properties = append(properties, dbgp.Property{
			Name:     name,
			Fullname: name,
			Address:  address,
			Type:     g.getType(name),
		})
	}

	return properties, nil
}

func (g *GDB) PropertyGet(depth, context int, name string) (string, error) {
	g.stdin <- "p " + name
	lines := g.stdoutLines()
	if len(lines) == 0 {
		return "", fmt.Errorf("No output produced.")
	}
	vals, err := reExtract("\\$[0-9]+ = (.+)", lines[0], 1)
	return vals[0], err
}

func (g *GDB) BreakpointSet(bpType, fileName string, lineNumber int) (dbgp.Breakpoint, error) {
	if bpType != "line" {
		return dbgp.Breakpoint{}, fmt.Errorf("only line breakpoints are supported.")
	}

	cmd := fmt.Sprintf("b %s:%d", stripAbsFilePrefix(fileName), lineNumber)
	g.stdin <- "set breakpoint pending on"
	g.stdin <- cmd

	matches, err := reExtract("Breakpoint ([0-9]+) ", strings.Join(g.stdoutLines(), "\n"), 1)
	if err != nil {
		return dbgp.Breakpoint{}, err
	}
	bpNum, err := strconv.Atoi(matches[0])
	return dbgp.Breakpoint{ID: bpNum, State: "enabled"}, err
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
		stdout:   scanReaderToan(stdout, errChan),
		stderr:   scanReaderToan(stderr, errChan),
		stdin:    stringanToWriter(stdin, errChan),
		errChan:  errChan,
		features: dbgp.Features{},
	}, nil
}

// get the type for a symbol
func (g *GDB) getType(symbol string) string {
	g.stdin <- "ptype " + symbol
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
func (g *GDB) currentFilenameAndLang() (lineNumber, lang string, err error) {
	//go io.Copy(g.stdin, os.Stdin) // @todo consider user stdin
	g.stdin <- "list 1"
	// not interested in list output, needed for "info source"
	g.stdoutLines()
	g.stdin <- "info source"

	sourceInfo := g.stdoutLines()
	info := strings.Join(sourceInfo, "\n")

	// extract meaningful things
	fileNameMatches, e := reExtract("Current source file is (.+)", info, 1)
	if e != nil {
		err = e
		return
	}
	langMatches, e := reExtract("Source language is (.+).", info, 1)
	if e != nil {
		err = e
		return
	}

	// select the first group from each regex match result
	return fileNameMatches[0], langMatches[0], nil
}

// Obtain the current line number
func (g *GDB) currentLineNumber() (int, error) {
	//go io.Copy(g.stdin, os.Stdin) // @todo consider user stdin
	g.stdin <- "where"
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
	result := g.consumeLines(g.stdout, defaultWait)
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
			glog.V(2).Infoln("(gdb) ", line)
		case err := <-g.errChan:
			glog.Warningln("error while consuming:", err)
		case <-time.After(maxWait): // give other end of the channel some time
			return result
		}
	}
}

// Consumes the reader and generates a string for every newline read
func scanReaderToan(r io.Reader, errChan chan<- error) <-chan string {
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
func stringanToWriter(w io.Writer, errChan chan<- error) chan<- string {
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
			glog.V(1).Infoln("(gdb) ", s)
			bw.Flush()
		}
	}()
	return c
}

// Strips "file://" from the beginning of a string
func stripAbsFilePrefix(lineNumber string) string {
	return strings.TrimPrefix(lineNumber, "file://")
}

// extracts the specified matchGroups from target based on regex
func reExtract(regex, target string, matchGroup ...int) ([]string, error) {
	results := make([]string, 0)
	matches := regexp.MustCompile(regex).FindStringSubmatch(target)
	for _, mg := range matchGroup {
		if mg > len(matches)-1 {
			return nil, fmt.Errorf("not enough matches")
		}
		results = append(results, matches[mg])
	}
	return results, nil
}

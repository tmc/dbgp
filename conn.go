package dbgp

import (
	"bufio"
	"encoding/base64"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"strings"
)

type Conn struct {
	sock   *bufio.ReadWriter
	client DBGPClient
}

//var commands map[string]*flag.FlagSet

var nul = []byte{0}

var protocolVersion = 18

// Creates a new DBGP client connection with an rw for the communication and
// a DBGPClient
func NewConn(conn io.ReadWriter, client DBGPClient) *Conn {
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	return &Conn{rw, client}
}

// Initializes connection with the server
func (c *Conn) init() error {
	return c.writeXML(xml_init{xml.Name{}, c.client.Init(), "1.0"})
}

func (c *Conn) next() ([]string, error) {
	raw, err := c.sock.ReadString(0)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(raw, "\x00"), " "), nil
}

func (c *Conn) Run() error {
	if err := c.init(); err != nil {
		return err
	}
	flgs := new(flag.FlagSet)
	txId := flgs.Int("i", 0, "")
	depth := flgs.Int("d", 0, "")
	fileName := flgs.String("f", "", "")
	context := flgs.Int("c", 0, "")

	for {
		// reinit flags
		flgs.Init("", flag.ContinueOnError)

		parts, err := c.next()
		log.Println(parts)
		if err != nil {
			c.writeError("unknown", *txId, err)
		}

		cmd := parts[0]
		flgs.Parse(parts[1:])

		attrs := make(map[string]interface{})
		var (
			payload    interface{}
			payloadRaw bool // controls whether payload shoudl be marshalled or if it's already prepared
		)
		switch cmd {
		case "step_into":
			status, reason := c.client.StepInto()
			attrs["status"] = status
			attrs["reason"] = reason
		case "stack_depth":
			attrs["depth"] = c.client.StackDepth()
		case "source":
			log.Println("[conn.go]", "source", *fileName)
			fn := strings.Replace(*fileName, "file://", "", 1)
			f, err := os.Open(fn)
			if err != nil {
				log.Println("[conn.go] error opening file:", fn, err)
				break
			}
			a, err := ioutil.ReadAll(f)
			if err != nil {
				log.Println("[conn.go] error reading file:", err)
				break
			}
			attrs["encoding"] = "base64"
			payload = "<![CDATA[" + base64.StdEncoding.EncodeToString(a) + "]]>"
			payloadRaw = true
		case "stack_get":
			stackEntries := c.client.StackGet(*depth)
			wrapped := make([]stack, len(stackEntries))
			for i, se := range stackEntries {
				wrapped[i] = stack{se}
			}
			payload = wrapped

		case "context_names":
			names := c.client.ContextNames(*depth)
			payload = names

		case "context_get":
			contextProperties := c.client.ContextGet(*depth, *context)
			payload = contextProperties
		default:
			err = ErrUnimplemented
		}
		if err != nil {
			err = c.writeError(cmd, *txId, err)
		} else {
			err = c.writeResponse(cmd, *txId, attrs, payload, payloadRaw)
		}
		if err != nil {
			panic(err)
		}
	}
}

func (c *Conn) writeError(cmd string, txId int, err error) error {
	if _, ok := err.(dbgpError); !ok {
		err = dbgpError{999, err.Error()}
	}

	e := struct {
		XMLName string `xml:"error"`
		dbgpError
	}{"error", err.(dbgpError)}

	return c.writeResponse(cmd, txId, nil, e, false)
}

func (c *Conn) writeResponse(cmd string, txId int, attrs map[string]interface{}, payload interface{}, payloadRaw bool) error {
	if attrs == nil {
		attrs = make(map[string]interface{})
	}

	attrs["command"] = cmd
	attrs["transaction_id"] = txId
	attrs["xmlns"] = "urn:debugger_protocol_v1"

	attrsToStrings := make([]string, 0, len(attrs))
	for k, v := range attrs {
		attrsToStrings = append(attrsToStrings, fmt.Sprint(k, `="`, v, `"`))
	}

	var (
		payloadBytes []byte
		err          error
	)
	if payloadRaw {
		payloadBytes = []byte(fmt.Sprint(payload))
	} else {
		payloadBytes, err = xml.MarshalIndent(payload, "", " ")
		if err != nil {
			return err
		}

	}

	r := fmt.Sprintf(`<response %s>%s</response>`,
		strings.Join(attrsToStrings, " "),
		string(payloadBytes))

	return c.writeBytes([]byte(r))
}

func (c *Conn) writeXML(v interface{}) error {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	length := len(b) + len(xml.Header)
	c.sock.WriteString(fmt.Sprint(length))
	c.sock.Write(nul)
	c.sock.WriteString(xml.Header)
	_, err = c.sock.Write(b)
	if err != nil {
		return err
	}
	c.sock.Write(nul)
	return c.sock.Flush()
}

type stack struct {
	Stack
}

func (c *Conn) writeBytes(b []byte) error {
	c.sock.WriteString(fmt.Sprint(len(b)))
	c.sock.Write(nul)
	_, err := c.sock.Write(b)
	if err != nil {
		return err
	}
	c.sock.Write(nul)
	return c.sock.Flush()
}

// Encodes an init message
type xml_init struct {
	XMLName xml.Name `xml:"init"`
	InitResponse
	ProtocolVersion string `xml:"protocol_version,attr"`
}

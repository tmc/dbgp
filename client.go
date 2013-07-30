// package dbgp implements the dbgp client protocol
package dbgp

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"strings"

	"io"
)

type Cmd struct {
	Cmd  string
	Args []string
}

type Client struct {
	r *bufio.Reader
	w *bufio.Writer
}

var nul = []byte{0}

var protocolVersion = 18

// Creates a new DBGP client from an io.ReadWriter
func NewClient(c io.ReadWriter) *Client {
	return &Client{bufio.NewReader(c), bufio.NewWriter(c)}
}

// Initializes connection with the server
func (c *Client) Init() error {
	return c.sendXml(xml_init{xml.Name{}, "(appid)", "(idekey)", "(session)",
	"(thread)", "(parent)", "(lang)", "1.0", "file:///tmp/foo"})
}

// Consumes the next command from the server
func (c *Client) Next() (Cmd, error) {
	s, err := c.r.ReadString('\n')
	if err != nil {
		return Cmd{}, err
	}
	bits := strings.Split(s, " ")
	return Cmd{bits[0], bits[1:]}, nil
}

func (c *Client) sendXml(v interface{}) error {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	length := len(b) + len(xml.Header)
	c.w.WriteString(fmt.Sprint(length))
	c.w.Write(nul)
	c.w.WriteString(xml.Header)
	_, err = c.w.Write(b)
	if err != nil {
		return err
	}
	c.w.Write(nul)
	return c.w.Flush()
}

// Encodes an init messages
//
//<init appid="APPID"
//      idekey="IDE_KEY"
//      session="DBGP_COOKIE"
//      thread="THREAD_ID"
//      parent="PARENT_APPID"
//      language="LANGUAGE_NAME"
//      protocol_version="1.0"
//      fileuri="file://path/to/file">
type xml_init struct {
	XMLName         xml.Name `xml:"init"`
	AppId           string   `xml:"appid,attr"`
	IdeKey          string   `xml:"idekey,attr"`
	Session         string   `xml:"session,attr"`
	Thread          string   `xml:"thread,attr"`
	Parent          string   `xml:"parent,attr"`
	Language        string   `xml:"language,attr"`
	ProtocolVersion string   `xml:"protocol_version,attr"`
	FileURI         string   `xml:"fileuri,attr"`
}

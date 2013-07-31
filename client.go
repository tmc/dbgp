// Package dbgp implements the dbgp client protocol
//
// see https://github.com/derickr/dbgp/blob/master/debugger_protocol.rst
//
// status: pre-alpha
package dbgp

// The DBGPClient interface captures what a client implementation must provide
type DBGPClient interface {
	Init() InitResponse
	Status() string                                    // Return status, one of ("starting", "stopping", "running", "break")
	Features() Features                                // Return supported features (called after Init())
	StepInto() (status string, reason string)          // Step the debugger into the program. State being one of ("starting", "stopping", "running", "break"), and reason one of ("ok, "error", "aborted", "exception")
	StepOver() (status string, reason string)          // Step over the program. State being one of ("starting", "stopping", "running", "break"), and reason one of ("ok, "error", "aborted", "exception")
	StackDepth() int                                   // Return the maximum stack depth
	StackGet(depth int) ([]Stack, error)               // Return one or more Stack elements based on the requested depth
	ContextNames(depth int) ([]Context, error)         // Return the relevant Contexts
	ContextGet(depth, context int) ([]Property, error) // Return the properties assocaited with the specified stack depth and context

	PropertyGet(depth, context int, name string) (string, error) // Return the value for a property

	BreakpointSet(btType, fileName string, line int) (Breakpoint, error) // Set a breakpoint
}

type Features struct {
	Supports_async bool
	Language_name  string
}

type Breakpoint struct {
	ID    int    `xml:"breakpoint_id,attr"`
	State string `xml:"state,attr"`
}

type InitResponse struct {
	AppID    string `xml:"appid,attr"`
	IDeKey   string `xml:"idekey,attr"`
	Session  string `xml:"session,attr"`
	Thread   string `xml:"thread,attr"`
	Parent   string `xml:"parent,attr"`
	Language string `xml:"language,attr"`
	FileURI  string `xml:"fileuri,attr"`
}

type Stack struct {
	Level    int    `xml:"level,attr"`    // the stack depth of this stack element
	Type     string `xml:"type,attr"`     // the type of stack frame. Valid values are "file" or "eval"
	Filename string `xml:"filename,attr"` // absolute file URI in the local filesystem
	Lineno   int    `xml:"lineno,attr"`   // 1-based line offset into the buffer
	Where    string `xml:"where,attr"`    // current command name (optional)
	cmdBegin string `xml:"cmdbegin,attr"` // (line number):(text offset) from beginning of line for the current instruction (optional)
	cmdEnd   string `xml:"cmdend,attr"`   // same as CmdBegin, denotes end of current instruction
}

type Property struct {
	Name        string `xml:"name,attr"`      // Short variable name.
	Fullname    string `xml:"fullname,attr"`  // Long variable name. This is the long form of the name which can be eval'd by the language to retrieve the value of the variable.
	Classname   string `xml:"classname,attr"` // If the type is an object or resource, then the debugger engine MAY specify the class name This is an optional attribute.
	Type        string `xml:"type,attr"`      // language specific data type name
	page        string // if not all the children in the first level are returned, then the page attribute, in combination with the pagesize attribute will define where in the array or object these children should be located. The page number is 0-based.
	pageSize    string // the size of each page of data, defaulted by the debugger engine, or negotiated with feature_set and max_children. Required when the page attribute is available.
	facet       string // provides a hint to the IDE about additional facets of this value. These are space separated names, such as private, protected, public, constant, etc.
	size        string // size of property data in bytes
	children    bool   // true/false whether the property has children this would be true for objects or array's.
	numChildren int    // optional attribute with number of children for the property.
	key         string // language dependent reference for the property. if the key is available, the IDE SHOULD use it to retrieve further data for the property, optional
	Address     string `xml:"address,attr"` // containing physical memory address, optional
	encoding    string // if this is binary data, it should be base64 encoded with this attribute set
}

type Context struct {
	Name string `xml:"name,attr"`
	ID   int    `xml:"id,attr"`
}

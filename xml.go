package turborel

// XML values [CLIENT XML values]: a value comes as its serialization as
// CONTENT, which Text gives and Nodes parses for its nodes; a parameter
// of an XML type is made of its text.

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// sqlstateInvalidXMLContent is the SQLSTATE of text that is no XML
// content: class 22, data exception, subclass 00N.
const sqlstateInvalidXMLContent = "2200N"

// XMLNode is a node of an XML value: [XMLElement], [XMLText],
// [XMLComment] or [XMLProcInst].
type XMLNode interface {
	isXMLNode()
}

// XMLElement is an element, its name resolved to its namespace, its
// attributes, and its children in order.
type XMLElement struct {
	Name       xml.Name
	Attributes []xml.Attr
	Children   []XMLNode
}

// XMLText is character data.
type XMLText string

// XMLComment is a comment's text.
type XMLComment string

// XMLProcInst is a processing instruction.
type XMLProcInst struct {
	Target string
	Inst   string
}

func (XMLElement) isXMLNode()  {}
func (XMLText) isXMLNode()     {}
func (XMLComment) isXMLNode()  {}
func (XMLProcInst) isXMLNode() {}

// Text gives the value's serialization as CONTENT, as the server sent it.
func (x XML) Text() string { return string(x) }

// Nodes parses the value as CONTENT, its white space kept, for its
// nodes. It fails, as an [*Error] of SQLSTATE 2200N, if the text is not
// XML content.
func (x XML) Nodes() ([]XMLNode, error) {
	d := xml.NewDecoder(strings.NewReader(string(x)))

	nodes, err := readNodes(d, nil)
	if err != nil {
		return nil, &Error{
			SQLCode: sqlcodeData, SQLState: sqlstateInvalidXMLContent, Message: "not XML content: " + err.Error(), cause: err,
		}
	}

	return nodes, nil
}

// The ends of an XML text: its own, and one within an element its
// content never closes.
var (
	errTextEnd  = errors.New("the text has ended")
	errUnclosed = errors.New("an element is not closed")
)

// readNodes reads nodes until the end of the element open, or of the text
// if open is nil.
func readNodes(d *xml.Decoder, open *xml.StartElement) ([]XMLNode, error) {
	nodes := []XMLNode{}

	for {
		token, err := nextToken(d, open)
		if errors.Is(err, errTextEnd) {
			return nodes, nil
		}

		if err != nil {
			return nil, err
		}

		node, end, err := nodeOf(d, token)
		if err != nil {
			return nil, err
		}

		if end {
			return nodes, nil
		}

		if node != nil {
			nodes = append(nodes, node)
		}
	}
}

// nextToken gives the next token: errTextEnd at the text's end, where
// open is nil; at its end within an element, a failure.
func nextToken(d *xml.Decoder, open *xml.StartElement) (xml.Token, error) {
	token, err := d.Token()

	switch {
	case err == nil:
		return token, nil
	case errors.Is(err, io.EOF) && open == nil:
		return nil, errTextEnd
	case errors.Is(err, io.EOF):
		return nil, errUnclosed
	default:
		return nil, err //nolint:wrapcheck // Nodes gives the driver's failure of it.
	}
}

// nodeOf makes the node token begins; true at its element's end.
func nodeOf(d *xml.Decoder, token xml.Token) (XMLNode, bool, error) {
	switch t := token.(type) {
	case xml.StartElement:
		start := t.Copy()

		children, err := readNodes(d, &start)
		if err != nil {
			return nil, false, err
		}

		return XMLElement{Name: start.Name, Attributes: start.Attr, Children: children}, false, nil
	case xml.EndElement:
		return nil, true, nil
	case xml.CharData:
		return XMLText(t), false, nil
	case xml.Comment:
		return XMLComment(t), false, nil
	case xml.ProcInst:
		return XMLProcInst{Target: t.Target, Inst: string(t.Inst)}, false, nil
	default:
		return nil, false, nil
	}
}

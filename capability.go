package turborel

// What a connection asks for as it opens [PROTOCOL Capabilities]
// [CLIENT What a connection asks for]: the capabilities, by name, and the
// connection's attributes. The server grants those it has; a request or
// tail field of a capability not granted is never sent.

import "slices"

// The capabilities, in the order of the protocol's table, the order the
// server grants them in.
const (
	CapabilityNotices              = "notices"
	CapabilityStatus               = "status"
	CapabilityDiagnostics          = "diagnostics"
	CapabilityDescribe             = "describe"
	CapabilityCursors              = "cursors"
	CapabilityPing                 = "ping"
	CapabilityReset                = "reset"
	CapabilityChangeUser           = "change-user"
	CapabilityQueryAttributes      = "query-attributes"
	CapabilityConnectionAttributes = "connection-attributes"
	CapabilityMultiStatement       = "multi-statement"
	CapabilityCompression          = "compression"
	CapabilityCommitPosition       = "commit-position"
	CapabilityCopy                 = "copy"
	CapabilityRowCounts            = "row-counts"
	CapabilityGeneratedValues      = "generated-values"
	CapabilityFetchEnds            = "fetch-ends"
	CapabilityConditions           = "conditions"
	CapabilityDeclaredTypes        = "declared-types"
	CapabilityParameterModes       = "parameter-modes"
	CapabilityDebugger             = "debugger"
	CapabilityColumns              = "columns"
)

// AttributeApplication is the connection attribute that names the
// program, shown in INFORMATION_SCHEMA.CONNECTIONS.
const AttributeApplication = "application"

// Capabilities gives every capability, in the protocol's order.
func Capabilities() []string {
	return []string{
		CapabilityNotices, CapabilityStatus, CapabilityDiagnostics, CapabilityDescribe,
		CapabilityCursors, CapabilityPing, CapabilityReset, CapabilityChangeUser,
		CapabilityQueryAttributes, CapabilityConnectionAttributes, CapabilityMultiStatement,
		CapabilityCompression, CapabilityCommitPosition, CapabilityCopy, CapabilityRowCounts,
		CapabilityGeneratedValues, CapabilityFetchEnds, CapabilityConditions, CapabilityDeclaredTypes,
		CapabilityParameterModes, CapabilityDebugger, CapabilityColumns,
	}
}

// Offer is what a connection asks of the server as it opens: the
// capabilities, its attributes, and the threshold from which it
// compresses its frames once compression is granted.
type Offer struct {
	Capabilities []string
	Attributes   []Attribute
	CompressFrom int
}

// DefaultOffer asks for every capability but compression, as
// turborel-client does, with no attribute.
func DefaultOffer() Offer {
	all := Capabilities()

	return Offer{
		Capabilities: slices.DeleteFunc(all, func(c string) bool { return c == CapabilityCompression }),
		CompressFrom: defaultCompressionThreshold,
	}
}

// NoOffer asks for no capability: the server answers in the protocol's
// first form, byte for byte.
func NoOffer() Offer {
	return Offer{CompressFrom: defaultCompressionThreshold}
}

// Compressed is the offer asking for compression too.
func (o Offer) Compressed() Offer {
	if !slices.Contains(o.Capabilities, CapabilityCompression) {
		o.Capabilities = append(slices.Clone(o.Capabilities), CapabilityCompression)
	}

	return o
}

// WithAttribute is the offer carrying the connection attribute name, of
// value; the server shows it in INFORMATION_SCHEMA.CONNECTION_ATTRIBUTES.
func (o Offer) WithAttribute(name, value string) Offer {
	o.Attributes = append(slices.Clone(o.Attributes), Attribute{Name: name, Value: value})

	return o
}

// granted is the set of capabilities a server granted.
type granted map[string]bool

// grantedOf is the set of capabilities Ready named.
func grantedOf(names []string) granted {
	set := granted{}
	for _, name := range names {
		set[name] = true
	}

	return set
}

package turborel

import (
	"bytes"
	"maps"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

// expectedReplies is the reply of each server sample, as its meaning
// gives it [PROTOCOL Sample frames].
func expectedReplies(t *testing.T) map[string]reply {
	t.Helper()

	replies := sessionReplies()
	for _, more := range []map[string]reply{
		completionReplies(), statusReplies(), columnReplies(), typeReplies(), moreTypeReplies(), debugReplies(),
		valueReplies(t), moreValueReplies(t),
	} {
		maps.Copy(replies, more)
	}

	return replies
}

// oneColumn is a Description of one column C, nullable, of typ.
func oneColumn(typ DataType) reply {
	return descriptionReply{columns: []Column{{Named: true, Name: "C", Type: typ}}}
}

// oneValue is Rows of one row of one value.
func oneValue(v Value) reply {
	return rowsReply{rows: []Row{{v}}}
}

// sessionReplies is the reply of each sample of logging in, the
// statements' first replies, large objects and Pong.
func sessionReplies() map[string]reply {
	varying := Parameter{Varying: true, Type: VaryingType{Length: 1<<15 - 1}, Mode: ModeIn}
	input := Parameter{Type: ExactType{Precision: 10}, Mode: ModeIn}
	integer := ExactType{Precision: 10}
	k := Column{Named: true, Name: "K", Type: integer, NotNull: true}
	columns := []Column{k, {Type: VaryingType{Length: 5}}}
	declared := k
	declared.Declared = &Declared{"INTEGER", "INTEGER"}

	return map[string]reply{
		"challenge": challengeReply{challenge: []byte("r=abcdef,s=c2FsdA==,i=4096")},
		"ready": readyReply{
			authorization: "ANN", key: [keyBytes]byte(bytes.Repeat([]byte{0xA5}, keyBytes)),
			capabilities: []string{CapabilityPing}, server: "turborel 0.0.0 (alpha)",
		},
		"prepared": preparedReply{
			statement: 1, parameters: []Parameter{varying, input}, columns: columns,
		},
		"prepared-empty": preparedReply{statement: 2},
		"prepared-declared": preparedReply{
			statement: 1, parameters: []Parameter{varying, input}, columns: []Column{declared},
		},
		"prepared-called":   calledReply(),
		"description":       descriptionReply{columns: columns},
		"description-empty": descriptionReply{},
		"rows":              rowsReply{rows: []Row{{DecimalOf(1, 0), Char("a")}, {Null{}, Char("")}}},
		"rows-none":         rowsReply{},
		"rows-empty-row":    rowsReply{rows: []Row{{}}},
		"rows-compressed":   compressedRows(),
		"suspended":         suspendedReply{},
		"lob-data":          lobDataReply{data: []byte("aé")},
		"lob-data-empty":    lobDataReply{data: []byte{}},
		"lob-locator":       lobLocatorReply{locator: 4},
		"notice":            noticeReply{notice: Notice{SQLState: "01004", Message: "string data, right truncation"}},
		"notice-diagnostics": noticeReply{notice: Notice{
			SQLState: "01004", Message: "string data, right truncation",
			Diagnostics: Diagnostics{Severity: "WARNING", Detail: "cut to 4"},
		}},
		"pong": pongReply{},
	}
}

// calledReply is the Prepared of CALL S.P(?, ?) the prepared-called
// sample holds: an input, an output column G, and tail 20 naming S.P, IN
// of parameter 1 and OUT of parameter 2.
func calledReply() reply {
	integer := ExactType{Precision: 10}

	return preparedReply{
		statement:  3,
		parameters: []Parameter{{Type: integer, Mode: ModeIn}},
		columns:    []Column{{Named: true, Name: "G", Type: integer, NotNull: true}},
		called: &called{procedure: QualifiedName{"S", "P"}, parameters: []Parameter{
			{Type: integer, Mode: ModeIn, Ordinal: 1}, {Type: integer, Mode: ModeOut, Ordinal: 2},
		}},
	}
}

// debugReplies is the reply of each sample of the debugger.
func debugReplies() map[string]reply {
	raisePay := QualifiedName{"ANN", "RAISE_PAY"}
	set := []Breakpoint{{Number: 1, Routine: raisePay, Line: 4}}
	pause := &Pause{Breakpoint: 1, Frames: []StackFrame{{
		Origin:  &Origin{Kind: RoutineProcedure, Specific: raisePay, Name: raisePay},
		Visible: true, Place: 1, Placed: true, Line: 4, Column: 3,
		Parameters: []Variable{{Name: "PCT", Type: "INTEGER", Value: DecimalOf(10, 0)}},
		Variables:  []Variable{{Name: "N", Type: "INTEGER", Value: Null{}}},
		Cursors:    []FrameCursor{{Name: "C", Query: "SELECT ID FROM STAFF"}},
	}}}

	return map[string]reply{
		"debugged-connection":  debuggedReply{what: debuggedConnection, connection: 2},
		"debugged-breakpoint":  debuggedReply{what: debuggedBreakpoint, breakpoints: set},
		"debugged-breakpoints": debuggedReply{what: debuggedBreakpoints, breakpoints: set},
		"debugged-running":     debuggedReply{what: debuggedStack},
		"debugged-stack":       debuggedReply{what: debuggedStack, pause: pause},
		"event-paused":         eventReply{event: DebugEvent{Kind: EventPaused, Pause: pause}},
		"event-timed-out":      eventReply{event: DebugEvent{Kind: EventTimedOut}},
		"event-cancelled":      eventReply{event: DebugEvent{Kind: EventCancelled}},
		"event-ended":          eventReply{event: DebugEvent{Kind: EventEnded}},
	}
}

// compressedRows is the Rows the compressed server sample holds: 64
// rows, 0 to 63 and abcdefgh four times.
func compressedRows() reply {
	rows := make([]Row, 64)
	for i := range rows {
		rows[i] = Row{DecimalOf(int64(i), 0), Char(strings.Repeat("abcdefgh", 4))}
	}

	return rowsReply{rows: rows}
}

// completionReplies is the reply of each sample of Complete and Error.
func completionReplies() map[string]reply {
	unique := func(active bool) *Error {
		return &Error{
			SQLCode: -803, SQLState: "23505", Message: "a unique constraint is broken",
			Rule: "ISO/IEC 9075-2:2023 §11.7", Placed: true, Position: 12, Active: active,
		}
	}
	diagnosed := unique(true)
	diagnosed.Diagnostics = &Diagnostics{
		Severity: "ERROR", Detail: "key (K)=(1)", Hint: "choose another K", Context: "trigger S.TR",
		Schema: "S", Table: "T", Column: "K", DataType: "INTEGER", Constraint: "T_PK",
	}
	truncation := &Error{SQLState: "01004", Message: "string data, right truncation", Active: true}

	return map[string]reply{
		"complete":          completeReply{Completion{SQLState: "00000"}},
		"complete-active":   completeReply{Completion{SQLState: "00000", Active: true}},
		"complete-no-data":  completeReply{Completion{SQLCode: 100, SQLState: "02000"}},
		"complete-position": completeReply{Completion{SQLState: "00000", Position: 4711, Committed: true}},
		"complete-more":     completeReply{Completion{SQLState: "00000", More: true}},
		"complete-rows":     completeReply{Completion{SQLState: "00000", Rows: 1, Counted: true}},
		"complete-every": completeReply{Completion{
			SQLState: "00000", Position: 4711, Committed: true, More: true, Rows: 3, Counted: true,
			Generated: []string{"7", "8"}, Time: &Timing{Total: 1500, Queued: 20, Locks: 300},
		}},
		"complete-conditions": completeReply{Completion{
			SQLState: "01004", Active: true, Conditions: []*Error{truncation},
		}},
		"error": errorReply{failure: &Error{
			SQLCode: -803, SQLState: "23505", Message: "a unique constraint is broken",
		}},
		"error-cited":       errorReply{failure: unique(true)},
		"error-diagnostics": errorReply{failure: diagnosed},
		"error-connection": errorReply{failure: &Error{
			SQLCode: sqlcodeConnection, SQLState: sqlstateConnectionFailure, Message: "a malformed message",
		}},
	}
}

// statusReplies is the reply of each sample of Status.
func statusReplies() map[string]reply {
	return map[string]reply{
		"status-server-version":   statusReply{StatusServerVersion, "turborel 0.0.0 (alpha)"},
		"status-authorization":    statusReply{StatusAuthorization, "ANN"},
		"status-schema":           statusReply{StatusSchema, "ANN"},
		"status-read-only":        statusReply{StatusReadOnly, "NO"},
		"status-server-read-only": statusReply{StatusServerReadOnly, "YES"},
		"status-catalog":          statusReply{StatusCatalog, "TURBOREL"},
		"status-owner":            statusReply{StatusOwner, "YES"},
		"status-time-zone":        statusReply{"SESSION.TIME_ZONE", "+05:30"},
		"status-null":             statusReply{"STATEMENT.TIMEOUT_MS", ""},
	}
}

// columnReplies is the reply of each sample of declared types.
func columnReplies() map[string]reply {
	declared := func(name string, typ DataType, notNull bool, d Declared) Column {
		return Column{Named: true, Name: name, Type: typ, NotNull: notNull, Declared: &d}
	}

	return map[string]reply{
		"description-declared": descriptionReply{columns: []Column{
			declared("I", ExactType{Precision: 10}, true, Declared{"INTEGER", "INTEGER"}),
			declared("S", ExactType{Precision: 5}, false, Declared{"SMALLINT", "SMALLINT"}),
			declared("B", ExactType{Precision: 18}, false, Declared{"BIGINT", "BIGINT"}),
			declared("D", ExactType{Precision: 9, Scale: 2}, false, Declared{"DECIMAL", "DECIMAL(9,2)"}),
			declared("R", ApproximateType{Precision: 24}, false, Declared{"REAL", "REAL"}),
			declared("P", ApproximateType{Precision: 53}, false, Declared{"DOUBLE PRECISION", "DOUBLE PRECISION"}),
			declared("M", ExactType{Precision: 9, Scale: 2}, false, Declared{"USER-DEFINED", "S.MONEY"}),
		}},
		"description-declared-array": descriptionReply{columns: []Column{declared(
			"C", ArrayType{Max: 3, Element: ExactType{Precision: 10}}, false, Declared{"ARRAY", "INTEGER ARRAY[3]"},
		)}},
	}
}

// typeReplies is the reply of each sample of a data type of a string,
// a number or a datetime.
func typeReplies() map[string]reply {
	return map[string]reply{
		"description-character":        oneColumn(CharacterType{Length: 4}),
		"description-character-octets": oneColumn(CharacterType{Length: 16, Units: Octets}),
		"description-exact":            oneColumn(ExactType{Precision: 9, Scale: 2}),
		"description-approximate":      oneColumn(ApproximateType{Precision: 53}),
		"description-plugin-type":      oneColumn(PluginType{Name: "ST_GEOMETRY"}),
		"description-varying":          oneColumn(VaryingType{Length: 20}),
		"description-binary":           oneColumn(BinaryType{Length: 16}),
		"description-binary-varying":   oneColumn(VarBinaryType{Length: 255}),
		"description-date":             oneColumn(DateType{}),
		"description-time":             oneColumn(TimeType{Precision: 3}),
		"description-time-zoned":       oneColumn(TimeType{Zoned: true}),
		"description-timestamp":        oneColumn(TimestampType{Precision: 6}),
		"description-timestamp-zoned":  oneColumn(TimestampType{Precision: 6, Zoned: true}),
		"description-interval-year-month": oneColumn(IntervalType{
			Qualifier: Qualifier{Start: Year, End: Month, Leading: 4},
		}),
		"description-interval-day-second": oneColumn(IntervalType{
			Qualifier: Qualifier{Start: Day, End: Second, Leading: 2, Fraction: 6},
		}),
		"description-boolean":  oneColumn(BooleanType{}),
		"description-decfloat": oneColumn(DecFloatType{Precision: 34}),
		"description-clob":     oneColumn(CLOBType{Length: 1 << 20}),
		"description-blob":     oneColumn(BLOBType{Length: 1 << 16, Units: Octets}),
		"description-json":     oneColumn(JSONType{}),
		"description-uuid":     oneColumn(UUIDType{}),
		"description-distinct": oneColumn(VaryingType{Length: 4}),
	}
}

// moreTypeReplies is the reply of each sample of a composite, a
// user-defined or an XML type.
func moreTypeReplies() map[string]reply {
	shape := StructuredType{Name: UDTName{"S", "SHAPE"}, Supertypes: []UDTName{{"S", "FIGURE"}}}
	namespace := "urn:po"

	return map[string]reply{
		"description-array": oneColumn(ArrayType{Max: 3, Element: BooleanType{}}),
		"description-row": oneColumn(RowType{Fields: []Field{
			{Name: "X", Type: VaryingType{Length: 3}}, {Name: "Y", Type: BooleanType{}},
		}}),
		"description-multiset":   oneColumn(MultisetType{Element: CharacterType{Length: 2}}),
		"description-structured": oneColumn(shape),
		"description-reference":  oneColumn(ReferenceType{Referenced: shape}),
		"description-reference-scoped": oneColumn(ReferenceType{
			Referenced: shape, Scope: &UDTName{"S", "SHAPES"},
		}),
		"description-xml-document": oneColumn(XMLType{}),
		"description-xml-content":  oneColumn(XMLType{Primary: XMLContent, Secondary: XMLAny}),
		"description-xml-sequence": oneColumn(XMLType{Primary: XMLSequence}),
		"description-xml-schema": oneColumn(XMLType{
			Secondary: XMLXMLSchema, Schema: &XMLSchemaName{Schema: "S", Name: "PO", Namespace: &namespace},
		}),
	}
}

// moment is the instant of a datetime as the protocol carries it:
// microseconds from 0001-01-01 00:00 UTC.
func moment(at time.Time) int64 {
	return at.UnixMicro() + unixEpochDays*microsPerDay
}

// sinceMidnight is a time's microseconds from midnight.
func sinceMidnight(hour, minute, second, micro int) int64 {
	return time.Date(1970, 1, 1, hour, minute, second, micro*int(time.Microsecond), time.UTC).UnixMicro()
}

// valueReplies is the reply of each sample of a value of a string, a
// number or a datetime.
func valueReplies(t *testing.T) map[string]reply {
	t.Helper()

	largest, err := NewDecimal(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1)), 0)
	yearMonth, err2 := NewInterval(Qualifier{Start: Year, End: Month, Leading: 2}, big.NewInt(-14))
	daySecond, err3 := NewInterval(Qualifier{Start: Day, End: Second, Leading: 2, Fraction: 6}, big.NewInt(93784500000))
	decfloat, err4 := NewDecFloat(big.NewInt(-15), -1)
	noFailure(t, err, err2, err3, err4)

	day := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	noon := time.Date(2024, 2, 29, 12, 34, 56, 123456000, time.UTC)
	pacific := time.Date(2024, 2, 29, 20, 34, 56, 0, time.UTC)

	return map[string]reply{
		"value-null":                      oneValue(Null{}),
		"value-character":                 oneValue(Char("abc")),
		"value-character-empty":           oneValue(Char("")),
		"value-character-utf8":            oneValue(Char("é€")),
		"value-exact":                     oneValue(DecimalOf(12345, 2)),
		"value-exact-negative":            oneValue(DecimalOf(-1, 0)),
		"value-exact-largest":             oneValue(largest),
		"value-approximate":               oneValue(Approx(1.5)),
		"value-approximate-negative-zero": oneValue(Approx(math.Copysign(0, -1))),
		"value-binary":                    oneValue(Binary{0xDE, 0xAD, 0xBE, 0xEF}),
		"value-binary-empty":              oneValue(Binary{}),
		"value-date":                      oneValue(Datetime{Moment: MomentDate, UTC: moment(day)}),
		"value-time": oneValue(Datetime{
			Moment: MomentTime, UTC: sinceMidnight(12, 34, 56, 500000), Precision: 1,
		}),
		"value-time-zoned": oneValue(Datetime{
			Moment: MomentTime, UTC: sinceMidnight(7, 4, 56, 0), Zone: 330, Zoned: true,
		}),
		"value-timestamp": oneValue(Datetime{Moment: MomentTimestamp, UTC: moment(noon), Precision: 6}),
		"value-timestamp-zoned": oneValue(Datetime{
			Moment: MomentTimestamp, UTC: moment(pacific), Zone: -480, Zoned: true,
		}),
		"value-timestamp-earliest":  oneValue(Datetime{Moment: MomentTimestamp}),
		"value-interval-year-month": oneValue(yearMonth),
		"value-interval-day-second": oneValue(daySecond),
		"value-true":                oneValue(Bool(true)),
		"value-false":               oneValue(Bool(false)),
		"value-decfloat":            oneValue(decfloat),
	}
}

// moreValueReplies is the reply of each sample of any other value.
func moreValueReplies(t *testing.T) map[string]reply {
	t.Helper()

	geometry := []byte{
		0x1A, 0, 0, 0, 0x01, 0xE6, 0x10, 0, 0, 0x01, 0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xF0, 0x3F, 0, 0, 0, 0, 0, 0, 0, 0x40,
	}
	documentJSON, err := NewJSON(JSONObject{{Key: "a", Value: JSONArray{DecimalOf(1, 0), Char("x")}}})
	scalars, err2 := NewJSON(JSONArray{
		JSONNull{}, Bool(true), Bool(false), DecimalOf(-2, 0), DecimalOf(150, 2), Char(""), JSONObject{},
	})
	uuid, err3 := ParseUUID("00112233-4455-6677-8899-aabbccddeeff")
	noFailure(t, err, err2, err3)

	return map[string]reply{
		"value-plugin":        oneValue(PluginValue{Type: "ST_GEOMETRY", Text: "POINT (1 2)", Bytes: geometry}),
		"value-array":         oneValue(Array{DecimalOf(1, 0), Null{}}),
		"value-array-empty":   oneValue(Array{}),
		"value-row":           oneValue(Row{Char("x"), Bool(true)}),
		"value-lob-character": oneValue(LOB{Kind: CharacterLOB, Octets: []byte("aé")}),
		"value-lob-binary":    oneValue(LOB{Kind: BinaryLOB, Octets: []byte{0x00, 0xFF}}),
		"value-lob-locator": oneValue(Locator{
			Kind: CharacterLOB, Locator: 7, Octets: 300000, Characters: 200000,
		}),
		"value-multiset": oneValue(Multiset{Char("a"), Char("a")}),
		"value-structured": oneValue(Structured{
			Type: UDTName{"S", "SHAPE"}, Attributes: []Value{DecimalOf(3, 0), Null{}},
		}),
		"value-reference":    oneValue(Reference{1, 2, 3, 4, 5, 6, 7, 8}),
		"value-json":         oneValue(documentJSON),
		"value-json-scalars": oneValue(scalars),
		"value-xml":          oneValue(XML("<a>&lt;</a>")),
		"value-uuid":         oneValue(uuid),
	}
}

// noFailure fails the test of any error of errs.
func noFailure(t *testing.T, errs ...error) {
	t.Helper()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

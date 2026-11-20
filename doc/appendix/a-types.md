# Appendix A: Types

[Contents](README.md)

Each data type of the native protocol, by its tag, and the Go type the
driver describes it by; then each value tag, and the Go type the driver
gives and takes for it. Every type of the first table and every value
of the second implements the package's `DataType` or `Value`, so a
program tells them apart by a type switch. The protocol's layouts are
those of [The Native Protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md),
Values and fields.

## Data types

| Tag | SQL type | Go type | Its fields |
|---|---|---|---|
| 1 | CHARACTER | `CharacterType` | `Length`, `Units` |
| 2 | exact numeric | `ExactType` | `Precision`, `Scale` |
| 3 | approximate numeric | `ApproximateType` | `Precision` |
| 4 | a plug-in's type | `PluginType` | `Name`, `Params`, at most two |
| 5 | CHARACTER VARYING | `VaryingType` | `Length`, `Units` |
| 6 | BINARY | `BinaryType` | `Length` |
| 7 | BINARY VARYING | `VarBinaryType` | `Length` |
| 8 | DATE | `DateType` | `Precision` |
| 9, 10 | TIME, WITH TIME ZONE | `TimeType` | `Precision`, `Zoned` |
| 11, 12 | TIMESTAMP, WITH TIME ZONE | `TimestampType` | `Precision`, `Zoned` |
| 13 | INTERVAL | `IntervalType` | `Qualifier`: `Start` and `End`, `Year` to `Second`; `Leading`, `Fraction` |
| 14 | BOOLEAN | `BooleanType` | |
| 15 | DECFLOAT | `DecFloatType` | `Precision` |
| 16 | array | `ArrayType` | `Max`, 1 to 65535; `Element` |
| 17 | row | `RowType` | `Fields`, one or more, each a `Name` and a `Type` |
| 18 | CHARACTER LARGE OBJECT | `CLOBType` | `Length`, `Units` |
| 19 | BINARY LARGE OBJECT | `BLOBType` | `Length`, `Units` |
| 22 | multiset | `MultisetType` | `Element` |
| 23 | structured type | `StructuredType` | `Name`, `Supertypes`, each a `UDTName` |
| 24 | reference type | `ReferenceType` | `Referenced`, `Scope`, nil if unscoped |
| 25 | JSON | `JSONType` | |
| 26 | XML | `XMLType` | `Primary`, `Secondary`, `Schema` of XMLSCHEMA |
| 27 | UUID | `UUIDType` | |

A length's `Units` are `Characters` or `Octets`. A distinct type comes as
its source type. Types and values nest at most 16 deep.

## Values

| Tag | Value | Go type | Bounds |
|---|---|---|---|
| 0 | null | `Null` | |
| 1 | character string, of either character type; tag 5 read as 1 | `Char` | UTF-8 |
| 2 | exact number | `Decimal` | units, `Units()`, a `*big.Int` from −2¹²⁷ to 2¹²⁷ − 1; `Scale()`, a `uint32` |
| 3 | approximate number | `Approx` | a `float64`, its bits kept, NaN and −0.0 among them |
| 4 | a plug-in's value | `PluginValue` | `Type`, `Text`, `Bytes`, sent back as it came |
| 6 | binary string; tag 7 read as 6 | `Binary` | |
| 8, 9, 11 | date, time, timestamp; tags 10 and 12 read as 9 and 11, the zoned byte saying WITH TIME ZONE | `Datetime` | `Moment`; `UTC`, microseconds in UTC; `Zone`, the displacement in minutes; `Zoned`; `Precision`; `Time()` gives a `time.Time` |
| 13 | interval | `Interval` | `Qualifier`; `Units()`, months or microseconds, an i128 |
| 14 | truth value | `Bool` | unknown is `Null` |
| 15 | decimal floating point | `DecFloat` | `Coefficient()`, at most 34 digits, no trailing zero; `Exponent()`, its power of ten, the leading digit's within −6143 and 6144 |
| 16 | array | `Array` | |
| 17 | row | `Row` | |
| 20 | large object, whole | `LOB` | `Kind`, `CharacterLOB` or `BinaryLOB`; `Octets`, UTF-8 of a character one |
| 21 | large object, by locator | `Locator` | `Kind`, `Locator`, `Octets`, `Characters` |
| 22 | multiset | `Multiset` | at most 65535 elements |
| 23 | structured value | `Structured` | `Type`, `Attributes` |
| 24 | reference | `Reference` | |
| 25 | JSON | `JSON` | the stored form, checked whole; `Item()` gives a `JSONItem`, `NewJSON` makes one of it |
| 26 | XML | `XML` | its serialization as CONTENT |
| 27 | UUID | `UUID` | 16 octets; `String()` and `ParseUUID` its text |

`NewDecimal`, `NewDecFloat` and `NewInterval` refuse a number the
protocol cannot carry with an `*Error` of SQLSTATE 22003; `NewDecFloat`
first rounds a coefficient of more than 34 digits half to even, as
Turborel's DecFloat::new does. `JSONOf` refuses a JSON value that is
not one with SQLSTATE 22032, and `NewJSON` one past Turborel's limits,
nested deeper than 128 or stored in more than 256 MiB, with SQLCODE
-900, SQLSTATE 0A000.

## JSON items

A JSON value's item is a `JSONItem`: `JSONNull`, `Bool`, `Decimal`,
`DecFloat`, `Approx`, `Char`, `Datetime`, `JSONArray`, or `JSONObject`,
a list of `JSONMember`s in the order written, a key free to come more
than once. Arrays and objects nest at most 128 deep. An exact number is
stored in the narrowest of the protocol's three forms that holds it.

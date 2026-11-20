# Values

[Contents](README.md)

## NAME

`Value`, `DataType`: the values the low-level client gives and takes,
and the types of columns and parameters.

## SYNOPSIS

```go
type Value interface{ /* sealed */ }
type DataType interface{ /* sealed */ }

func NewDecimal(units *big.Int, scale uint32) (Decimal, error)
func DecimalOf(units int64, scale uint32) Decimal
func NewDecFloat(coefficient *big.Int, exponent int32) (DecFloat, error)
func NewInterval(qualifier Qualifier, units *big.Int) (Interval, error)
func NewJSON(item JSONItem) (JSON, error)
func JSONOf(doc []byte) (JSON, error)
func ParseUUID(text string) (UUID, error)
```

## DESCRIPTION

A value of a row, or of a parameter, is one of the package's value
types, and a column's or a parameter's type one of its type types;
[Appendix A](../appendix/a-types.md) lists each against its protocol
tag, with its fields and bounds. Both interfaces are sealed: only the
package's own types implement them, so a type switch over them names
every case.

An exact number is a `Decimal`, which holds every units and scale the
protocol carries and gives its units as a `*big.Int`; its `String` writes
it as SQL does, as `-12.50`. A DECFLOAT is a `DecFloat`, kept in
Turborel's one form. A datetime is a `Datetime`, its instant in UTC
microseconds from 0001-01-01 with its displacement, whose `Time` gives
a `time.Time`. A JSON value is a `JSON`, its stored form checked whole,
whose `Item` gives its SQL/JSON item. A UUID is a `UUID`, whose `String`
gives its text.

The values a program gets come from the server's replies, and the
values it sends are written as the protocol says, the reply checked as
Turborel's own client checks it. This page is completed at milestone
M3, when statements give and take these values.

## ERRORS

| SQLSTATE | When |
|---|---|
| 22003 | `NewDecimal` or `NewInterval` of units outside a signed 128-bit integer; `NewDecFloat` of more than 34 significant digits, or out of DECFLOAT's range; `ParseUUID` of text that is no UUID |
| 22032 | `NewJSON` of an item nested deeper than 128, or `JSONOf` of octets that are not a stored JSON value |
| 08006 | A reply holding a value that is malformed, SQLCODE -919: the connection fails |

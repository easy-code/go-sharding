/*
 * Copyright 2021. Go-Sharding Author All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 *  File author: Anders Xiao
 */

package types

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/XiaoMi/Gaea/util/hack"
	"strconv"
)

// Value represents a typed value.
type Value struct {
	ValueType MySqlType
	Value     []byte
}

func (v *Value) Equals(value interface{}) bool {
	if vv, ok := value.(*Value); ok {
		return v.ValueType == vv.ValueType && bytes.Equal(v.Value, vv.Value)
	}
	return false
}

func (v *Value) Clone() *Value {
	if v == nil {
		return nil
	}

	var b []byte
	if v.Value != nil {
		b = make([]byte, len(v.Value))
		copy(b, v.Value)
	}

	return &Value{
		ValueType: v.ValueType,
		Value:     b,
	}
}

var (
	// NULL represents the NULL value.
	NULL = Value{}

	// DontEscape tells you if a character should not be escaped.
	DontEscape = byte(255)

	nullstr = []byte("null")

	// ErrIncompatibleTypeCast indicates a casting problem
	ErrIncompatibleTypeCast = errors.New("Cannot convert value to desired type")
)

// BinWriter interface is used for encoding values.
// Types like bytes.Buffer conform to this interface.
// We expect the writer objects to be in-memory buffers.
// So, we don't expect the write operations to fail.
type BinWriter interface {
	Write([]byte) (int, error)
}

// NewValue builds a Value using typ and val. If the value and typ
// don't match, it returns an error.
func NewValue(typ MySqlType, val []byte) (v Value, err error) {
	switch {
	case IsSigned(typ):
		if _, err := strconv.ParseInt(string(val), 0, 64); err != nil {
			return NULL, err
		}
		return MakeTrusted(typ, val), nil
	case IsUnsigned(typ):
		if _, err := strconv.ParseUint(string(val), 0, 64); err != nil {
			return NULL, err
		}
		return MakeTrusted(typ, val), nil
	case IsFloat(typ) || typ == Decimal:
		if _, err := strconv.ParseFloat(string(val), 64); err != nil {
			return NULL, err
		}
		return MakeTrusted(typ, val), nil
	case IsQuoted(typ) || typ == Bit || typ == Null:
		return MakeTrusted(typ, val), nil
	}
	// All other types are unsafe or invalid.
	return NULL, fmt.Errorf("invalid type specified for MakeValue: %v", typ)
}

// MakeTrusted makes a new Value based on the type.
// This function should only be used if you know the value
// and type conform to the rules. Every place this function is
// called, a comment is needed that explains why it's justified.
// Exceptions: The current package and mysql package do not need
// comments. Other packages can also use the function to create
// VarBinary or VarChar values.
func MakeTrusted(typ MySqlType, val []byte) Value {

	if typ == Null {
		return NULL
	}

	return Value{ValueType: typ, Value: val}
}

// NewInt64 builds an Int64 Value.
func NewInt64(v int64) Value {
	return MakeTrusted(Int64, strconv.AppendInt(nil, v, 10))
}

// NewInt8 builds an Int8 Value.
func NewInt8(v int8) Value {
	return MakeTrusted(Int8, strconv.AppendInt(nil, int64(v), 10))
}

// NewInt32 builds an Int64 Value.
func NewInt32(v int32) Value {
	return MakeTrusted(Int32, strconv.AppendInt(nil, int64(v), 10))
}

// NewUint64 builds an Uint64 Value.
func NewUint64(v uint64) Value {
	return MakeTrusted(Uint64, strconv.AppendUint(nil, v, 10))
}

// NewUint32 builds an Uint32 Value.
func NewUint32(v uint32) Value {
	return MakeTrusted(Uint32, strconv.AppendUint(nil, uint64(v), 10))
}

// NewFloat64 builds an Float64 Value.
func NewFloat64(v float64) Value {
	return MakeTrusted(Float64, strconv.AppendFloat(nil, v, 'g', -1, 64))
}

// NewFloat32 builds an Float32 Value.
func NewFloat32(v float32) Value {
	return MakeTrusted(Float32, strconv.AppendFloat(nil, float64(v), 'g', -1, 32))
}

// NewVarChar builds a VarChar Value.
func NewVarChar(v string) Value {
	return MakeTrusted(VarChar, []byte(v))
}

// NewVarBinary builds a VarBinary Value.
// The input is a string because it's the most common use case.
func NewVarBinary(v string) Value {
	return MakeTrusted(VarBinary, []byte(v))
}

// NewIntegral builds an integral type from a string representation.
// The type will be Int64 or Uint64. Int64 will be preferred where possible.
func NewIntegral(val string) (n Value, err error) {
	signed, err := strconv.ParseInt(val, 0, 64)
	if err == nil {
		return MakeTrusted(Int64, strconv.AppendInt(nil, signed, 10)), nil
	}
	unsigned, err := strconv.ParseUint(val, 0, 64)
	if err != nil {
		return Value{}, err
	}
	return MakeTrusted(Uint64, strconv.AppendUint(nil, unsigned, 10)), nil
}

// InterfaceToValue builds a value from a go type.
// Supported types are nil, int64, uint64, float64,
// string and []byte.
// This function is deprecated. Use the type-specific
// functions instead.
func InterfaceToValue(goval interface{}) (Value, error) {
	switch goval := goval.(type) {
	case nil:
		return NULL, nil
	case []byte:
		return MakeTrusted(VarBinary, goval), nil
	case int64:
		return NewInt64(goval), nil
	case uint64:
		return NewUint64(goval), nil
	case float64:
		return NewFloat64(goval), nil
	case string:
		return NewVarChar(goval), nil
	default:
		return NULL, fmt.Errorf("unexpected type %T: %v", goval, goval)
	}
}

// ToBytes returns the value as MySQL would return it as []byte.
// In contrast, Raw returns the internal representation of the Value, which may not
// match MySQL's representation for newer types.
// If the value is not convertible like in the case of Expression, it returns nil.
func (v Value) ToBytes() []byte {
	if v.ValueType == Expression {
		return nil
	}
	return v.Value
}

// Len returns the length.
func (v Value) Len() int {
	return len(v.Value)
}

// ToInt64 returns the value as MySQL would return it as a int64.
func (v Value) ToInt64() (int64, error) {
	if !v.IsIntegral() {
		return 0, ErrIncompatibleTypeCast
	}

	return strconv.ParseInt(v.ToString(), 10, 64)
}

// ToFloat64 returns the value as MySQL would return it as a float64.
func (v Value) ToFloat64() (float64, error) {
	if !IsNumber(v.ValueType) {
		return 0, ErrIncompatibleTypeCast
	}

	return strconv.ParseFloat(v.ToString(), 64)
}

// ToUint64 returns the value as MySQL would return it as a uint64.
func (v Value) ToUint64() (uint64, error) {
	if !v.IsIntegral() {
		return 0, ErrIncompatibleTypeCast
	}

	return strconv.ParseUint(v.ToString(), 10, 64)
}

// ToBool returns the value as a bool value
func (v Value) ToBool() (bool, error) {
	i, err := v.ToInt64()
	if err != nil {
		return false, err
	}
	switch i {
	case 0:
		return false, nil
	case 1:
		return true, nil
	}
	return false, ErrIncompatibleTypeCast
}

// ToString returns the value as MySQL would return it as string.
// If the value is not convertible like in the case of Expression, it returns nil.
func (v Value) ToString() string {
	if v.ValueType == Expression {
		return ""
	}
	return hack.String(v.Value)
}

// String returns a printable version of the value.
func (v Value) String() string {
	if v.ValueType == Null {
		return "NULL"
	}
	if v.IsQuoted() || v.ValueType == Bit {
		return fmt.Sprintf("%v(%q)", v.ValueType, v.Value)
	}
	return fmt.Sprintf("%v(%s)", v.ValueType, v.Value)
}

// EncodeSQL encodes the value into an SQL statement. Can be binary.
func (v Value) EncodeSQL(b BinWriter) error {
	switch {
	case v.ValueType == Null:
		_, err := b.Write(nullstr)
		return err
	case v.IsQuoted():
		return encodeBytesSQL(v.Value, b)
	case v.ValueType == Bit:
		return encodeBytesSQLBits(v.Value, b)
	default:
		_, err := b.Write(v.Value)
		return err
	}
}

// EncodeASCII encodes the value using 7-bit clean ascii bytes.
func (v Value) EncodeASCII(b BinWriter) {
	switch {
	case v.ValueType == Null:
		b.Write(nullstr)
	case v.IsQuoted() || v.ValueType == Bit:
		encodeBytesASCII(v.Value, b)
	default:
		b.Write(v.Value)
	}
}

// IsNull returns true if Value is null.
func (v Value) IsNull() bool {
	return v.ValueType == Null
}

// IsIntegral returns true if Value is an integral.
func (v Value) IsIntegral() bool {
	return IsIntegral(v.ValueType)
}

// IsSigned returns true if Value is a signed integral.
func (v Value) IsSigned() bool {
	return IsSigned(v.ValueType)
}

// IsUnsigned returns true if Value is an unsigned integral.
func (v Value) IsUnsigned() bool {
	return IsUnsigned(v.ValueType)
}

// IsFloat returns true if Value is a float.
func (v Value) IsFloat() bool {
	return IsFloat(v.ValueType)
}

// IsQuoted returns true if Value must be SQL-quoted.
func (v Value) IsQuoted() bool {
	return IsQuoted(v.ValueType)
}

// IsText returns true if Value is a collatable text.
func (v Value) IsText() bool {
	return IsText(v.ValueType)
}

// IsBinary returns true if Value is binary.
func (v Value) IsBinary() bool {
	return IsBinary(v.ValueType)
}

// IsDateTime returns true if Value is datetime.
func (v Value) IsDateTime() bool {
	dt := int(Datetime)
	return int(v.ValueType)&dt == dt
}

// MarshalJSON should only be used for testing.
// It's not a complete implementation.
func (v Value) MarshalJSON() ([]byte, error) {
	switch {
	case v.IsQuoted() || v.ValueType == Bit:
		return json.Marshal(v.ToString())
	case v.ValueType == Null:
		return nullstr, nil
	}
	return v.Value, nil
}

// UnmarshalJSON should only be used for testing.
// It's not a complete implementation.
func (v *Value) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("error unmarshaling empty bytes")
	}
	var val interface{}
	var err error
	switch b[0] {
	case '-':
		var ival int64
		err = json.Unmarshal(b, &ival)
		val = ival
	case '"':
		var bval []byte
		err = json.Unmarshal(b, &bval)
		val = bval
	case 'n': // null
		err = json.Unmarshal(b, &val)
	default:
		var uval uint64
		err = json.Unmarshal(b, &uval)
		val = uval
	}
	if err != nil {
		return err
	}
	*v, err = InterfaceToValue(val)
	return err
}

func encodeBytesSQL(val []byte, b BinWriter) error {
	buf := &bytes.Buffer{}
	buf.WriteByte('\'')
	for _, ch := range val {
		if encodedChar := SQLEncodeMap[ch]; encodedChar == DontEscape {
			buf.WriteByte(ch)
		} else {
			buf.WriteByte('\\')
			buf.WriteByte(encodedChar)
		}
	}
	buf.WriteByte('\'')
	_, err := b.Write(buf.Bytes())
	return err
}

func encodeBytesSQLBits(val []byte, b BinWriter) error {
	var err error
	_, err = fmt.Fprint(b, "b'")
	if err != nil {
		return err
	}
	for _, ch := range val {
		_, err = fmt.Fprintf(b, "%08b", ch)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprint(b, "'")
	return err
}

func encodeBytesASCII(val []byte, b BinWriter) {
	buf := &bytes.Buffer{}
	buf.WriteByte('\'')
	encoder := base64.NewEncoder(base64.StdEncoding, buf)
	encoder.Write(val)
	encoder.Close()
	buf.WriteByte('\'')
	b.Write(buf.Bytes())
}

// SQLEncodeMap specifies how to escape binary data with '\'.
// Complies to http://dev.mysql.com/doc/refman/5.1/en/string-syntax.html
var SQLEncodeMap [256]byte

// SQLDecodeMap is the reverse of SQLEncodeMap
var SQLDecodeMap [256]byte

var encodeRef = map[byte]byte{
	'\x00': '0',
	'\'':   '\'',
	'"':    '"',
	'\b':   'b',
	'\n':   'n',
	'\r':   'r',
	'\t':   't',
	26:     'Z', // ctl-Z
	'\\':   '\\',
}

func init() {
	for i := range SQLEncodeMap {
		SQLEncodeMap[i] = DontEscape
		SQLDecodeMap[i] = DontEscape
	}
	for i := range SQLEncodeMap {
		if to, ok := encodeRef[byte(i)]; ok {
			SQLEncodeMap[byte(i)] = to
			SQLDecodeMap[to] = byte(i)
		}
	}
}

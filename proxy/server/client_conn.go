// Copyright 2019 The Gaea Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"bytes"
	"fmt"
	"github.com/XiaoMi/Gaea/logging"

	"github.com/XiaoMi/Gaea/mysql"
)

var connLogger = logging.GetLogger("client-conn")

// ClientConn session client connection
type ClientConn struct {
	*mysql.Conn

	salt []byte

	manager *Manager

	namespace string // TODO: remove it when refactor is done
}

// NewClientConn constructor of ClientConn
func NewClientConn(c *mysql.Conn, manager *Manager) *ClientConn {
	salt, _ := mysql.RandomBuf(20)
	return &ClientConn{
		Conn:    c,
		salt:    salt,
		manager: manager,
	}
}

//https://dev.mysql.com/doc/internals/en/connection-phase-packets.html#packet-Protocol::HandshakeV10
func (cc *ClientConn) writeInitialHandshake() error {
	var data []byte

	//min version 10
	data = append(data, mysql.ProtocolVersion)

	//server version[00]
	data = append(data, mysql.ServerVersion...)
	data = append(data, 0x00)

	//connection id
	data = append(data, byte(cc.GetConnectionID()), byte(cc.GetConnectionID()>>8), byte(cc.GetConnectionID()>>16), byte(cc.GetConnectionID()>>24))

	//auth-plugin-data-part-1
	data = append(data, cc.salt[0:8]...)

	//filter 0x00 byte, terminating the first part of a scramble
	data = append(data, 0x00)

	//capability flag lower 2 bytes, using default capability here
	data = append(data, byte(DefaultCapability), byte(DefaultCapability>>8))

	//charset
	data = append(data, uint8(mysql.DefaultCollationID))

	//status
	data = append(data, byte(0), byte(0>>8))

	//capability flag upper 2 bytes, using default capability here
	data = append(data, byte(DefaultCapability>>16), byte(DefaultCapability>>24))

	// server supports CLIENT_PLUGIN_AUTH and CLIENT_SECURE_CONNECTION
	data = append(data, byte(8+12+1))

	//reserved 10 [00]
	data = append(data, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)

	//auth-plugin-data-part-2
	data = append(data, cc.salt[8:]...)
	// second part of the password cipher [mininum 13 bytes],
	// where len=MAX(13, length of auth-plugin-data - 8)
	// add \NUL to terminate the string
	data = append(data, 0x00)

	// auth plugin name
	data = append(data, mysql.AUTH_CACHING_SHA2_PASSWORD...)

	// EOF if MySQL version (>= 5.5.7 and < 5.5.10) or (>= 5.6.0 and < 5.6.2)
	// \NUL otherwise, so we use \NUL
	data = append(data, 0)
	return cc.WritePacket(data)
}

func (cc *ClientConn) writeInitialHandshakeV10() error {
	length :=
		1 + // protocol version
			mysql.LenNullString(mysql.ServerVersion) +
			4 + // connection ID
			8 + // first part of salt data
			1 + // filler byte
			2 + // capability flags (lower 2 bytes)
			1 + // character set
			2 + // status flag
			2 + // capability flags (upper 2 bytes)
			1 + // length of auth plugin data
			10 + // reserved (0)
			13 // auth-plugin-data
	// mysql.LenNullString(mysql.AUTH_NATIVE_PASSWORD) // auth-plugin-name

	data := cc.StartEphemeralPacket(length)
	pos := 0

	// Protocol version.
	pos = mysql.WriteByte(data, pos, mysql.ProtocolVersion)

	// Copy server version.
	// server version data with terminate character 0x00, type: string[NUL].
	pos = mysql.WriteNullString(data, pos, mysql.ServerVersion)

	// Add connectionID in.
	// connection id type: 4 bytes.
	pos = mysql.WriteUint32(data, pos, cc.GetConnectionID())

	// auth-plugin-data-part-1 type: string[8].
	pos += copy(data[pos:], cc.salt[:8])

	// One filler byte, always 0.
	pos = mysql.WriteByte(data, pos, 0)

	// Lower part of the capability flags, lower 2 bytes.
	pos = mysql.WriteUint16(data, pos, uint16(DefaultCapability))

	// Character set.
	pos = mysql.WriteByte(data, pos, byte(mysql.DefaultCollationID))

	// Status flag.
	pos = mysql.WriteUint16(data, pos, initClientConnStatus)

	// Upper part of the capability flags.
	pos = mysql.WriteUint16(data, pos, uint16(DefaultCapability>>16))

	// Length of auth plugin data.
	// Always 21 (8 + 13).
	pos = mysql.WriteByte(data, pos, 21)

	// Reserved 10 bytes: all 0
	pos = mysql.WriteZeroes(data, pos, 10)

	// Second part of auth plugin data.
	pos += copy(data[pos:], cc.salt[8:])
	data[pos] = 0
	pos++

	// Copy authPluginName. We always start with mysql_native_password.
	// pos = mysql.WriteNullString(data, pos, mysql.AUTH_NATIVE_PASSWORD)

	// Sanity check.
	if pos != len(data) {
		return fmt.Errorf("error building Handshake packet: got %v bytes expected %v", pos, len(data))
	}

	if err := cc.WriteEphemeralPacket(); err != nil {
		return err
	}

	return nil
}

func readAuthData(data []byte, pos int, capability uint32) ([]byte, int, bool) {
	// length encoded data
	var auth []byte
	var authLen int
	if capability&mysql.ClientPluginAuthLenencClientData > 0 {
		authData, newPos, isNULL, isOk := mysql.ReadLenEncStringAsBytes(data, pos)
		if !isOk {
			return nil, 0, false
		}
		if isNULL {
			// no auth length and no auth data, just \NUL, considered invalid auth data, and reject connection as MySQL does
			return nil, 0, false
		}
		auth = authData
		return auth, newPos, true
	} else if capability&mysql.ClientSecureConnection != 0 {
		//auth length and auth
		authLen = int(data[pos])
		pos++
		auth = data[pos : pos+authLen]
	} else {
		authLen = bytes.IndexByte(data[pos:], 0x00)
		auth = data[pos : pos+authLen]
		// account for last NUL
		authLen++
	}
	return auth, pos + authLen, true
}

func readPluginName(data []byte, pos int, capability uint32) (string, int) {
	if capability&mysql.ClientPluginAuth != 0 {
		buf := data[pos:]
		end := pos + bytes.IndexByte(buf, 0x00)
		str := data[pos:end]
		authPluginName := string(str)
		pos += len(authPluginName)
		return authPluginName, pos
	} else {
		// The method used is Native Authentication if both CLIENT_PROTOCOL_41 and CLIENT_SECURE_CONNECTION are set,
		// but CLIENT_PLUGIN_AUTH is not set, so we fallback to 'mysql_native_password'
		return mysql.AUTH_NATIVE_PASSWORD, pos
	}
}

func (cc *ClientConn) readHandshakeResponse() (HandshakeResponseInfo, error) {
	info := HandshakeResponseInfo{}
	info.Salt = cc.salt

	data, err := cc.ReadEphemeralPacketDirect()
	defer cc.RecycleReadPacket()
	if err != nil {
		return info, err
	}

	pos := 0

	// Client flags, 4 bytes.
	var ok bool
	var capability uint32
	capability, pos, ok = mysql.ReadUint32(data, pos)
	if !ok {
		return info, fmt.Errorf("readHandshakeResponse: can't read client flags")
	}
	if capability&mysql.ClientProtocol41 == 0 {
		return info, fmt.Errorf("readHandshakeResponse: only support protocol 4.1")
	}

	// Max packet size. Don't do anything with this now.
	_, pos, ok = mysql.ReadUint32(data, pos)
	if !ok {
		return info, fmt.Errorf("readHandshakeResponse: can't read maxPacketSize")
	}

	// Character set
	collationID, pos, ok := mysql.ReadByte(data, pos)
	if !ok {
		return info, fmt.Errorf("readHandshakeResponse: can't read characterSet")
	}
	info.CollationID = mysql.CollationID(collationID)

	// reserved 23 zero bytes, skipped
	pos += 23

	// username
	var user string
	user, pos, ok = mysql.ReadNullString(data, pos)
	if !ok {
		return info, fmt.Errorf("readHandshakeResponse: can't read username")
	}
	info.User = user
	info.ClientPluginAuth = capability&mysql.ClientPluginAuth > 0
	info.AuthResponse, pos, ok = readAuthData(data, pos, capability)

	// check if with database
	if capability&mysql.ClientConnectWithDB > 0 {
		var db string
		db, pos, ok = mysql.ReadNullString(data, pos)
		if !ok {
			return info, fmt.Errorf("readHandshakeResponse: can't read db")
		}
		info.Database = db
	}

	info.AuthPlugin, _ = readPluginName(data, pos, capability)
	return info, nil
}

func (cc *ClientConn) writeOK(status uint16) error {
	err := cc.WriteOKPacket(0, 0, status, 0)
	if err != nil {
		connLogger.Warnf("write ok packet failed, %v", err)
		return err
	}
	return nil
}

func (cc *ClientConn) writeMoreDataFlag(value byte) error {
	data := cc.StartEphemeralPacket(2)
	pos := 0
	pos = mysql.WriteByte(data, pos, mysql.MoreDataPacket)
	mysql.WriteByte(data, pos, value)
	return cc.WriteEphemeralPacket()
}

func (cc *ClientConn) WriteAuthMoreDataFastAuth() error {
	return cc.writeMoreDataFlag(mysql.CacheSha2FastAuthSucceed)
}

func (cc *ClientConn) WriteAuthMoreDataFullAuth() error {
	return cc.writeMoreDataFlag(mysql.CacheSha2FullAuthRequired)
}

func (cc *ClientConn) WriteAuthSwitchRequest(authMethod string) error {
	l := 1 + len(authMethod) + 1 + len(cc.salt) + 1
	data := cc.StartEphemeralPacket(l)
	pos := 0
	pos = mysql.WriteByte(data, pos, mysql.AuthSwitchHeader)
	pos = mysql.WriteNullString(data, pos, authMethod)
	pos = mysql.WriteBytes(data, pos, cc.salt)
	mysql.WriteByte(data, pos, 0)
	return cc.WriteEphemeralPacket()
}

func (cc *ClientConn) writeOKResult(status uint16, r *mysql.Result) error {
	if r.Resultset == nil {
		return cc.WriteOKPacket(r.AffectedRows, r.InsertID, status, 0)
	}
	return cc.writeResultset(status, r.Resultset)
}

func (cc *ClientConn) writeEOFPacket(status uint16) error {
	err := cc.WriteEOFPacket(status, 0)
	if err != nil {
		connLogger.Warnf("write eof packet failed, %v", err)
		return err
	}
	return nil
}

func (cc *ClientConn) writeErrorPacket(err error) error {
	e := cc.WriteErrorPacketFromError(err)
	if e != nil {
		connLogger.Warnf("write error packet failed, %v", err)
		return e
	}
	return nil
}

func (cc *ClientConn) writeColumnCount(count uint64) error {
	length := mysql.LenEncIntSize(count)
	data := cc.StartEphemeralPacket(length)
	cc.manager.GetStatisticManager().AddWriteFlowCount(cc.namespace, length)
	mysql.WriteLenEncInt(data, 0, count)
	return cc.WriteEphemeralPacket()
}

func (cc *ClientConn) writeRow(row []byte) error {
	length := len(row)
	data := cc.StartEphemeralPacket(length)
	pos := 0
	copy(data[pos:], row)
	cc.manager.GetStatisticManager().AddWriteFlowCount(cc.namespace, length)
	return cc.WriteEphemeralPacket()
}

// https://dev.mysql.com/doc/internals/en/com-query-response.html#packet-ProtocolText::Resultset
func (cc *ClientConn) writeResultset(status uint16, r *mysql.Resultset) error {
	var err error
	cc.StartWriterBuffering()

	// write column count
	columnCount := uint64(len(r.Fields))
	err = cc.writeColumnCount(columnCount)
	if err != nil {
		return err
	}

	// write columns
	err = cc.writeFieldList(status, r.Fields)
	if err != nil {
		return err
	}

	// write rows data
	// resultset row, NULL is sent as 0xfb, everything else is converted into a string and is sent as Protocol::LengthEncodedString
	for _, v := range r.RowDatas {
		err = cc.writeRow(v)
		if err != nil {
			return err
		}
	}

	err = cc.writeEOFPacket(status)
	if err != nil {
		return err
	}

	err = cc.Flush()
	if err != nil {
		return err
	}

	return nil
}

func (cc *ClientConn) writeFieldList(status uint16, fs []*mysql.Field) error {
	var err error
	for _, f := range fs {
		err = cc.writeColumnDefinition(f)
		if err != nil {
			return err
		}
	}

	err = cc.writeEOFPacket(status)
	return err
}

func (cc *ClientConn) writeColumnDefinition(field *mysql.Field) error {
	schemaLen := uint64(len(field.Schema))
	tableLen := uint64(len(field.Table))
	orgTableLen := uint64(len(field.OrgTable))
	nameLen := uint64(len(field.Name))
	orgNameLen := uint64(len(field.OrgName))
	length := 4 + // lenEncStringSize("def")
		mysql.LenEncIntSize(schemaLen) +
		len(field.Schema) +
		mysql.LenEncIntSize(tableLen) +
		len(field.Table) +
		mysql.LenEncIntSize(orgTableLen) +
		len(field.OrgTable) +
		mysql.LenEncIntSize(nameLen) +
		len(field.Name) +
		mysql.LenEncIntSize(orgNameLen) +
		len(field.OrgName) +
		1 + // length of fixed length fields
		2 + // character set
		4 + // column length
		1 + // type
		2 + // flags
		1 + // decimals
		2 // filler

	data := cc.StartEphemeralPacket(length)
	pos := 0
	pos = mysql.WriteLenEncString(data, pos, "def") // Always the same.

	pos = mysql.WriteLenEncInt(data, pos, schemaLen)
	copy(data[pos:], field.Schema)
	pos += len(field.Schema)

	pos = mysql.WriteLenEncInt(data, pos, tableLen)
	copy(data[pos:], field.Table)
	pos += len(field.Table)

	pos = mysql.WriteLenEncInt(data, pos, orgTableLen)
	copy(data[pos:], field.OrgTable)
	pos += len(field.OrgTable)

	pos = mysql.WriteLenEncInt(data, pos, nameLen)
	copy(data[pos:], field.Name)
	pos += len(field.Name)

	pos = mysql.WriteLenEncInt(data, pos, orgNameLen)
	copy(data[pos:], field.OrgName)
	pos += len(field.OrgName)

	pos = mysql.WriteByte(data, pos, 0x0c)
	pos = mysql.WriteUint16(data, pos, field.Charset)
	pos = mysql.WriteUint32(data, pos, field.ColumnLength)
	pos = mysql.WriteByte(data, pos, byte(field.Type))
	pos = mysql.WriteUint16(data, pos, field.Flag)
	pos = mysql.WriteByte(data, pos, byte(field.Decimal))
	pos = mysql.WriteUint16(data, pos, uint16(0x0000))

	if pos != len(data) {
		return fmt.Errorf("internal error: packing of column definition used %v bytes instead of %v", pos, len(data))
	}
	cc.manager.GetStatisticManager().AddWriteFlowCount(cc.namespace, len(data))

	return cc.WriteEphemeralPacket()
}

// writePrepareResponse write prepare response
func (cc *ClientConn) writePrepareResponse(status uint16, s *Stmt) error {
	var err error
	length := 1 + // status
		4 + // statement-id
		2 + // number of columns
		2 + // number of params
		1 + // filler
		2 // number of warnings
	data := cc.StartEphemeralPacket(length)
	pos := 0
	// status ok
	pos = mysql.WriteByte(data, pos, 0)
	// stmt id
	pos = mysql.WriteUint32(data, pos, s.id)
	// number columns
	pos = mysql.WriteUint16(data, pos, uint16(s.columnCount))
	// number params
	pos = mysql.WriteUint16(data, pos, uint16(s.paramCount))
	// filler [00]
	pos = mysql.WriteByte(data, pos, 0)
	// number of warnings
	pos = mysql.WriteUint16(data, pos, 0)
	if pos != length {
		return fmt.Errorf("internal error packet row: got %v bytes but expected %v", pos, length)
	}

	err = cc.WriteEphemeralPacket()
	if err != nil {
		return err
	}

	if s.paramCount > 0 {
		for i := 0; i < s.paramCount; i++ {
			err = cc.writeColumnDefinition(p)
			if err != nil {
				return err
			}
		}
		err = cc.writeEOFPacket(status)
		return err
	}

	if s.columnCount > 0 {
		for i := 0; i < s.columnCount; i++ {
			err = cc.writeColumnDefinition(c)
			if err != nil {
				return err
			}
		}
		err = cc.writeEOFPacket(status)
		return err
	}

	return nil
}

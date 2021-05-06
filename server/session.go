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

package server

import (
	"github.com/endink/go-sharding/database"
	"github.com/endink/go-sharding/mysql/types"
)

type Session struct {
	// in_transaction is set to true if the session is in a transaction.
	InTransaction bool
	// autocommit specifies if the session is in autocommit mode.
	// This is used only for V3.
	Autocommit bool

	// shard_sessions keep track of per-shard transaction info.
	ShardSessions []*database.DbSession

	Options *types.ExecuteOptions

	// target_string is the target expressed as a string. Valid
	// names are: keyspace:shard@target, keyspace@target or @target.
	// This is used only for V3.
	TargetString string

	// system_variables keeps track of all session variables set for this connection
	// TODO: systay should we keep this so we can apply it ordered?
	SystemVariables map[string]string

	// transaction_mode specifies the current transaction mode.
	TransactionMode TransactionMode
	// warnings contains non-fatal warnings from the previous query
	Warnings []string
	// pre_sessions contains sessions that have to be committed first.
	PreSessions []*database.DbSession
	// post_sessions contains sessions that have to be committed last.
	PostSessions []*database.DbSession
	// last_insert_id keeps track of the last seen insert_id for this session
	LastInsertId uint64
	// found_rows keeps track of how many rows the last query returned
	FoundRows uint64
	// user_defined_variables contains all the @variables defined for this session
	UserDefinedVariables map[string]*types.BindVariable
	// row_count keeps track of the last seen rows affected for this session
	RowCount int64
	// Stores savepoint and release savepoint calls inside a transaction
	// and is reset once transaction is committed or rolled back.
	SavePoints []string
	// in_reserved_conn is set to true if the session should be using reserved connections.
	InReservedConn bool
	// lock_session keep tracks of shard on which the lock query is sent.
	LockSession *database.DbSession
	// last_lock_heartbeat keep tracks of when last lock heartbeat was sent.
	LastLockHeartbeat int64
	// DDL strategy
	DDLStrategy string
	// Session UUID
	SessionUUID string
	// enable_system_settings defines if we can use reserved connections.
	EnableSystemSettings bool
}

func (s *Session) Clone() *Session {
	ops := *s.Options

	newSession := &Session{
		InTransaction:        s.InTransaction,
		Autocommit:           s.Autocommit,
		ShardSessions:        cloneDbSessions(s.ShardSessions),
		Options:              &ops,
		TargetString:         s.TargetString,
		SystemVariables:      database.CopyMap(s.SystemVariables),
		TransactionMode:      s.TransactionMode,
		Warnings:             database.CopyArray(s.Warnings),
		PreSessions:          cloneDbSessions(s.PreSessions),
		PostSessions:         cloneDbSessions(s.PostSessions),
		LastInsertId:         s.LastInsertId,
		FoundRows:            s.FoundRows,
		UserDefinedVariables: s.cloneUserDefinedVariables(),
		RowCount:             s.RowCount,
		SavePoints:           database.CopyArray(s.SavePoints),
		InReservedConn:       s.InReservedConn,
		LockSession:          cloneDbSession(s.LockSession),
		LastLockHeartbeat:    s.LastLockHeartbeat,
		DDLStrategy:          s.DDLStrategy,
		SessionUUID:          s.SessionUUID,
		EnableSystemSettings: s.EnableSystemSettings,
	}

	return newSession
}

func (s *Session) cloneUserDefinedVariables() map[string]*types.BindVariable {
	if s.UserDefinedVariables == nil {
		return nil
	}
	dest := make(map[string]*types.BindVariable, len(s.UserDefinedVariables))
	for name, variable := range s.UserDefinedVariables {
		if variable == nil {
			dest[name] = nil
		} else {
			dest[name] = variable.Clone()
		}
	}
	return dest
}

func cloneDbSession(source *database.DbSession) *database.DbSession {
	if source == nil {
		return nil
	}

	return source.Clone()
}

func cloneDbSessions(source []*database.DbSession) []*database.DbSession {
	if source == nil {
		return nil
	}

	sessions := make([]*database.DbSession, len(source))
	for i, s := range source {
		sessions[i] = s.Clone()
	}
	return sessions
}

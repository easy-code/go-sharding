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

package gen

import (
	"github.com/XiaoMi/Gaea/explain"
	"github.com/XiaoMi/Gaea/mysql/types"
)

func GenerateSql(defaultDataSource string, expl *explain.SqlExplain, bindVariables []*types.BindVariable) (*SqlGenResult, error) {
	values, vErr := expl.RestoreShardingValues(bindVariables)
	if vErr != nil {
		return nil, vErr
	}

	if len(values) == 0 { //没有存在任何分片表数据
		return &SqlGenResult{
			Usage: UsageRaw,
		}, nil
	} else {

		runtime, err := NewRuntime(defaultDataSource, expl, values, bindVariables)
		if err != nil {
			return nil, err
		}
		err = expl.SetVars(bindVariables)
		if err != nil {
			return nil, err
		}

		var r *SqlGenResult
		r, err = gen(expl, runtime)
		if err != nil {
			return nil, err
		}
		return r, nil
	}
}

func gen(sqlExplain *explain.SqlExplain, runtime *genRuntime) (*SqlGenResult, error) {
	genResult := &SqlGenResult{
		Usage: UsageShard,
	}

	for {
		if runtime.Next() {
			sql, restErr := sqlExplain.RestoreSql(runtime)
			if restErr != nil {
				return nil, restErr
			}

			cmd := &ScatterCommand{
				DataSource: runtime.currentDb,
				SqlCommand: sql,
				Vars:       runtime.GetCurrentBindVariables(),
			}

			genResult.Commands = append(genResult.Commands, cmd)
		} else {
			//其他数据库循环重复生成目前没有意义，留作将来扩展
			break
		}
	}
	return genResult, nil
}

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

package rewriting

import (
	"fmt"
	"github.com/XiaoMi/Gaea/explain"
	"github.com/pingcap/errors"
	"github.com/pingcap/parser/ast"
)

var _ explain.Rewriter = &Engine{}

type Engine struct {
	context explain.Context
}

func NewRewritingEngine(context explain.Context) *Engine {
	return &Engine{
		context: context,
	}
}

func (s *Engine) RewriteTable(table *ast.TableSource, explainContext explain.Context) (explain.RewriteNodeResult, error) {
	tableName, isTableName := table.Source.(*ast.TableName)
	if !isTableName {
		return nil, fmt.Errorf("table source is not type of TableName, type: %T", table.Source)
	}
	sd, ok, fe := FindShardingTable(tableName, explainContext)
	if fe != nil {
		return nil, fe
	}
	if ok {
		if writer, err := NewTableNameWriter(tableName, explainContext); err == nil {
			return ResultFromNode(writer, sd), nil
		} else {
			return nil, err
		}
	}
	return NoneRewriteNodeResult, nil
}

func (s *Engine) RewriteField(columnName *ast.ColumnNameExpr, explainContext explain.Context) (explain.RewriteExprResult, error) {
	sd, ok, err := FindShardingTableByColumn(columnName, explainContext, false)
	if err != nil {
		return nil, err
	}
	if ok {
		if writer, e := NewColumnNameWriter(columnName, explainContext); e == nil {
			return ResultFromExprNode(writer, sd), nil
		} else {
			return nil, e
		}
	}
	return NoneRewriteExprNodeResult, nil
}

func (s *Engine) RewriteColumn(columnName *ast.ColumnNameExpr, explainContext explain.Context) (explain.RewriteExprResult, error) {
	sd, ok, err := FindShardingTableByColumn(columnName, explainContext, true)
	if err != nil {
		return nil, err
	}
	if ok {
		if writer, e := NewColumnNameWriter(columnName, explainContext); e == nil {
			return ResultFromExprNode(writer, sd), nil
		} else {
			return nil, e
		}
	}
	return NoneRewriteExprNodeResult, nil
}

func (s *Engine) RewritePatterIn(patternIn *ast.PatternInExpr, explainContext explain.Context) (explain.RewriteExprResult, error) {
	columnNameExpr, ok := patternIn.Expr.(*ast.ColumnNameExpr)
	if !ok {
		return nil, errors.New("pattern in statement required ColumnNameExpr")
	}
	sd, ok, err := FindShardingTableByColumn(columnNameExpr, explainContext, true)
	if err != nil {
		return nil, err
	}
	if ok {
		if writer, e := NewPatternInWriter(patternIn, sd, explainContext); e == nil {
			return ResultFromExprNode(writer, sd), nil
		} else {
			return nil, e
		}
	}
	return NoneRewriteExprNodeResult, nil
}

func (s *Engine) RewriteBetween(between *ast.BetweenExpr, explainContext explain.Context) (explain.RewriteExprResult, error) {
	columnNameExpr, ok := between.Expr.(*ast.ColumnNameExpr)
	if !ok {
		return nil, errors.New("between and statement required ColumnNameExpr")
	}
	sd, ok, err := FindShardingTableByColumn(columnNameExpr, explainContext, true)
	if err != nil {
		return nil, err
	}
	if ok {
		if writer, e := NewBetweenWriter(between, explainContext); e == nil {
			return ResultFromExprNode(writer, sd), nil
		} else {
			return nil, e
		}
	}
	return NoneRewriteExprNodeResult, nil
}

func (s *Engine) RewriteLimit(limit *ast.Limit, explainContext explain.Context) (explain.RewriteLimitResult, error) {
	lookup := explainContext.LimitLookup()
	if lookup.HasLimit() && lookup.HasOffset() {
		writer, err := NewLimitWriter(explainContext)
		if err != nil {
			return nil, err
		}
		return ResultFromLimit(writer), nil
	}
	return NoneRewriteLimitResult, nil
}

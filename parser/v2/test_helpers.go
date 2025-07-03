package parser

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// cmpIgnoreExpressionStmt ignores the AstNode field in Expression comparisons
var cmpIgnoreExpressionStmt = cmp.Comparer(func(a, b Expression) bool {
	return a.Value == b.Value && a.Range == b.Range
})

// cmpIgnoreScopes ignores unexported scope fields in AST nodes
var cmpIgnoreScopes = cmpopts.IgnoreUnexported(
	Element{},
	IfExpression{},
	ElseIfExpression{},
	ForExpression{},
	SwitchExpression{},
	CaseExpression{},
	HTMLTemplate{},
	TemplateFile{},
)
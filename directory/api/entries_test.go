package api

import (
	"strings"
	"testing"
)

func TestBuildEntrySQL(t *testing.T) {
	// Test with no WHERE clause
	sql := buildEntrySQL("")
	if !strings.Contains(sql, "SELECT") {
		t.Error("SQL should contain SELECT")
	}
	if !strings.Contains(sql, "FROM entries e") {
		t.Error("SQL should contain FROM entries e")
	}

	// Test with WHERE clause
	sql = buildEntrySQL("WHERE e.id = $1")
	if !strings.Contains(sql, "WHERE e.id = $1") {
		t.Error("SQL should contain WHERE clause")
	}

	// Test with ORDER BY clause
	sql = buildEntrySQL("ORDER BY e.name")
	if !strings.Contains(sql, "ORDER BY e.name") {
		t.Error("SQL should contain ORDER BY clause")
	}
}

func TestHandleEntryCQL(t *testing.T) {
	// Test simple name search
	res, err := handleEntryCQL("name=foo", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}
	args := res.GetQueryArguments()
	if len(args) != 1 {
		t.Errorf("Expected 1 argument, got %d", len(args))
	}

	// Test description search
	res, err = handleEntryCQL("description=library", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}

	// Test wildcard search
	res, err = handleEntryCQL("name=*foo*", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}
	if !strings.Contains(res.GetWhereClause(), "LIKE") {
		t.Error("Expected LIKE operator for wildcard search")
	}

	// Test combined query
	res, err = handleEntryCQL("name=foo AND description=bar", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}
	args = res.GetQueryArguments()
	if len(args) != 2 {
		t.Errorf("Expected 2 arguments, got %d", len(args))
	}

	// Test invalid CQL
	_, err = handleEntryCQL("invalid cql query (((", 0)
	if err == nil {
		t.Error("Expected error for invalid CQL, got nil")
	}
}

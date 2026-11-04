package sqlite

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestConnectionLimits(t *testing.T) {
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	defer c.Close()
	other, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for _, tc := range []struct {
		category LimitCategory
		limit    int
		query    string
	}{
		{LimitLength, 1 << 20, "SELECT zeroblob(1048577)"},
		{LimitSQLLength, 4096, strings.Repeat(" ", 4096) + "SELECT 1"},
		{LimitExprDepth, 8, "SELECT " + strings.Repeat("1+(", 32) + "1" + strings.Repeat(")", 32)},
		{LimitVariableNumber, 3, "SELECT ?4"},
	} {
		previous, err := c.SetLimit(tc.category, tc.limit)
		if err != nil || previous <= tc.limit {
			t.Fatalf("set %d: previous=%d error=%v", tc.category, previous, err)
		}
		current, err := c.SetLimit(tc.category, -1)
		if err != nil || current != tc.limit {
			t.Fatalf("query %d: %d %v", tc.category, current, err)
		}
		if r, err := c.QueryContext(t.Context(), tc.query, nil); err == nil {
			r.Close()
			t.Fatal("limit was not enforced", tc.category)
		}
		untouched, err := other.(*Conn).SetLimit(tc.category, -1)
		if err != nil || untouched != previous {
			t.Fatalf("limit leaked to another connection: %d %v", untouched, err)
		}
		if _, err := c.SetLimit(tc.category, previous); err != nil {
			t.Fatal(err)
		}
	}
	before, err := c.SetLimit(LimitLength, -1)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.SetLimit(LimitLength, math.MinInt); err != nil || got != before {
		t.Fatal("negative limit did not query", got, err)
	}
	for _, category := range []LimitCategory{-1, math.MaxInt32} {
		if _, err := c.SetLimit(category, 1); err == nil {
			t.Fatal("invalid category accepted", category)
		}
	}
	if strconv.IntSize == 64 {
		large := int64(math.MaxInt32) + 1
		if _, err := c.SetLimit(LimitLength, int(large)); err == nil {
			t.Fatal("oversized limit truncated")
		}
	}
	if previous, err := c.SetLimit(LimitVariableNumber, math.MaxInt32); err != nil || previous < 3 {
		t.Fatal(previous, err)
	}
	if current, err := c.SetLimit(LimitVariableNumber, -1); err != nil || current < 3 || current > math.MaxInt32 {
		t.Fatal("compiled maximum not respected", current, err)
	}
	r, err := c.QueryContext(t.Context(), "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetLimit(LimitLength, 1<<20); !errors.Is(err, ErrActiveRows) {
		t.Fatal("changed limits with active rows", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetLimit(LimitLength, 1<<20); !errors.Is(err, ErrClosed) {
		t.Fatal("closed connection accepted limit", err)
	}
}

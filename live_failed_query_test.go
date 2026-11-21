package turborel

import (
	"context"
	"testing"
)

// TestLiveFailsBeforeItsFirstBatch reads a query that fails at its first
// row.
//
// Verifies: [PROTOCOL Query 'Q'] a statement that fails before its first
// batch is sent is answered by its Error alone, no Description or Rows
// before it: a query and a prepared one fail with no columns given, the
// connection kept [PROTOCOL Execute 'E'] [CLIENT Running statements].
func TestLiveFailsBeforeItsFirstBatch(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "FAILED_TEST")
	mustRun(t, c, "CREATE TABLE FAILED_TEST.T (K INTEGER)")
	mustRun(t, c, "INSERT INTO FAILED_TEST.T VALUES (0), (1)")
	mustRun(t, c, "COMMIT")

	ctx := context.Background()
	failing := "SELECT 1 / K FROM FAILED_TEST.T ORDER BY K"
	sink := &collector{}

	_, err := c.QueryInto(ctx, failing, sink)
	assertState(t, err, "22012")

	if sink.columns != nil || sink.batches != 0 {
		t.Errorf("the failing query gave columns %v and %d batches", sink.columns, sink.batches)
	}

	o, err := c.Query(ctx, failing)
	assertState(t, err, "22012")

	if o != nil {
		t.Errorf("the failing query gave %+v", o)
	}

	st, err := c.Prepare(ctx, failing)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Execute(ctx, st, nil)
	assertState(t, err, "22012")

	if c.IsLost() || len(mustRun(t, c, "SELECT K FROM FAILED_TEST.T").Rows) != 2 {
		t.Error("the connection did not go on")
	}
}

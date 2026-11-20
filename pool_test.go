package turborel

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeSource makes connections to scripted servers, each served by
// the next script.
func fakeSource(t *testing.T, capabilities []string, scripts ...func(f *fake)) PoolSourceFunc {
	t.Helper()

	next := 0

	return func(context.Context) (*Conn, error) {
		if next == len(scripts) {
			return nil, errDiskGone
		}

		next++

		return openGranted(t, capabilities, scripts[next-1]), nil //nolint:contextcheck // A scripted server's connection.
	}
}

// resetTwice serves a pooled connection reset twice, checked between and
// after, its last check failing.
func resetTwice(f *fake) {
	for _, answered := range []bool{true, false} {
		f.expect(kindReset)
		f.done(false)
		f.expect(kindQuery)
		f.done(false)
		f.expect(kindPing)

		if answered {
			f.reply(kindPong, nil)
		} else {
			f.failure(-919, "08006")
		}
	}
}

// TestPoolResetsAndChecks gives connections back reset and hands them out
// checked.
//
// Verifies: [CLIENT Pools] a connection given back is reset by the
// protocol's Reset, then the reset statements run, and handed out again
// checked by Ping; one failing its check is closed and another made; a
// borrower waits for one to come back; a wait given up fails with the
// context's error; no goroutine is started by the pool.
func TestPoolResetsAndChecks(t *testing.T) {
	t.Parallel()

	source := fakeSource(t, []string{CapabilityPing, CapabilityReset}, resetTwice, func(*fake) {})
	config := DefaultPoolConfig()
	config.Max, config.Reset = 1, []string{"SET SCHEMA 'A'"}
	pool := NewPool(source, config)

	first, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	conn := first.Conn
	first.Release()

	again, err := pool.Get(context.Background())
	if err != nil || again.Conn != conn {
		t.Fatalf("handed out %v, %v", again, err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		time.Sleep(20 * time.Millisecond)
		again.Release()
	}()

	waited, err := pool.Get(context.Background())
	<-done

	if err != nil || waited.Conn == conn {
		t.Errorf("after a failed check gave %v, %v", waited, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := pool.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("a wait given up gave %v", err)
	}
}

// resetByStatementsScript serves a statement prepared, a reset by
// statements, its Close refused, and a check by ROLLBACK, noting in sent
// the kinds the reset sent.
func resetByStatementsScript(f *fake, sent *[]byte) {
	kinds := []byte{kindQuery, kindClose, kindDeallocate, kindQuery, kindQuery, kindQuery, kindQuery}

	f.sqlOf()
	f.preparedOf(nil, nil)

	for _, kind := range kinds {
		got, _ := f.request()
		*sent = append(*sent, got)

		if kind == kindClose {
			f.refuse("24000", false, "")
		} else {
			f.done(false)
		}
	}

	f.expect(kindQuery)
	f.done(false)
}

// TestPoolResetByStatements resets a connection of a server that grants
// neither ping nor reset, and refuses one of too many statements.
//
// Verifies: [CLIENT Pools] of a server that grants neither, the check is
// ROLLBACK and the reset one pipeline: ROLLBACK, the statements prepared
// closed and destroyed, its authorization, schema and zone its login's,
// then the reset statements; a failure of the reset closes it; a
// connection of more statements left prepared than are destroyed is
// closed.
func TestPoolResetByStatements(t *testing.T) {
	t.Parallel()

	var sent []byte

	source := fakeSource(t, nil, func(f *fake) { resetByStatementsScript(f, &sent) })
	config := DefaultPoolConfig()
	config.Reset = []string{"SET SCHEMA 'A'"}
	pool := NewPool(source, config)

	p, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.Prepare(context.Background(), "VALUES 1"); err != nil {
		t.Fatal(err)
	}

	p.Release()

	checked, err := pool.Get(context.Background())
	if err != nil || string(sent) != "QCDQQQQ" {
		t.Fatalf("the reset sent %q; get gave %v", sent, err)
	}

	checked.Release()

	if made, _ := pool.Counts(); made != 0 {
		t.Errorf("a connection whose reset failed was kept: %d made", made)
	}

	many := openGranted(t, nil, func(*fake) {})
	many.nextStatement = ResetMostStatements + 1

	if err := reset(context.Background(), many, nil); err == nil {
		t.Error("a connection of too many statements was reset")
	}
}

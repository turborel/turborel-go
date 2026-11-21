package turborel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// scripted is a mechanism whose steps a test sets.
type scripted struct {
	answers   [][]byte
	challenge [][]byte
	refused   bool
	verified  error
	fail      error
}

func (*scripted) Name() string    { return "scripted" }
func (*scripted) Initial() []byte { return []byte("first") }

func (s *scripted) Answer(challenge []byte) ([]byte, error) {
	s.challenge = append(s.challenge, challenge)
	if s.fail != nil {
		return nil, s.fail
	}

	answer := s.answers[0]
	s.answers = s.answers[1:]

	return answer, nil
}

func (s *scripted) Verified() error { return s.verified }
func (s *scripted) Refused()        { s.refused = true }

// errScripted is a mechanism's own failure.
var errScripted = errors.New("scripted")

// TestLoginExchange logs in through a Challenge, statuses and Ready.
//
// Verifies: [PROTOCOL Opening a connection] Hello carries version 1, the
// user, the mechanism and its first bytes, the capabilities and the
// attributes; each Challenge is answered by an Answer until Ready, the
// Statuses before it taken as the session's settings; Ready's
// capabilities are what was granted
// [PROTOCOL Challenge 'C'] [PROTOCOL Answer 'A'] [PROTOCOL Ready 'R'].
func TestLoginExchange(t *testing.T) {
	t.Parallel()

	mechanism := &scripted{answers: [][]byte{[]byte("second")}}
	offer := DefaultOffer().WithAttribute(AttributeApplication, "test")

	c, err := openFake(t, mechanism, fakeConfig(offer), func(f *fake) {
		kind, body := f.request()

		want := encodeRequest(helloRequest{
			user: "ANN", mechanism: "scripted", initial: []byte("first"),
			capabilities: offer.Capabilities, attributes: offer.Attributes,
		})
		if kind != kindHello || string(body) != string(want[frameHeaderBytes:]) {
			t.Errorf("Hello % X", body)
		}

		f.reply(kindChallenge, func(w *writer) { w.bytes([]byte("step")) })

		if kind, body := f.request(); kind != kindAnswer || string(body[u32Bytes:]) != "second" {
			t.Errorf("Answer %c % X", kind, body)
		}

		f.reply(kindStatus, func(w *writer) { w.str(StatusSchema); w.str("ANN") })
		f.ready(CapabilityStatus, CapabilityPing)
	})
	if err != nil {
		t.Fatal(err)
	}

	s, _ := c.Setting(StatusSchema)
	if s != "ANN" || len(mechanism.challenge) != 1 || string(mechanism.challenge[0]) != "step" {
		t.Errorf("schema %q, challenges %q", s, mechanism.challenge)
	}

	if !slices.Equal(c.GrantedCapabilities(), []string{CapabilityStatus, CapabilityPing}) || c.Granted(CapabilityCopy) {
		t.Errorf("granted %q", c.GrantedCapabilities())
	}
}

// TestLoginRefused gives the server's refusal, and forgets the exchange.
//
// Verifies: [PROTOCOL Opening a connection] refused, the server sends an
// Error and closes; the login gives it, of SQLSTATE 28000, and a
// mechanism that keeps what it learnt forgets it.
func TestLoginRefused(t *testing.T) {
	t.Parallel()

	mechanism := &scripted{}

	_, err := openFake(t, mechanism, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.failure(sqlcodeConnection, sqlstateInvalidAuthorization)
	})

	assertState(t, err, sqlstateInvalidAuthorization)

	if !mechanism.refused {
		t.Error("the mechanism was not told of its refusal")
	}
}

// TestLoginBroken refuses a login the mechanism or the server breaks.
//
// Verifies: [PROTOCOL Opening a connection] a reply other than Challenge,
// Ready or Error, a mechanism with no answer, Ready from a server that
// did not prove itself to a mechanism that asks, and attributes past the
// bounds, each fail the login.
func TestLoginBroken(t *testing.T) {
	t.Parallel()

	_, err := openFake(t, &scripted{}, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.reply(kindRows, func(w *writer) { w.u32(0) })
	})
	assertBroken(t, err, "sent no Ready")

	_, err = openFake(t, &scripted{fail: errScripted}, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.reply(kindChallenge, func(w *writer) { w.bytes(nil) })
	})
	if !errors.Is(err, errScripted) {
		t.Errorf("a mechanism's failure: %v", err)
	}

	_, err = openFake(t, &scripted{verified: errScripted}, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.ready()
	})
	if !errors.Is(err, errScripted) {
		t.Errorf("a server not proved: %v", err)
	}

	many := DefaultOffer()
	for range maxAttributes + 1 {
		many = many.WithAttribute("a", "b")
	}

	_, err = openFake(t, Trust{}, fakeConfig(many), func(*fake) {})
	assertBroken(t, err, "attributes are more than")
}

// TestLoginContextDone gives up a login whose context is done.
//
// Verifies: [CLIENT Connecting] a login given up as its context is done
// fails as the driver's error, whose cause is the context's, so
// errors.Is(err, context.Canceled) holds.
func TestLoginContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := serve(t, func(f *fake) {
		f.request()
		cancel()
	})

	_, err := Open(ctx, client, "ANN", Trust{}, fakeConfig(DefaultOffer()))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it caused by the context", err)
	}

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLState != sqlstateConnectionFailure {
		t.Errorf("err = %v, want a connection failure", err)
	}
}

// TestCompressedAfterReady compresses requests, and inflates replies,
// once compression is granted.
//
// Verifies: [PROTOCOL Compressed frames] with compression granted, a
// request past the client's threshold goes compressed, and a compressed
// reply is inflated; Ready itself is read as it is, before compression
// begins [PROTOCOL Opening a connection].
func TestCompressedAfterReady(t *testing.T) {
	t.Parallel()

	c, err := openFake(t, Trust{}, fakeConfig(DefaultOffer().Compressed()), func(f *fake) {
		f.request()
		f.ready(CapabilityCompression, CapabilityPing)

		kind, _, compressed := f.requestCompressed()
		if kind != kindQuery || !compressed {
			t.Errorf("the long Query came as %c, compressed %v", kind, compressed)
		}

		f.request()

		var w writer

		w.frame(kindStatus, func() { w.str(strings.Repeat("S", 3000)); w.str("x") })
		_, _ = f.conn.Write(compressFrames(w.buf, 0))

		f.reply(kindPong, nil)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.send(queryRequest{sql: strings.Repeat("x", 4000)}); err != nil {
		t.Fatal(err)
	}

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if v, _ := c.Setting(strings.Repeat("S", 3000)); v != "x" {
		t.Error("the compressed Status was not read")
	}
}

// TestReadTimeout loses a connection whose server does not answer in
// time.
//
// Verifies: [CLIENT Timeouts] a read that waits past the read timeout
// loses the connection (SQLSTATE 08006), saying the server did not answer
// in time; the connection then takes no more.
func TestReadTimeout(t *testing.T) {
	t.Parallel()

	c, err := openFake(t, Trust{}, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.ready(CapabilityPing)
		f.request()
		time.Sleep(200 * time.Millisecond)
	})
	if err != nil {
		t.Fatal(err)
	}

	c.SetReadTimeout(20 * time.Millisecond)

	if c.ReadTimeout() != 20*time.Millisecond {
		t.Errorf("ReadTimeout() = %v", c.ReadTimeout())
	}

	assertBroken(t, c.Ping(context.Background()), "did not answer in time")

	if !c.IsLost() {
		t.Error("the connection is not lost")
	}

	assertBroken(t, c.send(pingRequest{}), "did not answer in time")
}

// TestMaxBody loses a connection whose server sends a reply past the
// longest taken.
//
// Verifies: [CLIENT Timeouts] replies are taken up to 16 MiB, or as many
// bytes as the program says; a longer one loses the connection.
func TestMaxBody(t *testing.T) {
	t.Parallel()

	c, err := openFake(t, Trust{}, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.ready(CapabilityPing)
		f.request()
		f.reply(kindStatus, func(w *writer) { w.str("LONG"); w.str("value") })
	})
	if err != nil {
		t.Fatal(err)
	}

	c.SetMaxBody(4)
	assertBroken(t, c.Ping(context.Background()), "longer than the 4 taken")
}

// TestTerminateEnds reads what the server sends after Terminate.
//
// Verifies: [CLIENT Connecting] Terminate waits for the server to close:
// nothing, or a notice, ends well; an Error, as a commit that failed, is
// given; any other reply fails.
func TestTerminateEnds(t *testing.T) {
	t.Parallel()

	ends := []struct {
		reply func(f *fake)
		check func(err error)
	}{
		{func(*fake) {}, func(err error) {
			if err != nil {
				t.Errorf("a clean close: %v", err)
			}
		}},
		{func(f *fake) { f.failure(-913, "40002") }, func(err error) { assertState(t, err, "40002") }},
		{func(f *fake) { f.reply(kindNotice, func(w *writer) { w.str("01000"); w.str("n") }) }, func(err error) {
			if err != nil {
				t.Errorf("a notice: %v", err)
			}
		}},
		{func(f *fake) { f.reply(kindRows, func(w *writer) { w.u32(0) }) }, func(err error) {
			assertBroken(t, err, "answered Terminate")
		}},
	}
	for _, end := range ends {
		c, err := openFake(t, Trust{}, fakeConfig(DefaultOffer()), func(f *fake) {
			f.request()
			f.ready()

			if kind, _ := f.request(); kind != kindTerminate {
				t.Errorf("Terminate came as %c", kind)
			}

			end.reply(f)
		})
		if err != nil {
			t.Fatal(err)
		}

		end.check(c.Terminate())
	}
}

// TestNoticesKept keeps the last 64 notices, or hands each to a handler.
//
// Verifies: [CLIENT What the server tells] notices are kept, the last 64,
// with no handler set, and taken; a handler set is given each as it
// comes.
func TestNoticesKept(t *testing.T) {
	t.Parallel()

	c, err := openFake(t, Trust{}, fakeConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.ready(CapabilityPing)
		f.request()

		for range keptNotices + 1 {
			f.reply(kindNotice, func(w *writer) { w.str("01000"); w.str("n") })
		}

		f.reply(kindPong, nil)
		f.request()
		f.reply(kindNotice, func(w *writer) { w.str("01000"); w.str("handed") })
		f.reply(kindPong, nil)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	if kept := c.TakeNotices(); len(kept) != keptNotices || len(c.TakeNotices()) != 0 {
		t.Errorf("kept %d", len(kept))
	}

	var handed []Notice

	c.SetNoticeHandler(func(n Notice) { handed = append(handed, n) })

	if err := c.Ping(context.Background()); err != nil || len(handed) != 1 || handed[0].Message != "handed" {
		t.Errorf("handed %v, %v", handed, err)
	}
}

// TestEndsConnection tells the failures that end their connection.
//
// Verifies: [PROTOCOL Error 'E'] an Error of class 08 but 08003, or of
// class 28 as the connection is made, ends the connection; others do not.
func TestEndsConnection(t *testing.T) {
	t.Parallel()

	cases := map[*Error]bool{
		{SQLCode: -919, SQLState: "08006"}: true,
		{SQLCode: -919, SQLState: "08004"}: true,
		{SQLCode: -919, SQLState: "08003"}: false,
		{SQLCode: -919, SQLState: "28000"}: true,
		{SQLCode: -104, SQLState: "28000"}: false,
		{SQLCode: -103, SQLState: "2A000"}: false,
	}
	for failure, want := range cases {
		if endsConnection(failure) != want {
			t.Errorf("%v ends the connection: %v", failure, !want)
		}
	}
}

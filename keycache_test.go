package turborel

import (
	"bytes"
	"strconv"
	"testing"
)

// TestKeyCacheEntries keeps, finds, bounds and forgets entries.
//
// Verifies: [CLIENT The SCRAM key cache] an entry is found by the salt,
// the iterations and a tag of the password under the cache's own secret,
// so another password, salt or count finds nothing and the cache holds no
// password; it holds at most 64 entries, the oldest dropped first; an
// entry is dropped when a login that used it fails; hits and misses are
// counted.
func TestKeyCacheEntries(t *testing.T) {
	t.Parallel()

	c := NewKeyCache()
	salt := []byte("salt")

	first := c.derived("pw", salt, 1)
	if again := c.derived("pw", salt, 1); !bytes.Equal(first.client, again.client) {
		t.Error("the keys found differ from those derived")
	}

	c.derived("other", salt, 1)
	c.derived("pw", []byte("pepper"), 1)
	c.derived("pw", salt, 2)

	if hits, misses := c.Lookups(); hits != 1 || misses != 4 || c.Len() != 4 {
		t.Errorf("hits %d, misses %d, held %d", hits, misses, c.Len())
	}

	for _, e := range c.held {
		if bytes.Contains(e.tag, []byte("pw")) {
			t.Error("an entry holds the password")
		}
	}

	c.forget("pw", salt, 1)

	if c.Len() != 3 || c.String() != "KeyCache(..)" {
		t.Errorf("held %d after forgetting", c.Len())
	}

	c.Clear()

	for i := range keyCacheEntries + 1 {
		c.derived("pw", []byte(strconv.Itoa(i)), 1)
	}

	if c.Len() != keyCacheEntries || c.find(scramHMAC(c.secret, []byte("pw")), []byte("0"), 1) >= 0 {
		t.Errorf("held %d, the oldest kept", c.Len())
	}
}

// TestKeyCacheForgetsRefused drops the entry of a login the server
// refused.
//
// Verifies: [CLIENT The SCRAM key cache] a mechanism given a cache drops
// its entry when the server refuses the login that used it, and when the
// server's signature fails.
func TestKeyCacheForgetsRefused(t *testing.T) {
	t.Parallel()

	cache := NewKeyCache()

	s := vectorScram().WithCache(cache)
	s.Initial()

	if _, err := s.Answer([]byte(vectorServerFirst)); err != nil {
		t.Fatal(err)
	}

	if cache.Len() != 1 {
		t.Fatalf("held %d", cache.Len())
	}

	s.Refused()

	if cache.Len() != 0 {
		t.Errorf("a refused login's entry kept")
	}

	s.Initial()

	if _, err := s.Answer([]byte(vectorServerFirst)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Answer([]byte("v=AAAA")); err == nil || cache.Len() != 0 {
		t.Errorf("a false signature's entry kept: %v, held %d", err, cache.Len())
	}
}

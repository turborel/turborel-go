package turborel

// The SCRAM key cache [CLIENT The SCRAM key cache]: the keys a password
// derived, kept after a login the server proved, so a later login that
// meets the same salt and iterations skips the slow derivation. An entry
// is found by the salt, the iterations and a tag of the password made
// under a random secret of the cache's own, so the cache holds neither
// the password nor anything it could be guessed from offline. It holds
// 64 entries at most, the oldest dropped first, and lives in the
// process's memory alone.

import (
	"bytes"
	"crypto/rand"
	"sync"
)

// keyCacheEntries is the most entries a cache holds, KEY_CACHE_ENTRIES.
const keyCacheEntries = 64

// keyCacheSecretBytes is the random bytes of a cache's secret, a SCRAM
// nonce's.
const keyCacheSecretBytes = scramNonceBytes

// KeyCache keeps the keys passwords derived, for every Scram given it.
// It is safe for use by many goroutines at once.
type KeyCache struct {
	secret []byte
	mu     sync.Mutex
	held   []keyEntry
	hits   uint64
	misses uint64
}

// keyEntry is an entry of the cache: what finds it, and the keys.
type keyEntry struct {
	tag        []byte
	salt       []byte
	iterations uint32
	keys       derivedKeys
}

// NewKeyCache is an empty cache, with a secret of its own.
func NewKeyCache() *KeyCache {
	secret := make([]byte, keyCacheSecretBytes)
	_, _ = rand.Read(secret) // crypto/rand's Read never fails.

	return &KeyCache{secret: secret}
}

// Len gives the entries held.
func (c *KeyCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.held)
}

// Lookups gives the lookups that found an entry, and those that did not.
func (c *KeyCache) Lookups() (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.hits, c.misses
}

// Clear drops every entry.
func (c *KeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.held = nil
}

// String gives the entries held, never what they hold.
func (c *KeyCache) String() string { return "KeyCache(..)" }

// find gives the index of the entry of tag, salt and iterations, or -1.
func (c *KeyCache) find(tag, salt []byte, iterations uint32) int {
	for i, e := range c.held {
		if e.iterations == iterations && bytes.Equal(e.tag, tag) && bytes.Equal(e.salt, salt) {
			return i
		}
	}

	return -1
}

// derived gives the keys of password, salt and iterations: kept, or
// derived and kept.
func (c *KeyCache) derived(password string, salt []byte, iterations uint32) derivedKeys {
	tag := scramHMAC(c.secret, []byte(password))

	c.mu.Lock()
	if at := c.find(tag, salt, iterations); at >= 0 {
		c.hits++
		keys := c.held[at].keys
		c.mu.Unlock()

		return keys
	}
	c.misses++
	c.mu.Unlock()

	// Derived outside the lock, so other logins are not held up.
	keys := derive(password, salt, iterations)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.find(tag, salt, iterations) < 0 {
		if len(c.held) >= keyCacheEntries {
			c.held = c.held[1:]
		}

		c.held = append(c.held, keyEntry{tag: tag, salt: salt, iterations: iterations, keys: keys})
	}

	return keys
}

// forget drops the entry of password, salt and iterations, as a login
// that used it failed.
func (c *KeyCache) forget(password string, salt []byte, iterations uint32) {
	tag := scramHMAC(c.secret, []byte(password))

	c.mu.Lock()
	defer c.mu.Unlock()

	if at := c.find(tag, salt, iterations); at >= 0 {
		c.held = append(c.held[:at], c.held[at+1:]...)
	}
}

package sqlite

import (
	"container/list"
	"strings"
)

type statementCache struct {
	entries map[string]*list.Element
	lru     list.List
}

func cacheableSQL(query string) bool {
	if len(query) > 4096 {
		return false
	}
	word := strings.TrimSpace(query)
	for i, ch := range word {
		if ch < 'A' || ch > 'Z' && ch < 'a' || ch > 'z' {
			word = word[:i]
			break
		}
	}
	// Conservatively skip non-DML commands and leading comments. A miss only
	// costs a prepare; it must not affect execution.
	return strings.EqualFold(word, "SELECT") || strings.EqualFold(word, "WITH") || hasDMLPrefix(query)
}

func (c *Conn) prepareOneShotLocked(query string) (*stmt, string, error) {
	eligible := c.cfg.statementCache > 0 && cacheableSQL(query)
	if eligible && c.cache != nil {
		if entry := c.cache.entries[query]; entry != nil {
			s := entry.Value.(*stmt)
			delete(c.cache.entries, query)
			c.cache.lru.Remove(entry)
			return s, "", nil
		}
	}
	s, tail, err := c.prepareNextLocked(query)
	if err == nil && eligible && emptyTail(tail) {
		s.cacheKey = strings.Clone(query)
	}
	return s, tail, err
}

// releaseLocked transfers an idle native handle to a fresh internal wrapper.
// A caller retaining closed Rows must never observe that handle reused.
func (s *stmt) releaseLocked(reuse bool) error {
	if s.ptr == 0 {
		return nil
	}
	c := s.c
	if !reuse || s.cacheKey == "" || c.cfg.statementCache == 0 {
		return s.closeLocked()
	}
	if rc := c.api.ResetClear(s.ptr); rc != 0 {
		err := c.sqliteError(rc)
		_ = s.closeLocked()
		return err
	}
	if c.cache == nil {
		c.cache = &statementCache{entries: make(map[string]*list.Element)}
	}
	if entry := c.cache.entries[s.cacheKey]; entry != nil {
		c.cache.lru.Remove(entry)
		delete(c.cache.entries, s.cacheKey)
		_ = entry.Value.(*stmt).closeLocked()
	}
	if c.cache.lru.Len() >= c.cfg.statementCache {
		entry := c.cache.lru.Back()
		c.cache.lru.Remove(entry)
		idle := entry.Value.(*stmt)
		delete(c.cache.entries, idle.cacheKey)
		_ = idle.closeLocked()
	}
	idle := &stmt{c: c, ptr: s.ptr, paramCount: s.paramCount, knownWrite: s.knownWrite, cacheKey: s.cacheKey, names: s.names}
	c.stmts[idle] = struct{}{}
	delete(c.stmts, s)
	s.ptr = 0
	c.cache.entries[idle.cacheKey] = c.cache.lru.PushFront(idle)
	return nil
}

func (c *Conn) clearStatementCacheLocked() {
	if c.cache == nil {
		return
	}
	for _, entry := range c.cache.entries {
		_ = entry.Value.(*stmt).closeLocked()
	}
	clear(c.cache.entries)
	c.cache.lru.Init()
}

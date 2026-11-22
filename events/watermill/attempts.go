package dogerwatermill

// attemptTracker remembers, for at most limit messages, how many times the
// handler failed on each and why the transport gave up on it, if a give-up
// is pending. Remembering a new message beyond limit forgets the one that
// failed least recently: a message that failed here and was then handled
// by another member of the group never returns. It is owned by one
// consuming loop and not safe for concurrent use.
type attemptTracker struct {
	limit   int
	entries map[string]*attemptEntry
	oldest  *attemptEntry // the least recently failed; its prev is nil
	newest  *attemptEntry // the most recently failed; its next is nil
}

// attemptEntry is what the tracker remembers of one message, linked in
// the order the messages last failed.
type attemptEntry struct {
	uuid       string
	attempts   int   // failed handler runs
	giveUp     error // the reason of a give-up whose poison publish failed
	prev, next *attemptEntry
}

func newAttemptTracker(limit int) *attemptTracker {
	return &attemptTracker{limit: limit, entries: map[string]*attemptEntry{}}
}

// lookup returns the entry of uuid, or false.
func (t *attemptTracker) lookup(uuid string) (*attemptEntry, bool) {
	entry, ok := t.entries[uuid]
	return entry, ok
}

// touch returns the entry of uuid, created if needed, as the most recently
// failed one, forgetting the least recently failed beyond the limit.
func (t *attemptTracker) touch(uuid string) *attemptEntry {
	entry, ok := t.entries[uuid]
	if ok {
		t.unlink(entry)
	} else {
		entry = &attemptEntry{uuid: uuid}
		t.entries[uuid] = entry
	}
	t.append(entry)
	for len(t.entries) > t.limit {
		t.forget(t.oldest.uuid)
	}

	return entry
}

// forget drops the entry of uuid.
func (t *attemptTracker) forget(uuid string) {
	if entry, ok := t.entries[uuid]; ok {
		t.unlink(entry)
		delete(t.entries, uuid)
	}
}

// len returns how many messages the tracker remembers.
func (t *attemptTracker) len() int { return len(t.entries) }

// append links entry as the newest.
func (t *attemptTracker) append(entry *attemptEntry) {
	entry.prev, entry.next = t.newest, nil
	if t.newest != nil {
		t.newest.next = entry
	} else {
		t.oldest = entry
	}
	t.newest = entry
}

// unlink removes entry from the order.
func (t *attemptTracker) unlink(entry *attemptEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		t.oldest = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		t.newest = entry.prev
	}
	entry.prev, entry.next = nil, nil
}

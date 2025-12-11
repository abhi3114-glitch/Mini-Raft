package raft

import "sync"

// Log manages the Raft log entries
type Log struct {
	mu      sync.RWMutex
	entries []LogEntry
}

// NewLog creates a new empty log
func NewLog() *Log {
	return &Log{
		entries: make([]LogEntry, 0),
	}
}

// Append adds entries to the log
func (l *Log) Append(entries ...LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entries...)
}

// Get returns the entry at the given index (1-based)
// Returns nil if index is out of range
func (l *Log) Get(index uint64) *LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if index == 0 || index > uint64(len(l.entries)) {
		return nil
	}
	entry := l.entries[index-1]
	return &entry
}

// LastIndex returns the index of the last entry (0 if empty)
func (l *Log) LastIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return uint64(len(l.entries))
}

// LastTerm returns the term of the last entry (0 if empty)
func (l *Log) LastTerm() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.entries) == 0 {
		return 0
	}
	return l.entries[len(l.entries)-1].Term
}

// GetFrom returns all entries starting from the given index (1-based)
func (l *Log) GetFrom(startIndex uint64) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if startIndex == 0 || startIndex > uint64(len(l.entries)) {
		return nil
	}

	// Return a copy to avoid race conditions
	result := make([]LogEntry, len(l.entries)-int(startIndex)+1)
	copy(result, l.entries[startIndex-1:])
	return result
}

// GetRange returns entries in the range [start, end] (1-based, inclusive)
func (l *Log) GetRange(start, end uint64) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if start == 0 || start > uint64(len(l.entries)) {
		return nil
	}
	if end > uint64(len(l.entries)) {
		end = uint64(len(l.entries))
	}
	if start > end {
		return nil
	}

	result := make([]LogEntry, end-start+1)
	copy(result, l.entries[start-1:end])
	return result
}

// TruncateFrom removes all entries from the given index onwards (1-based)
func (l *Log) TruncateFrom(index uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index == 0 || index > uint64(len(l.entries)) {
		return
	}
	l.entries = l.entries[:index-1]
}

// TermAt returns the term of the entry at the given index (0 if not found)
func (l *Log) TermAt(index uint64) uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if index == 0 || index > uint64(len(l.entries)) {
		return 0
	}
	return l.entries[index-1].Term
}

// Length returns the number of entries in the log
func (l *Log) Length() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return uint64(len(l.entries))
}

// FindConflict finds the first index where the log diverges from the given entries
// Returns 0 if no conflict found
func (l *Log) FindConflict(prevLogIndex uint64, entries []LogEntry) (conflictIndex uint64, conflictTerm uint64) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for i, entry := range entries {
		index := prevLogIndex + uint64(i) + 1
		if index > uint64(len(l.entries)) {
			return index, 0
		}
		if l.entries[index-1].Term != entry.Term {
			return index, l.entries[index-1].Term
		}
	}
	return 0, 0
}

// AppendAfter appends entries after the given index, truncating any conflicting entries
func (l *Log) AppendAfter(prevLogIndex uint64, entries []LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, entry := range entries {
		index := prevLogIndex + uint64(i) + 1

		if index <= uint64(len(l.entries)) {
			// Check for conflict
			if l.entries[index-1].Term != entry.Term {
				// Truncate from here
				l.entries = l.entries[:index-1]
				l.entries = append(l.entries, entries[i:]...)
				return
			}
			// Entry matches, continue
		} else {
			// Append remaining entries
			l.entries = append(l.entries, entries[i:]...)
			return
		}
	}
}

// MatchesPrevLog checks if the log matches at the given previous index and term
func (l *Log) MatchesPrevLog(prevLogIndex, prevLogTerm uint64) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// prevLogIndex == 0 means this is the first entry
	if prevLogIndex == 0 {
		return true
	}

	if prevLogIndex > uint64(len(l.entries)) {
		return false
	}

	return l.entries[prevLogIndex-1].Term == prevLogTerm
}

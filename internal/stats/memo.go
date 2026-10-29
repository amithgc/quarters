package stats

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// ---- results worked out from files, kept until one of the files changes ----

// The side panel asks every couple of seconds; ~/.claude.json alone can be
// hundreds of kilobytes of JSON. A few stat calls tell whether anything
// needs reading again.
var memos = struct {
	sync.Mutex
	m map[string]memoEntry
}{m: map[string]memoEntry{}}

type memoEntry struct {
	sig string
	val any
}

// filesSig changes when any of files is created, removed or written.
func filesSig(files []string) string {
	var sb strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&sb, "%d.%d;", st.Size(), st.ModTime().UnixNano())
		} else {
			sb.WriteString("-;")
		}
	}
	return sb.String()
}

// memo returns what work returned for key while files are unchanged.
func memo[T any](key string, files []string, work func() T) T {
	sig := filesSig(files)
	memos.Lock()
	e, ok := memos.m[key]
	memos.Unlock()
	if ok && e.sig == sig {
		return e.val.(T)
	}
	v := work()
	memos.Lock()
	memos.m[key] = memoEntry{sig, v}
	memos.Unlock()
	return v
}

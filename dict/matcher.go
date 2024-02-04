package dict

import (
	"context"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

type MatchResult struct {
	Entry  *Entry
	result algo.Result
}

func (m *MatchResult) String() string {
	return m.Entry.String()
}

func (m *MatchResult) Order() int {
	score := m.result.Score
	score = score * m.Entry.text.Length()
	return score
}

type Matcher interface {
	Search(key []rune, list []*Entry, resultChan chan<- []*MatchResult, ctx context.Context)
	Reset()
}

type CacheMatcher struct {
	cache map[string][]*MatchResult
}

func (m *CacheMatcher) Reset() {
	m.cache = nil
}

var slab = util.MakeSlab(100*1024, 2048)

func (m *CacheMatcher) Search(key []rune, list []*Entry, resultChan chan<- []*MatchResult, ctx context.Context) {
	var cache []*MatchResult
	if m.cache != nil {
		cachedKey := ""
		for i := len(key); i > 0; i-- {
			cachedKey = string(key[:i])
			if cache = m.cache[cachedKey]; cache != nil {
				break
			}
		}
		if cache != nil && cachedKey == string(key) {
			resultChan <- cache
			return
		}
	}

	if cache != nil {
		list = make([]*Entry, len(cache))
		for i, m := range cache {
			list[i] = m.Entry
		}
	}

	var done bool
	go func() {
		<-ctx.Done()
		done = true
	}()

	const CHUNK_SIZE = 500
	matched := make([]MatchResult, 0)
	lastIdx := 0
	listLen := len(list)
	for idx, entry := range list {
		if entry.modType == DELETE {
			continue
		}
		result, _ := algo.FuzzyMatchV2(false, true, true, &entry.text, key, false, slab)
		if result.Score > 0 {
			matched = append(matched, MatchResult{entry, result})
		}
		if idx%CHUNK_SIZE == 0 || done || idx == listLen-1 {
			m2 := matched[lastIdx:]
			if len(m2) > 0 {
				ret := make([]*MatchResult, len(m2))
				for i, m := range m2 {
					ret[i] = &m
				}
				resultChan <- ret
				lastIdx = len(matched)
			}
		}
		if done {
			return
		}
	}

	cache = make([]*MatchResult, len(matched))
	for i, m := range matched {
		cache[i] = &m
	}
	if m.cache == nil {
		m.cache = make(map[string][]*MatchResult)
	}
	m.cache[string(key)] = cache
}

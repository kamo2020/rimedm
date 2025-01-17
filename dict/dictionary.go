package dict

import (
	"bytes"
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/junegunn/fzf/src/util"
)

type Dictionary struct {
	matcher     Matcher
	entries     []*Entry
	fileEntries []*FileEntries
}

func NewDictionary(fes []*FileEntries, matcher Matcher) *Dictionary {
	if matcher == nil {
		matcher = &CacheMatcher{}
	}
	entries := make([]*Entry, 0)
	for _, fe := range fes {
		entries = append(entries, fe.Entries...)
	}
	return &Dictionary{
		matcher:     matcher,
		entries:     entries,
		fileEntries: fes,
	}
}

func (d *Dictionary) Entries() []*Entry {
	return d.entries
}

func (d *Dictionary) Search(key []rune, resultChan chan<- []*MatchResult, ctx context.Context) {
	log.Println("search key: ", string(key))
	if len(key) == 0 {
		done := false
		go func() {
			<-ctx.Done()
			done = true
		}()
		list := d.Entries()
		deleteCount := 0
		ret := make([]*MatchResult, len(list))
		for i, entry := range list {
			if done {
				return
			}
			if entry.IsDelete() {
				deleteCount += 1
				continue
			}
			ret[i-deleteCount] = &MatchResult{Entry: entry}
		}
		resultChan <- ret[0 : len(ret)-deleteCount]
	} else {
		d.matcher.Search(key, d.Entries(), resultChan, ctx)
	}
}

func (d *Dictionary) Add(entry *Entry) {
	for _, fe := range d.fileEntries {
		if fe.FilePath == entry.refFile {
			fe.Entries = append(fe.Entries, entry)
		}
	}
	d.entries = append(d.entries, entry)
}

func (d *Dictionary) Delete(entry *Entry) {
	entry.Delete()
}

func (d *Dictionary) ResetMatcher() {
	d.matcher.Reset()
}

func (d *Dictionary) Len() int {
	return len(d.entries)
}

func (d *Dictionary) Flush() (changed bool) {
	start := time.Now()
	changed = output(d.fileEntries)
	since := time.Since(start)
	if changed {
		log.Printf("flush dictionary: %v\n", since)
	}
	return changed
}

func (d *Dictionary) ExportDict(path string) {
	exportDict(path, d.fileEntries)
}

func (d *Dictionary) Files() []*FileEntries {
	return d.fileEntries
}

type ModifyType int

const (
	NC ModifyType = iota // default no change
	DELETE
	MODIFY // by ReRaw
	ADD    // by NewEntryAdd
)

type Entry struct {
	refFile string
	Pair    [][]byte
	text    util.Chars
	seek    int64
	rawSize int64
	modType ModifyType
	saved   bool
	Weight  int
}

func (e *Entry) ReRaw(raw []byte) {
	e.text = util.ToChars(raw)
	e.Pair = ParsePair(raw)
	if e.modType != ADD {
		e.modType = MODIFY
	}
	e.saved = false
	e.Weight = 1
	if len(e.Pair) >= 3 {
		e.Weight, _ = strconv.Atoi(string(e.Pair[2]))
	}
	// don't change rawSize
}

func (e *Entry) Delete() {
	e.modType = DELETE
	e.saved = false
}

func (e *Entry) IsDelete() bool {
	return e.modType == DELETE
}

func (e *Entry) String() string {
	// return e.text.ToString() + "\t" + e.refFile
	return e.text.ToString()
}

func (e *Entry) Saved() {
	e.saved = true
	if e.modType == MODIFY {
		e.rawSize = int64(len(e.WriteLine())) + 1 // + 1 for '\n'
	}
}

func (e *Entry) WriteLine() []byte {
	bs := make([]byte, 0)
	for i := 0; i < len(e.Pair); i++ {
		if len(bytes.TrimSpace(e.Pair[i])) == 0 {
			continue
		}
		bs = append(bs, e.Pair[i]...)
		if i < len(e.Pair)-1 {
			bs = append(bs, '\t')
		}
	}
	return bs
}

// Parse input string to a pair of strings
// 0: 表(汉字) 1: 码(字母) 2: 权重
// 支持乱序输入，如 "你好 nau 1" 或 "nau 1 你好"
func ParseInput(raw string) (pair [3]string) {
	pair = [3]string{}
	// split by '\t' or ' '
	splits := strings.Fields(raw)
	lastType := 1 // 1: number 2:ascii 3:汉字
	for i := 0; i < len(splits); i++ {
		item := strings.TrimSpace(splits[i])
		if len(item) == 0 {
			continue
		}
		if isNumber(item) {
			if lastType == 1 {
				pair[2] = pair[2] + " " + item
			} else {
				pair[2] = item
			}
			lastType = 1
			continue
		}
		if isAscii(item) {
			if lastType == 2 {
				pair[1] = pair[1] + " " + item
			} else {
				pair[1] = item
			}
			lastType = 2
			continue
		}
		// 表(汉字)的输入可能包含空格，类似 "富强 强国"，因此在splited后重新拼接起来。
		pair[0] = pair[0] + " " + item
		lastType = 3
	}
	for i := 0; i < len(pair); i++ {
		pair[i] = strings.TrimSpace(pair[i])
	}
	return
}

func isNumber(str string) bool {
	for _, r := range str {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAscii(str string) bool {
	for _, r := range str {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

// Parse bytes as a couple of strings([]byte) separated by '\t'
// e.g. "你好	nau" > ["你好", "nau"]
// not like ParseInput, this function simply split by '\t'
func ParsePair(raw []byte) [][]byte {
	pair := make([][]byte, 0)
	for i, j := 0, 0; i < len(raw); i++ {
		if raw[i] == '\t' {
			item := bytes.TrimSpace(raw[j:i])
			if len(item) > 0 {
				pair = append(pair, item)
			}
			j = i + 1
		}
		if i == len(raw)-1 && j <= i {
			item := bytes.TrimSpace(raw[j:])
			if len(item) > 0 {
				pair = append(pair, item)
			}
		}
	}
	return pair
}

func NewEntry(raw []byte, refFile string, seek int64, size int64) *Entry {
	pair := ParsePair(raw)
	weight := 1
	if len(pair) >= 3 {
		weight, _ = strconv.Atoi(string(pair[2]))
	}
	return &Entry{
		text:    util.ToChars(raw),
		Pair:    pair,
		refFile: refFile,
		modType: NC,
		seek:    seek,
		rawSize: size,
		Weight:  weight,
		saved:   true,
	}
}

func NewEntryAdd(raw []byte, refFile string) *Entry {
	pair := ParsePair(raw)
	weight := 1
	if len(pair) >= 3 {
		weight, _ = strconv.Atoi(string(pair[2]))
	}
	return &Entry{
		text:    util.ToChars(raw),
		Pair:    pair,
		refFile: refFile,
		modType: ADD,
		saved:   false,
		Weight:  weight,
	}
}

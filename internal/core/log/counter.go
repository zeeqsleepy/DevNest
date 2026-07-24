package log

import "sort"

// maxKeys caps how many distinct values one counter tracks.
//
// This is the one place where memory could still grow with the input: a log
// with a million distinct URLs would otherwise put a million strings in a map.
// Past the cap, new keys are counted in a single bucket and the counter says
// it happened, so a truncated ranking is visible rather than quietly wrong.
// The cap is generous enough that no ordinary log reaches it.
const maxKeys = 100_000

// overflowKey is where values beyond the cardinality cap are counted.
const overflowKey = "(other)"

// counter tallies how often each value occurs.
//
// Every ranked listing in this module is one of these. Sharing it means the
// tie-breaking, the percentages, and the cardinality guard are written once
// and behave the same for methods, status codes, paths, clients, and error
// categories.
// The tallies are pointers rather than values, which is the difference
// between allocating once per distinct value and once per line. Go optimises
// a map lookup written as m[string(b)] into one that does not copy the bytes,
// but an assignment back into the map does copy them, so incrementing in place
// through a pointer is what keeps a scan over ten million lines from
// allocating ten million strings.
type counter struct {
	counts   map[string]*int
	total    int
	overflow bool
}

func newCounter() *counter {
	return &counter{counts: make(map[string]*int)}
}

// add records one occurrence. The key is a byte slice from the scanner's
// buffer; converting it to a string is what copies it, and that happens only
// the first time a value is seen.
func (c *counter) add(key []byte) {
	c.total++
	if tally, seen := c.counts[string(key)]; seen {
		*tally++
		return
	}
	c.insert(string(key))
}

// addText records one occurrence of a value that is already a string.
func (c *counter) addText(key string) {
	c.total++
	if tally, seen := c.counts[key]; seen {
		*tally++
		return
	}
	c.insert(key)
}

// insert adds a value seen for the first time, or folds it into the overflow
// bucket once the cardinality cap is reached.
func (c *counter) insert(key string) {
	if len(c.counts) >= maxKeys {
		c.overflow = true
		key = overflowKey
		if tally, seen := c.counts[key]; seen {
			*tally++
			return
		}
	}
	first := 1
	c.counts[key] = &first
}

// unique reports how many distinct values were seen.
func (c *counter) unique() int { return len(c.counts) }

// top ranks the most frequent values, highest first.
//
// Ties break on the value itself so that two runs over an unchanged file
// produce byte-identical output. A report nobody can diff is a report nobody
// trusts.
func (c *counter) top(limit int, total int) []Count {
	ranked := make([]Count, 0, len(c.counts))
	for value, tally := range c.counts {
		ranked = append(ranked, Count{Value: value, Count: *tally, Percent: percent(*tally, total)})
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].Value < ranked[j].Value
	})

	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ensure(ranked)
}

// ordered ranks by the given sequence of values rather than by frequency, and
// reports every one of them including the zeroes.
//
// Status classes want this: a summary that omits 5xx because there were none
// makes the reader wonder whether it was omitted or missed.
func (c *counter) ordered(values []string, total int) []Count {
	ranked := make([]Count, 0, len(values))
	for _, value := range values {
		count := 0
		if tally, seen := c.counts[value]; seen {
			count = *tally
		}
		ranked = append(ranked, Count{Value: value, Count: count, Percent: percent(count, total)})
	}
	return ranked
}

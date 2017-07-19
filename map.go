package poi

import (
	"sort"
	"strings"
)

type sortFunc func(asc bool) func(i, j int) bool

var sortMethod map[string]sortFunc

func init() {
	sortMethod = make(map[string]sortFunc, 13)
}

type dict struct {
	start, rownum int
	keys          []string
	m             map[string]*tableData
}

func newDict() *dict {
	d := &dict{
		rownum: 2,
		keys:   make([]string, 0),
		m:      make(map[string]*tableData),
	}
	sortMethod["count"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).count > d.get(d.keys[j]).count
			}
			return d.get(d.keys[i]).count < d.get(d.keys[j]).count
		}
	}
	sortMethod["min"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).minTime > d.get(d.keys[j]).minTime
			}
			return d.get(d.keys[i]).minTime < d.get(d.keys[j]).minTime
		}
	}
	sortMethod["max"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).maxTime > d.get(d.keys[j]).maxTime
			}
			return d.get(d.keys[i]).maxTime < d.get(d.keys[j]).maxTime
		}
	}
	sortMethod["avg"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).avgTime > d.get(d.keys[j]).avgTime
			}
			return d.get(d.keys[i]).avgTime < d.get(d.keys[j]).avgTime
		}
	}
	sortMethod["avg"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).stdev > d.get(d.keys[j]).stdev
			}
			return d.get(d.keys[i]).stdev < d.get(d.keys[j]).stdev
		}
	}
	sortMethod["p10"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).p10 > d.get(d.keys[j]).p10
			}
			return d.get(d.keys[i]).p10 < d.get(d.keys[j]).p10
		}
	}
	sortMethod["p50"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).p50 > d.get(d.keys[j]).p50
			}
			return d.get(d.keys[i]).p50 < d.get(d.keys[j]).p50
		}
	}
	sortMethod["p90"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).p90 > d.get(d.keys[j]).p90
			}
			return d.get(d.keys[i]).p90 < d.get(d.keys[j]).p90
		}
	}
	sortMethod["p95"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).p95 > d.get(d.keys[j]).p95
			}
			return d.get(d.keys[i]).p95 < d.get(d.keys[j]).p95
		}
	}
	sortMethod["p99"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).p99 > d.get(d.keys[j]).p99
			}
			return d.get(d.keys[i]).p99 < d.get(d.keys[j]).p99
		}
	}
	sortMethod["bodymin"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).minBody > d.get(d.keys[j]).minBody
			}
			return d.get(d.keys[i]).minBody < d.get(d.keys[j]).minBody
		}
	}
	sortMethod["bodymax"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).maxBody > d.get(d.keys[j]).maxBody
			}
			return d.get(d.keys[i]).maxBody < d.get(d.keys[j]).maxBody
		}
	}
	sortMethod["bodyavg"] = func(desc bool) func(i, j int) bool {
		return func(i, j int) bool {
			if desc {
				return d.get(d.keys[i]).avgBody > d.get(d.keys[j]).avgBody
			}
			return d.get(d.keys[i]).avgBody < d.get(d.keys[j]).avgBody
		}
	}
	return d
}

func (d *dict) set(key string, val *tableData) {
	if _, ok := d.m[key]; !ok {
		d.keys = append(d.keys, key)
	}
	d.m[key] = val
}

func (d *dict) get(key string) *tableData {
	if v, ok := d.m[key]; ok {
		return v
	}
	return nil
}

func (d *dict) sortedKeys(by string) []string {
	var desc bool // default is asc
	if strings.ContainsRune(by, ',') {
		sep := strings.Split(by, ",")
		sortedBy, orderBy := sep[0], sep[1]
		if orderBy == "desc" {
			desc = true
		}
		by = sortedBy
	}

	if c, ok := sortMethod[by]; ok {
		sort.Slice(d.keys, c(desc))
	} else {
		sort.Slice(d.keys, sortMethod["count"](desc))
	}

	l := len(d.keys)
	if d.start+d.rownum < l {
		return d.keys[d.start : d.start+d.rownum]
	}
	return d.keys[d.start:l]
}

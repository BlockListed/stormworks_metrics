package types

import "sort"

type Entry struct {
	K string
	V float64
}

type EntrySlice []Entry

func (a EntrySlice) Len() int           { return len(a) }
func (a EntrySlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a EntrySlice) Less(i, j int) bool { return a[i].K < a[j].K }

func SortedMap(data map[string]float64) []Entry {
	res := make([]Entry, 0, len(data))

	for k, v := range data {
		res = append(res, Entry{k, v})
	}

	sort.Sort(EntrySlice(res))

	return res
}

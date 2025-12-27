package common

import "sort"

// Prioritized represents an item with a priority for sorting
type Prioritized interface {
	GetPriority() int
}

// SortByPriority sorts a slice of Prioritized items by priority (higher priority first)
func SortByPriority[T Prioritized](items []T) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetPriority() > items[j].GetPriority()
	})
}

// SortByPriorityFunc sorts a slice using a priority extraction function
func SortByPriorityFunc[T any](items []T, getPriority func(T) int) {
	sort.Slice(items, func(i, j int) bool {
		return getPriority(items[i]) > getPriority(items[j])
	})
}

// SortByPriorityStable sorts a slice by priority using stable sort (preserves order of equal elements)
func SortByPriorityStable[T Prioritized](items []T) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].GetPriority() > items[j].GetPriority()
	})
}

// SortByPriorityFuncStable sorts a slice using a priority function with stable sort
func SortByPriorityFuncStable[T any](items []T, getPriority func(T) int) {
	sort.SliceStable(items, func(i, j int) bool {
		return getPriority(items[i]) > getPriority(items[j])
	})
}

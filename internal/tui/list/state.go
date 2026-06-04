package list

import "strings"

// State manages filter text, cursor movement, and filtered items for overlay lists.
type State[T any] struct {
	items    []T
	filtered []T
	filter   string
	cursor   int
	match    func(item T, needle string) bool
}

// NewState constructs a filterable list state with the supplied matcher.
func NewState[T any](match func(item T, needle string) bool) State[T] {
	return State[T]{match: match}
}

// SetItems replaces the backing items and resets the cursor to the first row.
func (s *State[T]) SetItems(items []T) {
	s.ResetItems(items, false)
}

// ResetItems replaces the backing items and optionally keeps the current cursor.
func (s *State[T]) ResetItems(items []T, preserveCursor bool) {
	s.items = clone(items)
	if !preserveCursor {
		s.cursor = 0
	}
	s.apply()
}

// SetFilter replaces the filter text and resets the cursor.
func (s *State[T]) SetFilter(filter string) {
	s.filter = filter
	s.cursor = 0
	s.apply()
}

// TypeFilter appends one character to the current filter.
func (s *State[T]) TypeFilter(ch string) {
	s.SetFilter(s.filter + ch)
}

// DeleteFilter removes the last character from the current filter.
func (s *State[T]) DeleteFilter() {
	if len(s.filter) == 0 {
		return
	}
	s.SetFilter(s.filter[:len(s.filter)-1])
}

// ClearFilter clears the current filter.
func (s *State[T]) ClearFilter() {
	s.SetFilter("")
}

// Refilter reapplies the current filter against the current items.
func (s *State[T]) Refilter() {
	s.apply()
}

// Up moves the cursor one row up.
func (s *State[T]) Up() {
	if s.cursor > 0 {
		s.cursor--
	}
}

// Down moves the cursor one row down.
func (s *State[T]) Down() {
	if s.cursor < len(s.filtered)-1 {
		s.cursor++
	}
}

// Selected returns the currently selected filtered item.
func (s *State[T]) Selected() (T, bool) {
	var zero T
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return zero, false
	}
	return s.filtered[s.cursor], true
}

// FilteredItems returns the filtered items in display order.
func (s *State[T]) FilteredItems() []T {
	return s.filtered
}

// Filter returns the current filter text.
func (s *State[T]) Filter() string {
	return s.filter
}

// Cursor returns the current filtered cursor position.
func (s *State[T]) Cursor() int {
	return s.cursor
}

func (s *State[T]) apply() {
	if s.filter == "" {
		s.filtered = clone(s.items)
	} else {
		needle := strings.ToLower(s.filter)
		s.filtered = s.filtered[:0]
		for _, item := range s.items {
			if s.match == nil || s.match(item, needle) {
				s.filtered = append(s.filtered, item)
			}
		}
	}
	if s.cursor >= len(s.filtered) {
		s.cursor = max(0, len(s.filtered)-1)
	}
}

func clone[T any](items []T) []T {
	out := make([]T, len(items))
	copy(out, items)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

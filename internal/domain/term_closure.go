package domain

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// MaxTermDepth is the deepest a term may be below its root: a term tree has at most four levels.
const MaxTermDepth = 3

// Errors of TermClosure. Messages name the term by ID.
var (
	ErrTermCycle         = errors.New("term is its own ancestor")
	ErrTermTooDeep       = fmt.Errorf("term is more than %d levels below its root", MaxTermDepth)
	ErrTermUnknownParent = errors.New("term's parent is not a term")
)

// TermAncestor is a row of the term_ancestors closure: AncestorID is DescendantID itself (Depth 0) or an ancestor
// Depth levels above it.
type TermAncestor struct {
	AncestorID   int64 `json:"ancestor_id"`
	DescendantID int64 `json:"descendant_id"`
	Depth        int   `json:"depth"`
}

// TermClosure returns the closure rows of the terms of parents, which maps each term's ID to its parent's ID, or
// nil for a root: every term's depth-0 row and one row per ancestor, ordered by descendant and then depth. It fails
// with ErrTermUnknownParent for a parent not in parents, ErrTermCycle for a term among its own ancestors and
// ErrTermTooDeep for one more than MaxTermDepth levels below its root, naming the lowest such term ID.
func TermClosure(parents map[int64]*int64) ([]TermAncestor, error) {
	var rows []TermAncestor
	for _, id := range slices.Sorted(maps.Keys(parents)) {
		ancestors, err := termAncestors(id, parents)
		if err != nil {
			return nil, err
		}
		if len(ancestors) > MaxTermDepth {
			return nil, fmt.Errorf("%w: term %d", ErrTermTooDeep, id)
		}
		rows = append(rows, TermAncestor{AncestorID: id, DescendantID: id})
		for i, ancestor := range ancestors {
			rows = append(rows, TermAncestor{AncestorID: ancestor, DescendantID: id, Depth: i + 1})
		}
	}

	return rows, nil
}

// termAncestors returns the ancestors of the term id, its parent first.
func termAncestors(id int64, parents map[int64]*int64) ([]int64, error) {
	var ancestors []int64
	seen := map[int64]bool{id: true}
	for parent := parents[id]; parent != nil; parent = parents[*parent] {
		if _, known := parents[*parent]; !known {
			return nil, fmt.Errorf("%w: term %d", ErrTermUnknownParent, id)
		}
		if seen[*parent] {
			return nil, fmt.Errorf("%w: term %d", ErrTermCycle, id)
		}
		seen[*parent] = true
		ancestors = append(ancestors, *parent)
	}

	return ancestors, nil
}

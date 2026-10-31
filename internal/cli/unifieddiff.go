package cli

import (
	"fmt"
	"strings"
)

const (
	// diffContext is how many unchanged lines a hunk shows on each side.
	diffContext = 3

	// diffBudget bounds the search, in diagonals entered and lines compared. Two
	// plans at the document limit, of the shortest lines it allows, are about
	// half a million lines each, and an unbounded search of them takes hours.
	// This much finds the shortest diff of plans about 2,900 changed lines apart.
	diffBudget = 1 << 22
)

// unifiedHunks is the hunks of a unified diff that turns old into updated, with
// no ---/+++ headers. It is empty where the two are the same.
//
// A reviewer asked to look again is shown this in place of the plan, so nothing
// here may drop a change. That does not rest on the search: the search only marks
// lines to try to keep, and editScript decides what is printed, comparing every
// line it leaves out. A wrong mark, or a search stopped at the budget, makes the
// diff larger. It cannot make it wrong.
func unifiedHunks(old, updated string) string {
	a, b := splitLines(old), splitLines(updated)
	inA, inB := make([]bool, len(a)), make([]bool, len(b))
	prefix, suffix := commonLines(a, b)
	for i := range prefix {
		inA[i], inB[i] = true, true
	}
	for i := 1; i <= suffix; i++ {
		inA[len(a)-i], inB[len(b)-i] = true, true
	}
	endA, endB := len(a)-suffix, len(b)-suffix
	idsA, idsB := lineIDs(a[prefix:endA], b[prefix:endB])
	markShortestEdit(idsA, idsB, inA[prefix:endA], inB[prefix:endB])
	return renderHunks(a, b, editScript(a, b, inA, inB))
}

// splitLines keeps each line's terminator, so that a line's ending is part of
// the line: one\r\n and one\n differ, and a last line with no newline is not the
// same line with one.
func splitLines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// commonLines is how many lines the two share at the start, and then at the end
// of what is left, so that a one-line correction of any plan costs no search.
func commonLines(a, b []string) (prefix, suffix int) {
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix, suffix
}

// lineIDs numbers each distinct line, so that a comparison in the search costs
// the same however long the line.
func lineIDs(a, b []string) (idsA, idsB []int32) {
	seen := make(map[string]int32, len(a)+len(b))
	var next int32
	number := func(lines []string) []int32 {
		ids := make([]int32, len(lines))
		for i, line := range lines {
			id, ok := seen[line]
			if !ok {
				id = next
				seen[line] = id
				next++
			}
			ids[i] = id
		}
		return ids
	}
	return number(a), number(b)
}

// markShortestEdit is Myers' greedy search: it marks in inX and inY the lines of
// x and y a shortest edit keeps. Past diffBudget it stops having marked nothing,
// and every line of both goes out as changed.
//
// Each row holds, for one count of edits d, how far along x each diagonal has got
// (diagonal k = x-y = 2i-d for the row's ith entry). A row has d+1 entries, so the
// rows together are bounded by the budget too, at about 16 MiB.
func markShortestEdit(x, y []int32, inX, inY []bool) {
	n, m := int32(len(x)), int32(len(y)) //nolint:gosec // G115: both are lines of a document of at most 1 MiB
	var rows [][]int32
	var prev []int32
	steps := 0
	for d := 0; ; d++ {
		row := make([]int32, d+1)
		for i := range row {
			if steps >= diffBudget {
				return
			}
			steps++
			k := int32(2*i - d)
			at := diagonalEntry(prev, d, i)
			for at < n && at-k < m && x[at] == y[at-k] {
				at++
				steps++
			}
			row[i] = at
			// Only this diagonal reaches both ends. A path that has gone past the
			// end of x or y cannot come back, and walkBack never meets one: the
			// first d to reach this point got here inside both.
			if k == n-m && at >= n {
				walkBack(rows, n, m, inX, inY)
				return
			}
		}
		rows = append(rows, row)
		prev = row
	}
}

// enteredByPuttingIn is whether row d's ith diagonal is reached by a line put in,
// from the diagonal above it, and not by a line taken out, from the one below.
// prev is row d-1, whose entries are d, and whichever got further wins.
func enteredByPuttingIn(prev []int32, i int) bool {
	return i == 0 || (i < len(prev) && prev[i-1] < prev[i])
}

// diagonalEntry is how far along x a diagonal starts, before any common lines.
func diagonalEntry(prev []int32, d, i int) int32 {
	switch {
	case d == 0:
		return 0
	case enteredByPuttingIn(prev, i):
		return prev[i]
	default:
		return prev[i-1] + 1
	}
}

// walkBack follows rows back from the end of both, marking the common lines of
// each diagonal the path ran along. rows are the rows before the one that arrived.
func walkBack(rows [][]int32, x, y int32, inX, inY []bool) {
	mark := func(from, to, k int32) {
		for at := from; at < to; at++ {
			inX[at], inY[at-k] = true, true
		}
	}
	for d := len(rows); d > 0; d-- {
		k := x - y
		i := (int(k) + d) / 2
		if enteredByPuttingIn(rows[d-1], i) {
			from := rows[d-1][i]
			mark(from, x, k)
			x, y = from, from-k-1
		} else {
			from := rows[d-1][i-1]
			mark(from+1, x, k)
			x, y = from, from-k+1
		}
	}
	mark(0, x, 0)
}

// editScript is what happens to each line of both plans, in order: ' ' kept, '-'
// taken out, '+' put in. It takes one line from a side, or one from each, on every
// turn until both are used up, so every line gets an op; and a line is kept only
// where this function has just compared the two lines' full bytes. So applying the
// ops to a gives b, whatever inA and inB say.
func editScript(a, b []string, inA, inB []bool) []byte {
	ops := make([]byte, 0, len(a)+len(b))
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && inA[i] && inB[j] && a[i] == b[j]:
			ops = append(ops, ' ')
			i++
			j++
		case j == len(b) || (i < len(a) && !inA[i]):
			ops = append(ops, '-')
			i++
		default:
			ops = append(ops, '+')
			j++
		}
	}
	return ops
}

// renderHunks prints ops, leaving out only runs it has read and found to be ' '.
func renderHunks(a, b []string, ops []byte) string {
	var out strings.Builder
	i, j := 0, 0
	for p := 0; ; {
		change := p
		for change < len(ops) && ops[change] == ' ' {
			change++
		}
		if change == len(ops) {
			return out.String()
		}
		start := max(change-diffContext, p)
		i, j = i+start-p, j+start-p
		p = hunkEnd(ops, change)
		oldLines, newLines := sideCounts(ops[start:p])
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", hunkStart(i, oldLines), oldLines, hunkStart(j, newLines), newLines)
		for _, op := range ops[start:p] {
			if op == '+' {
				writeDiffLine(&out, op, b[j])
				j++
				continue
			}
			writeDiffLine(&out, op, a[i])
			i++
			if op == ' ' {
				j++
			}
		}
	}
}

// writeDiffLine marks a line with no final newline as a unified diff does, so
// that a change in the last newline alone shows and can be applied.
func writeDiffLine(out *strings.Builder, op byte, line string) {
	out.WriteByte(op)
	out.WriteString(line)
	if !strings.HasSuffix(line, "\n") {
		out.WriteString("\n\\ No newline at end of file\n")
	}
}

// hunkEnd is where the hunk holding the change at index change ends. Changes with
// no more than two hunks' worth of context between them share a hunk.
func hunkEnd(ops []byte, change int) int {
	last := change
	for q := change + 1; q < len(ops) && q-last <= 2*diffContext+1; q++ {
		if ops[q] != ' ' {
			last = q
		}
	}
	return min(last+1+diffContext, len(ops))
}

// sideCounts is how many lines of each plan a hunk's ops cover.
func sideCounts(ops []byte) (oldLines, newLines int) {
	for _, op := range ops {
		if op != '+' {
			oldLines++
		}
		if op != '-' {
			newLines++
		}
	}
	return oldLines, newLines
}

// hunkStart is a hunk's position on one side: the number of its first line, or,
// where the side has no line in the hunk, the number of the line it comes after.
func hunkStart(before, count int) int {
	if count == 0 {
		return before
	}
	return before + 1
}

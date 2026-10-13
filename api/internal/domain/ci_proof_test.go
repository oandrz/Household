package domain

import "testing"

// TestThatAlwaysFails exists only on a throwaway branch. It proves that a
// failing test turns the required check red and blocks the merge. It must
// never reach main.
func TestThatAlwaysFails(t *testing.T) {
	t.Fatal("deliberate failure: this pull request must not be mergeable")
}

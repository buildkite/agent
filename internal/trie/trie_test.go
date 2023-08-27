package trie_test

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/trie"
	"gotest.tools/v3/assert"
)

func TestTrieExists(t *testing.T) {
	t.Parallel()

	type check struct {
		value    string
		expected bool
	}

	for _, test := range []struct {
		name   string
		insert []string
		checks []check
	}{
		{
			name:   "insert_nothing_check_empty",
			insert: []string{},
			checks: []check{{"", false}},
		},
		{
			name:   "insert_nothing_check_one",
			insert: []string{},
			checks: []check{{"foo", false}},
		},
		{
			name:   "insert_one_check_one",
			insert: []string{"foo"},
			checks: []check{{"foo", true}},
		},
		{
			name:   "insert_two_check_two",
			insert: []string{"foo", ""},
			checks: []check{{"foo", true}, {"", true}},
		},
		{
			name:   "insert_two_check_one",
			insert: []string{"foo", ""},
			checks: []check{{"foo", true}, {"sdf", false}},
		},
		{
			name:   "insert_one_check_prefix",
			insert: []string{"foo"},
			checks: []check{{"foo", true}, {"fo", false}, {"f", false}, {"", false}},
		},
		{
			name:   "insert_two_check_prefix",
			insert: []string{"foo", "bar"},
			checks: []check{{"foo", true}, {"fo", false}, {"bar", true}, {"ba", false}},
		},
		{
			name:   "insert_three_check_four",
			insert: []string{"veni", "vidi", "vici"},
			checks: []check{{"veni", true}, {"vidi", true}, {"vici", true}, {"vice", false}},
		},
		{
			name:   "insert_one_check_four",
			insert: []string{"veni vidi vici"},
			checks: []check{{"veni", false}, {"vidi", false}, {"vici", false}, {"veni vidi vici", true}},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tr := trie.New()
			for _, s := range test.insert {
				tr.Insert(s)
			}

			for _, check := range test.checks {
				assert.Check(t, check.expected == tr.Exists(check.value), "value: %q", check.value)
			}
		})
	}
}

func TestTriePrefixExists(t *testing.T) {
	t.Parallel()

	type check struct {
		value    string
		expected bool
	}

	for _, test := range []struct {
		name   string
		insert []string
		checks []check
	}{
		{
			name:   "insert_nothing_check_empty",
			insert: []string{},
			checks: []check{{"", true}},
		},
		{
			name:   "insert_nothing_check_one",
			insert: []string{},
			checks: []check{{"foo", false}},
		},
		{
			name:   "insert_one_check_one",
			insert: []string{"foo"},
			checks: []check{{"foo", true}},
		},
		{
			name:   "insert_two_check_two",
			insert: []string{"foo", ""},
			checks: []check{{"foo", true}, {"", true}},
		},
		{
			name:   "insert_two_check_one",
			insert: []string{"foo", ""},
			checks: []check{{"foo", true}, {"sdf", false}},
		},
		{
			name:   "insert_one_check_prefix",
			insert: []string{"foo"},
			checks: []check{{"foo", true}, {"fo", true}, {"f", true}, {"", true}},
		},
		{
			name:   "insert_three_check_four",
			insert: []string{"veni", "vidi", "vici"},
			checks: []check{{"veni", true}, {"vidi", true}, {"vici", true}, {"vice", false}},
		},
		{
			name:   "insert_one_check_four",
			insert: []string{"veni vidi vici"},
			checks: []check{{"veni", true}, {"vidi", false}, {"vici", false}, {"veni vidi vici", true}},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tr := trie.New()
			for _, s := range test.insert {
				tr.Insert(s)
			}

			for _, check := range test.checks {
				assert.Check(t, check.expected == tr.PrefixExists(check.value), "value: %q", check.value)
			}
		})
	}
}

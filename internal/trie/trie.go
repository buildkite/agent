package trie

type node struct {
	exists   bool
	children map[rune]*node
}

func newNode() *node {
	return &node{
		exists:   false,
		children: make(map[rune]*node),
	}
}

// Trie is a trie data structure that supports insertion and lookup of strings
// and prefixes of inserted strings.
type Trie struct {
	root *node
	size int
}

// New returns a new Trie.
func New() *Trie {
	return &Trie{
		root: newNode(),
	}
}

func (t *Trie) Size() int {
	return t.size
}

// Insert inserts the string `word` into the Trie. It takes O(r) time where r
// is the length of `word`.
func (t *Trie) Insert(word string) {
	x := t.root
	for _, r := range word {
		if _, ok := x.children[r]; !ok {
			x.children[r] = newNode()
		}
		x = x.children[r]
	}
	if !x.exists {
		t.size++
	}
	x.exists = true
}

// Exists tests if the string `word` has been inserted into the Trie. It takes
// O(r) time where r is the length of `word`.
func (t *Trie) Exists(word string) bool {
	x := t.root
	for _, r := range word {
		if _, ok := x.children[r]; !ok {
			return false
		}
		x = x.children[r]
	}
	return x.exists
}

// PrefixExists tests if the any string with the prefix `word` has been inserted
// into the Trie. It takes O(r) time where r is the length of `word`.
func (t *Trie) PrefixExists(word string) bool {
	x := t.root
	for _, r := range word {
		if _, ok := x.children[r]; !ok {
			return false
		}
		x = x.children[r]
	}
	return true
}

// Contents returns all the strings that have been inserted into the Trie. The
// worst case time complexity is O(m) where m is the sum of the lengths of all
// strings that have been inserted.
func (t *Trie) Contents() []string {
	return contents(t.root, []rune{}, make([]string, 0, t.size))
}

func contents(x *node, prefix []rune, acc []string) []string {
	if x.exists {
		acc = append(acc, string(prefix))
	}

	for r, y := range x.children {
		prefix = append(prefix, r)
		acc = contents(y, prefix, acc)
		prefix = prefix[:len(prefix)-1]
	}

	return acc
}

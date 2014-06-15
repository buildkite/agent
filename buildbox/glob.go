// Source: https://github.com/aashah/glob

package buildbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

/*
 * glob - an expanded version
 *
 * This implementation of globbing will still take advantage of the Glob
 * function in path/filepath, however this extends the pattern to include '**'
 *
 */

/*
Algorithm details

segments = glob pattern split by os.path separator
define Entry:
    path, index into glob

Base Case:
    add Entry{root, 0}

while num entries > 0
    given an entry (path, idx)
    given glob segment (gb) at idx

    if gb == **
        move cur entry idx + 1

        for each dir inside path
            add new Entry{dir, idx}
    else
        add gb to path
        check for any results from normal globbing
        if none
            remove entry
        else
            if idx + 1 is out of bounds
                add result to final list
            else
                add an entry{result, idx + 1}

    keep current entry if it's idx is in bounds

*/

type matchEntry struct {
	path string
	idx  int
}

func Glob(root string, pattern string) (matches []string, e error) {
	if strings.Index(pattern, "**") < 0 {
		return filepath.Glob(filepath.Join(root, pattern))
	}

	segments := strings.Split(pattern, string(os.PathSeparator))

	workingEntries := []matchEntry{
		matchEntry{path: root, idx: 0},
	}

	for len(workingEntries) > 0 {

		var temp []matchEntry
		for _, entry := range workingEntries {
			workingPath := entry.path
			idx := entry.idx
			segment := segments[entry.idx]

			if segment == "**" {
				// add all subdirectories and move yourself one step further
				// into pattern
				entry.idx++

				subDirectories, err := getAllSubDirectories(entry.path)

				if err != nil {
					return nil, err
				}

				for _, name := range subDirectories {
					path := filepath.Join(workingPath, name)

					newEntry := matchEntry{
						path: path,
						idx:  idx,
					}

					temp = append(temp, newEntry)
				}

			} else {
				// look at all results
				// if we're at the end of the pattern, we found a match
				// else add it to a working entry
				path := filepath.Join(workingPath, segment)
				results, err := filepath.Glob(path)

				if err != nil {
					return nil, err
				}

				for _, result := range results {
					if idx+1 < len(segments) {
						newEntry := matchEntry{
							path: result,
							idx:  idx + 1,
						}

						temp = append(temp, newEntry)
					} else {
						matches = append(matches, result)
					}
				}
				// delete ourself regardless
				entry.idx = len(segments)
			}

			// check whether current entry is still valid
			if entry.idx < len(segments) {
				temp = append(temp, entry)
			}
		}

		workingEntries = temp
	}

	return
}

func isDir(path string) (val bool, err error) {
	fi, err := os.Stat(path)

	if err != nil {
		return false, err
	}

	return fi.IsDir(), nil
}

func getAllSubDirectories(path string) (dirs []string, err error) {

	if dir, err := isDir(path); err != nil || !dir {
		return nil, errors.New("Not a directory " + path)
	}

	d, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	files, err := d.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		path := filepath.Join(path, file)
		if dir, err := isDir(path); err == nil && dir {
			dirs = append(dirs, file)
		}
	}
	return
}

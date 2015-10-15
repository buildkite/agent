package lockfile_test

import (
	lockfile "."
	"fmt"
)

func ExampleLockfile() {
	lock, err := lockfile.New("/tmp/lock.me.now.lck")
	if err != nil {
		fmt.Println("Cannot init lock. reason: %v", err)
		panic(err)
	}
	err = lock.TryLock()

	// Error handling is essential, as we only try to get the lock.
	if err != nil {
		fmt.Println("Cannot lock \"%v\", reason: %v", lock, err)
		panic(err)
	}

	defer lock.Unlock()

	fmt.Println("Do stuff under lock")
	// Output: Do stuff under lock
}

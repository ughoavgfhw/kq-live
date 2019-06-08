package main

import (
	"os"
	"time"
)

type FileWatcher struct {
	stop chan struct{}
}

// Watches the file at the given path for changes. The callback is called with
// the open file when a change occurs. Spurious callbacks may occur if system
// calls return an error. If the file is removed, or opening it fails for any
// reason, the callback will receive a nil file.
//
// If the path is relative, changes to the working directory will affect the
// watched file.
//
// The returned watcher should be closed to stop callbacks and clean up
// resources.
//
// The implementation may use polling. Users should not expect file operations
// to trigger a callback immediately.
func WatchFile(path string, callback func(f *os.File)) *FileWatcher {
	fw := &FileWatcher{make(chan struct{})}
	go fw.watch(path, callback)
	return fw
}

// Closes the file watcher. No callbacks will occur after this function
// returns, though they may happen concurrently with this function.
func (fw *FileWatcher) Close() error {
	// Send a struct instead of just closing the channel. This blocks the
	// current goroutine until the background loop read the channel, ensuring
	// that no more callbacks will happen.
	fw.stop <- struct{}{}
	return nil
}

// Quicker check using the open file. The mod time is compared to check for
// writes, and the name is compared to check for moves. This may have false
// matches if the file is moved but has the same base name, which won't be
// caught until the next full check. Returns true if there appears to be a
// change, or an error occurs during the check.
func quickStatDiffers(f *os.File, curr os.FileInfo) bool {
	if new, err := f.Stat(); err == nil &&
		new.ModTime().Equal(curr.ModTime()) &&
		new.Name() == curr.Name() {
		return false
	}
	return true
}

// Full check running a fresh stat. Checks for mod time changes, and that the
// new stat actually matches the same underlying file. A file-not-found error
// is treated as matching a nil current stat. Any other error case is treated
// as not matching.
func fullStatDiffers(path string, curr os.FileInfo) (os.FileInfo, bool) {
	if new, err := os.Stat(path); err != nil {
		treatAsDiff := curr != nil || !os.IsNotExist(err)
		return nil, treatAsDiff
	} else {
		if curr == nil ||
			!new.ModTime().Equal(curr.ModTime()) ||
			!os.SameFile(new, curr) {
			return new, true
		}
		return nil, false
	}
}

func (fw *FileWatcher) watch(path string, callback func(f *os.File)) {
	const checkEveryDur = time.Second
	const fullCheckEveryN = 10

	var f *os.File
	defer func() { f.Close() }()  // Wrap in a function to use the future f.
	var curr os.FileInfo
	tick := time.NewTicker(checkEveryDur)
	defer tick.Stop()
	count := 0
	for {
		select {
		case <-tick.C: break
		case <-fw.stop: return
		}
		count -= 1
		if count > 0 && !quickStatDiffers(f, curr) {
			continue
		}
		count = fullCheckEveryN
		if new, diff := fullStatDiffers(path, curr); !diff {
			continue
		} else {
			curr = new
		}
		defer f.Close()  // Close the current file after the callback runs.
		f, _ = os.Open(path)
		callback(f)
	}
}

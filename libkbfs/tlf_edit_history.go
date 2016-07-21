// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"
	"time"

	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

// TlfEditNotificationType indicates what type of edit happened to a
// file.
type TlfEditNotificationType int

const (
	// FileCreated indicates a new file.
	FileCreated TlfEditNotificationType = iota
	// FileModified indicates an existing file that was written to.
	FileModified
	// FileDeleted indicates an existing file that was deleted.  It
	// doesn't appear in the edit history, only in individual edit
	// updates.
	FileDeleted
)

// TlfEdit represents an individual update about a file edit within a
// TLF.
type TlfEdit struct {
	Filepath  string // relative to the TLF root
	Type      TlfEditNotificationType
	Writer    keybase1.UID
	LocalTime time.Time // reflects difference between server and local clock
}

// TlfEditHistory allows you to get the update history about a
// particular TLF.
type TlfEditHistory struct {
	config Config
	tlf    TlfID

	lock  sync.Mutex
	edits map[keybase1.UID][]TlfEdit
}

func (teh *TlfEditHistory) getEditsCopy() map[keybase1.UID][]TlfEdit {
	teh.lock.Lock()
	defer teh.lock.Unlock()
	if teh.edits == nil {
		return nil
	}
	edits := make(map[keybase1.UID][]TlfEdit)
	for user, userEdits := range teh.edits {
		userEditsCopy := make([]TlfEdit, 0, len(userEdits))
		copy(userEditsCopy, userEdits)
		edits[user] = userEditsCopy
	}
	return edits
}

// GetComplete returns the most recently known set of clustered edit
// history for this TLF.
func (teh *TlfEditHistory) GetComplete(ctx context.Context) (
	map[keybase1.UID][]TlfEdit, error) {
	currEdits := teh.getEditsCopy()
	if currEdits != nil {
		return currEdits, nil
	}

	// We have no history -- fetch from the server until we have a
	// complete history.

	// * Get current head for this folder.
	// * If unmerged, get all the unmerged updates.
	// * Then starting from the head, work backwards mdMax revisions at a time.
	// * Estimate the number of per-writer file operations by keeping a count
	//   of the createOps and syncOps found.
	// * Once the estimate hits the threshold for each writer, calculate the
	//   chains using all those MDs, and build the real edit map (discounting
	//   deleted files, etc).
	// * If any writer is below the threshold, reduce their count and continue
	//   backwards through the history, repeating the same process until the
	// chains give the right number, or we hit revision 1.

	currEdits = make(map[keybase1.UID][]TlfEdit)
	return currEdits, nil
}

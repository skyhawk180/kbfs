// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/keybase/client/go/protocol"
	"github.com/stretchr/testify/require"

	"golang.org/x/net/context"
)

func TestJournalMDOpsBasics(t *testing.T) {
	// setup
	tempdir, err := ioutil.TempDir(os.TempDir(), "journal_md_ops")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	config := MakeTestConfigOrBust(t, "test_user")
	defer config.Shutdown()

	// TODO: Sanity-check the root directory, e.g. create
	// it if it doesn't exist, make sure that it doesn't
	// point to /keybase itself, etc.
	log := config.MakeLogger("")
	jServer := makeJournalServer(
		config, log, tempdir,
		config.BlockServer(), config.MDOps(), config.MDServer())
	config.SetBlockServer(jServer.blockServer())
	config.SetMDOps(jServer.mdOps())

	mdOps := config.MDOps()
	ctx := context.Background()

	_, uid, err := config.KBPKI().GetCurrentUserInfo(ctx)
	require.NoError(t, err)

	// (1) get metadata -- allocates an ID
	bh, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	h, err := MakeTlfHandle(ctx, bh, config.KBPKI())
	require.NoError(t, err)

	id, irmd, err := mdOps.GetForHandle(ctx, h, Merged)
	require.NoError(t, err)
	require.Equal(t, ImmutableRootMetadata{}, irmd)

	// (2) push some new metadata blocks
	prevRoot := MdID{}
	middleRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var rmd RootMetadata
		err := updateNewBareRootMetadata(&rmd.BareRootMetadata, id, bh)
		require.NoError(t, err)
		rmd.tlfHandle = h
		rmd.SerializedPrivateMetadata = make([]byte, 1)
		rmd.SerializedPrivateMetadata[0] = 0x1
		rmd.Revision = MetadataRevision(i)
		FakeInitialRekey(&rmd.BareRootMetadata, bh)
		if i > 1 {
			rmd.PrevRoot = prevRoot
		}
		mdID, err := mdOps.Put(ctx, &rmd)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
		if i == 5 {
			middleRoot = prevRoot
		}
	}

	// (3) trigger a conflict
	var rmd RootMetadata
	err = updateNewBareRootMetadata(&rmd.BareRootMetadata, id, bh)
	require.NoError(t, err)
	rmd.tlfHandle = h
	rmd.Revision = MetadataRevision(10)
	rmd.SerializedPrivateMetadata = make([]byte, 1)
	rmd.SerializedPrivateMetadata[0] = 0x1
	FakeInitialRekey(&rmd.BareRootMetadata, bh)
	rmd.PrevRoot = prevRoot
	_, err = mdOps.Put(ctx, &rmd)
	require.IsType(t, MDServerErrorConflictRevision{}, err)

	// (4) push some new unmerged metadata blocks linking to the
	//     middle merged block.
	prevRoot = middleRoot
	bid, err := config.Crypto().MakeRandomBranchID()
	require.NoError(t, err)
	for i := MetadataRevision(6); i < 41; i++ {
		var rmd RootMetadata
		err := updateNewBareRootMetadata(&rmd.BareRootMetadata, id, bh)
		require.NoError(t, err)
		rmd.tlfHandle = h
		rmd.Revision = MetadataRevision(i)
		rmd.SerializedPrivateMetadata = make([]byte, 1)
		rmd.SerializedPrivateMetadata[0] = 0x1
		rmd.PrevRoot = prevRoot
		FakeInitialRekey(&rmd.BareRootMetadata, bh)
		rmd.WFlags |= MetadataFlagUnmerged
		rmd.BID = bid
		mdID, err := mdOps.Put(ctx, &rmd)
		require.NoError(t, err)
		prevRoot = mdID
		require.NoError(t, err)
	}

	// (5) check for proper unmerged head
	head, err := mdOps.GetUnmergedForTLF(ctx, id, bid)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, MetadataRevision(40), head.Revision)

	// (6a) try to get unmerged range
	rmdses, err := mdOps.GetUnmergedRange(ctx, id, bid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 35, len(rmdses))
	for i := MetadataRevision(6); i < 16; i++ {
		require.Equal(t, i, rmdses[i-6].Revision)
	}

	// (6b) try to get unmerged range subset.
	rmdses, err = mdOps.GetUnmergedRange(ctx, id, bid, 7, 14)
	require.NoError(t, err)
	require.Equal(t, 8, len(rmdses))
	for i := MetadataRevision(7); i <= 14; i++ {
		require.Equal(t, i, rmdses[i-7].Revision)
	}

	// (7) prune unmerged
	err = mdOps.PruneBranch(ctx, id, bid)
	require.NoError(t, err)

	// (8) verify head is pruned
	head, err = mdOps.GetUnmergedForTLF(ctx, id, NullBranchID)
	require.NoError(t, err)
	require.Nil(t, head)

	// (9) verify revision history is pruned
	rmdses, err = mdOps.GetUnmergedRange(ctx, id, NullBranchID, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 0, len(rmdses))

	// (10) check for proper merged head
	head, err = mdOps.GetForTLF(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, MetadataRevision(10), head.Revision)

	// (11) try to get merged range
	rmdses, err = mdOps.GetRange(ctx, id, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmdses))
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmdses[i-1].Revision)
	}
}

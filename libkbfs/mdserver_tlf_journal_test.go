// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/stretchr/testify/require"
)

type singleEncryptionKeyGetter struct {
	k TLFCryptKey
}

func (g singleEncryptionKeyGetter) GetTLFCryptKeyForEncryption(
	ctx context.Context, md ReadOnlyRootMetadata) (TLFCryptKey, error) {
	return g.k, nil
}

func getTlfJournalLength(t *testing.T, s *mdServerTlfJournal) int {
	len, err := s.journalLength()
	require.NoError(t, err)
	return int(len)
}

// TestMDServerTlfJournalBasic copies TestMDServerBasics, but for a
// single mdServerTlfJournal.
func TestMDServerTlfJournalBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDServerTlfJournal(codec, crypto, tempdir)
	defer s.shutdown()

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (1) Validate merged branch is empty.

	head, err := s.get(uid)
	require.NoError(t, err)
	require.Nil(t, head)

	require.Equal(t, 0, getTlfJournalLength(t, s))

	// (2) Push some new metadata blocks.

	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	require.Equal(t, 10, getTlfJournalLength(t, s))

	// (10) Check for proper merged head.

	head, err = s.get(uid)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, MetadataRevision(10), head.Revision)

	// (11) Try to get merged range.

	rmds, err := s.getRange(uid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmds))
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmds[i-1].Revision)
	}

	require.Equal(t, 10, getTlfJournalLength(t, s))
}

func TestMDServerTlfJournalBranchConversion(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDServerTlfJournal(codec, crypto, tempdir)
	defer s.shutdown()

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (2) Push some new metadata blocks.

	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	log := logger.NewTestLogger(t)
	bid, lastMdID, err := s.convertToBranch(log)
	require.NoError(t, err)

	rmds, err := s.getRange(uid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmds))
	prevRoot = MdID{}
	// TODO: Check first PrevRoot.
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmds[i-1].Revision)
		require.Equal(t, bid, rmds[i-1].BID)
		require.Equal(t, Unmerged, rmds[i-1].MergedStatus())

		if prevRoot != (MdID{}) {
			require.Equal(t, prevRoot, rmds[i-1].PrevRoot)
		}

		currRoot, err := crypto.MakeMdID(rmds[i-1])
		require.NoError(t, err)
		prevRoot = currRoot
	}
	require.Equal(t, lastMdID, prevRoot)

	require.Equal(t, 10, getTlfJournalLength(t, s))
}

func TestMDServerTlfJournalFlushBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDServerTlfJournal(codec, crypto, tempdir)
	defer s.shutdown()

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (2) Push some new metadata blocks.

	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	ctx := context.Background()
	var realCrypto Crypto
	var mdserver MDServer
	log := logger.NewTestLogger(t)
	for {
		flushed, err := s.flushOne(ctx, realCrypto, mdserver, log)
		require.NoError(t, err)
		if !flushed {
			break
		}
	}
}

package libkbfs

import (
	"github.com/keybase/client/go/client"
	"github.com/keybase/client/go/libkb"
	keybase1 "github.com/keybase/client/protocol/go"
	"github.com/maxtaco/go-framed-msgpack-rpc/rpc2"
)

// CryptoClient implements the Crypto interface by sending RPCs to the
// keybase daemon to perform signatures using the device's current
// signing key.
type CryptoClient struct {
	CryptoCommon
	ctx    *libkb.GlobalContext
	client keybase1.GenericClient
}

var _ Crypto = (*CryptoClient)(nil)

// NewCryptoClient constructs a new CryptoClient.
func NewCryptoClient(codec Codec, ctx *libkb.GlobalContext) (*CryptoClient, error) {
	_, xp, err := ctx.GetSocket()
	if err != nil {
		return nil, err
	}

	srv := rpc2.NewServer(xp, libkb.WrapError)

	protocols := []rpc2.Protocol{
		client.NewSecretUIProtocol(),
	}

	for _, p := range protocols {
		if err := srv.Register(p); err != nil {
			if _, ok := err.(rpc2.AlreadyRegisteredError); !ok {
				return nil, err
			}
		}
	}

	client := rpc2.NewClient(xp, libkb.UnwrapError)
	return newCryptoClientWithClient(codec, ctx, client), nil
}

// newCryptoClientWithClient should only be used for testing.
func newCryptoClientWithClient(codec Codec, ctx *libkb.GlobalContext, client keybase1.GenericClient) *CryptoClient {
	return &CryptoClient{CryptoCommon{codec}, ctx, client}
}

// Sign implements the Crypto interface for CryptoClient.
func (c *CryptoClient) Sign(msg []byte) (sigInfo SignatureInfo, err error) {
	defer func() {
		libkb.G.Log.Debug("Signed %d-byte message with %s: err=%v", len(msg), sigInfo, err)
	}()
	cc := keybase1.CryptoClient{Cli: c.client}
	ed25519SigInfo, err := cc.SignED25519(keybase1.SignED25519Arg{
		SessionID: 0,
		Msg:       msg,
		Reason:    "to use kbfs",
	})
	if err != nil {
		return
	}

	sigInfo = SignatureInfo{
		Version:      SigED25519,
		Signature:    ed25519SigInfo.Sig[:],
		VerifyingKey: VerifyingKey{libkb.NaclSigningKeyPublic(ed25519SigInfo.PublicKey).GetKid()},
	}
	return
}

// DecryptTLFCryptKeyClientHalf implements the Crypto interface for
// CryptoClient.
func (c *CryptoClient) DecryptTLFCryptKeyClientHalf(publicKey TLFEphemeralPublicKey, encryptedClientHalf EncryptedTLFCryptKeyClientHalf) (clientHalf TLFCryptKeyClientHalf, err error) {
	if encryptedClientHalf.Version != TLFEncryptionBox {
		err = UnknownTLFEncryptionVer{encryptedClientHalf.Version}
		return
	}

	var encryptedData keybase1.EncryptedBytes32
	if len(encryptedClientHalf.EncryptedData) != len(encryptedData) {
		err = libkb.DecryptionError{}
		return
	}
	copy(encryptedData[:], encryptedClientHalf.EncryptedData)

	var nonce keybase1.BoxNonce
	if len(encryptedClientHalf.Nonce) != len(nonce) {
		err = libkb.DecryptionError{}
		return
	}
	copy(nonce[:], encryptedClientHalf.Nonce)

	cc := keybase1.CryptoClient{Cli: c.client}
	decryptedClientHalf, err := cc.UnboxBytes32(keybase1.UnboxBytes32Arg{
		SessionID:        0,
		EncryptedBytes32: encryptedData,
		Nonce:            nonce,
		PeersPublicKey:   keybase1.BoxPublicKey(publicKey.PublicKey),
		Reason:           "to use kbfs",
	})
	if err != nil {
		return
	}

	clientHalf = TLFCryptKeyClientHalf{decryptedClientHalf}
	return
}

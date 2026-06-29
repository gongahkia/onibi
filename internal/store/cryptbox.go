package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gongahkia/onibi/internal/envelope"
)

const (
	storeCryptInfo = "onibi-store-cryptbox-v1"
	storeCryptType = "store-row"
)

var ErrCryptBoxUnavailable = errors.New("store cryptbox unavailable")

type CryptBox struct {
	codec *envelope.Codec
}

func NewCryptBox(masterKey []byte) (*CryptBox, error) {
	codec, err := envelope.NewCodec(masterKey, storeCryptInfo)
	if err != nil {
		return nil, err
	}
	return &CryptBox{codec: codec}, nil
}

func RowAAD(table, rowID, column string) []byte {
	var b bytes.Buffer
	for _, part := range []string{table, rowID, column} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = b.Write(size[:])
		_, _ = b.WriteString(part)
	}
	return b.Bytes()
}

func (b *CryptBox) Seal(ctx context.Context, plaintext, aad []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.codec == nil {
		return nil, ErrCryptBoxUnavailable
	}
	sealed, err := b.codec.Seal(storeCryptType, plaintext, aad)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return sealed, nil
}

func (b *CryptBox) Open(ctx context.Context, sealed, aad []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.codec == nil {
		return nil, ErrCryptBoxUnavailable
	}
	typ, plaintext, err := b.codec.Open(sealed, aad)
	if err != nil {
		return nil, err
	}
	if typ != storeCryptType {
		return nil, fmt.Errorf("bad store crypt frame type %q", typ)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plaintext, nil
}

package test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/andrewKyres/msg-server/internal/message"
	"github.com/stretchr/testify/assert"
)

func TestByteConversion(t *testing.T) {
	testInts := []uint64{12, 100000, 45000000}
	for _, i := range testInts {
		// encode & decode works
		assert.Equal(t, message.BytesToUint64(message.UInt64ToBytes(i)), i)
	}

	// encoding works as expected
	assert.Equal(t, message.UInt64ToBytes(testInts[0]), []byte{12, 0, 0, 0, 0, 0, 0, 0})
}

func TestReadBytes(t *testing.T) {
	var buf bytes.Buffer

	var wg sync.WaitGroup
	wg.Add(1)
	read := func() {
		defer wg.Done()
		message.ReadBytes(&buf, 8)
	}

	// Read 4 then 4
	buf.Write([]byte{0, 0, 0, 0})
	go read()
	buf.Write([]byte{0, 0, 0, 0})
	wg.Wait()

	assert.Equal(t, len(buf.Bytes()), 0)
}

func TestRWClientIds(t *testing.T) {
	type Test struct {
		Input  []uint64
		Output []byte
	}

	numClients := uint64(256) // There can exist >255 clients
	clientIds := make([]uint64, numClients)
	for i := uint64(1); i < numClients; i++ {
		clientIds[i] = i
	}
	clientBytes := make([]byte, 0, 8+(8*numClients))
	for _, i := range clientIds {
		clientBytes = append(clientBytes, message.UInt64ToBytes(clientIds[i])...)
	}

	tests := []Test{
		{[]uint64{12}, []byte{0x1, 0, 0, 0, 0, 0, 0, 0, 12, 0, 0, 0, 0, 0, 0, 0}},
		{clientIds, append(message.UInt64ToBytes(numClients), clientBytes...)},
	}
	var buf bytes.Buffer
	for _, test := range tests {
		message.WriteClientIds([]uint64(test.Input), &buf)

		assert.Equal(t, buf.Bytes(), test.Output)

		ids, err := message.ReadClientIds(&buf)
		assert.NoError(t, err)
		assert.Equal(t, ids, test.Input)
	}
}

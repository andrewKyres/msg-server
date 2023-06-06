package message

import (
	"encoding/binary"
	"fmt"
	"io"
)

type MessageType byte

const (
	IdentityMessage MessageType = iota
	ListMessage
	RelayMessage
)

func MessageTypeToByte(t MessageType) []byte {
	return []byte{byte(t)}
}

func UInt64ToBytes(n uint64) []byte {
	ret := make([]byte, 8)
	binary.LittleEndian.PutUint64(ret, n)
	return ret
}

func BytesToUint64(bs []byte) uint64 {
	return binary.LittleEndian.Uint64(bs)
}

// WriteClientIds writes the list of clientIds passed to the connection.
// This uses the protocol of 8 bytes initially for a uint64 for the number of clientIds
// coming, and then writes all those ids.
func WriteClientIds(clientIds []uint64, conn io.Writer) error {
	// write the first 8 bytes indicating number of uint64s coming
	// TODO can be 1 byte, given the current spec of 255 max recipients
	conn.Write(UInt64ToBytes(uint64(len(clientIds))))

	for _, clientId := range clientIds {
		bsToWrite := UInt64ToBytes(clientId)
		_, err := conn.Write(bsToWrite)
		if err != nil {
			return err
		}
	}

	return nil
}

// ReadClientIds will read a stream of client ids being sent over the connection.
// This uses the protocol of 8 bytes initially for a uint64 for the number of clientIds
// coming, and then reads all those ids.
func ReadClientIds(conn io.Reader) ([]uint64, error) {
	// Read first 8 bytes for uint64 of num clientIds to expect.
	first8, err := ReadBytes(conn, 8)
	if err != nil {
		return nil, fmt.Errorf("read error: %v", err)
	}

	// Create return array, and read all bytes for clientIds
	numClientIds := BytesToUint64(first8)
	clientIds := make([]uint64, numClientIds)
	clientIdBytes, err := ReadBytes(conn, numClientIds*8) // 1 uint64 per clientId expected
	if err != nil {
		return nil, fmt.Errorf("read error: %v", err)
	}
	for i := uint64(0); i < numClientIds; i++ {
		// get n+8 bytes, convert to int
		clientId := BytesToUint64(clientIdBytes[i*8 : i*8+8])
		clientIds[i] = clientId
	}

	return clientIds, nil
}

// ReadBytes will block until x bytes are read from the connection. It currently
// does not timeout but probably should have some concept of an overall timeout.
func ReadBytes(conn io.Reader, x uint64) ([]byte, error) {
	bytes := make([]byte, 0, x)
	filled := uint64(0)
	for filled < x {
		// TODO fix this which is over-allocating bytes, and could be improved with a minor buffer but honestly who
		// cares for the number of bytes we have. Although a ~1024 buffer would probably be good.
		remainingBuffer := make([]byte, x-filled)
		n, _ := conn.Read(remainingBuffer)
		// TODO error handle
		filled += uint64(n)
		bytes = append(bytes, remainingBuffer[:n]...)
	}

	return bytes, nil
}

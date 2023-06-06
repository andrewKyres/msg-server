package client

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/andrewKyres/msg-server/internal/message"
)

type IncomingMessage struct {
	SenderID uint64
	Body     []byte
}

type Client struct {
	Conn net.Conn
	// We set up a mutex for all usage of the connection. Any write or read operation should acquire it & release it
	// after use.
	mu             sync.Mutex
	CloseRequested bool
	Id             uint64
}

func New() *Client {
	return &Client{}
}

func (cli *Client) Connect(serverAddr *net.TCPAddr) error {
	conn, err := net.Dial("tcp", fmt.Sprintf(":%d", serverAddr.Port))
	if err != nil {
		fmt.Printf("failed to connect: %v\n", err)
		return err
	}

	cli.Conn = conn

	// TODO handle wrapping up as needed

	return nil
}

func (cli *Client) Close() error {
	fmt.Println("Closing the connection to the server")
	cli.CloseRequested = true
	return nil
}

func (cli *Client) WhoAmI() (uint64, error) {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	cli.Conn.Write(message.MessageTypeToByte(message.IdentityMessage))
	// after writing, we expect a uint64 back as our id
	bs, err := message.ReadBytes(cli.Conn, 8)
	if err != nil {
		return 0, nil
	}
	clientId := message.BytesToUint64(bs)
	return clientId, nil
}

func (cli *Client) ListClientIDs() ([]uint64, error) {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	cli.Conn.Write(message.MessageTypeToByte(message.ListMessage))
	// after writing, we read back the client ids response
	return message.ReadClientIds(cli.Conn)
}

func (cli *Client) SendMsg(recipients []uint64, body []byte) error {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	// First we send the type, then number of recipients, then recipients, then number of bytes in the body, then body
	cli.Conn.Write(message.MessageTypeToByte(message.RelayMessage))
	message.WriteClientIds(recipients, cli.Conn)
	cli.Conn.Write(message.UInt64ToBytes(uint64(len(body))))
	cli.Conn.Write(body)

	// no response from server, so we good.

	return nil
}

func (cli *Client) HandleIncomingMessages(writeCh chan<- IncomingMessage) {
	// Our schema will try to acquire a lock on the connection every 50ms and check for incoming read traffic.
	// Our write/read functionality (eg whoamI) acquires locks before the write, and so won't interfere with this
	// read traffic.
	// We have a possible race condition here, though:
	// 1. cli1.SendMsg (acuires cli1 lock, & server sets up lock on cli2)
	// 2. cli2.WhoAmI (acquires cli2 lock & writes bytes to the connection - server doesn't respond yet due to not having a lock)
	// 3. cli2 is waiting on read response from WhoAmI, but receives bytes from the sendMsg & interprets them incorrectly
	// To solve this, we need to set up a single read goroutine on the connection which consumes all messages
	// and returns those to goroutines making requests (eg a lock on write/read functionality), and a separate one for
	// incoming bytes meant as incoming messages (to the incomingMessage channel).
	// We then must prepend a MessageType byte per payload (for each batch of a single relayed message)

	for {
		if cli.CloseRequested {
			break
		}

		// We check every 50ms for traffic. We acquire a mutex on the connection to do so.
		time.Sleep(50 * time.Millisecond)
		cli.mu.Lock()

		// We check for traffic on the conn. If we get nothing in 5ms (things would be queued up or we'll get it next time we check)
		// then skip this iteration
		cli.Conn.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
		one := make([]byte, 1)
		n, err := cli.Conn.Read(one)
		cli.Conn.SetReadDeadline(time.Time{}) // pass a 0 value to remove read deadline again

		if n == 0 || err != nil {
			cli.mu.Unlock()
			continue
		}

		// We got some bytes, so actually handle receiving a message.
		// tbh, there might be some deadlock race between client & server mutexes, but unsure.
		// honestly, probably just put a comment somewhere being like "eh, there's probably deadlock issues somewhere here
		// and that in prod we'd have timeouts on stuff to get out of those states"
		// here, read from the conn (if we have a mutex on reading, and get data, and send it to the channel)
		next7, _ := message.ReadBytes(cli.Conn, 7)
		first8 := append(one, next7...)
		senderId := message.BytesToUint64(first8)
		next8, _ := message.ReadBytes(cli.Conn, 8)
		numBytes := message.BytesToUint64(next8)

		// Now read all num bytes & put into return struct
		bs, _ := message.ReadBytes(cli.Conn, numBytes) // TODO error handle

		cli.mu.Unlock() // TODO wrap stuff between lock & unlock in a function to use defer!

		msg := IncomingMessage{
			SenderID: senderId,
			Body:     bs,
		}
		writeCh <- msg
	}
}

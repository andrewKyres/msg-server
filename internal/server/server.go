package server

import (
	"fmt"
	"net"
	"sort"
	"sync"

	"github.com/andrewKyres/msg-server/internal/message"
)

type MessageType byte

type ConnWithMetadata struct {
	id   uint64
	conn net.Conn
	mu   sync.Mutex
}

type Server struct {
	Connections   map[uint64]*ConnWithMetadata // unsure how to purge this when connections get closed by the client. seems like a good idea
	NextId        uint64
	StopRequested bool
	Lis           net.Listener
	// DoneCh        chan bool // To wait forever for manual testing
}

func New() *Server {
	return &Server{
		Connections:   make(map[uint64]*ConnWithMetadata),
		NextId:        1, // The setup integration tests expect IDs to start at 1
		StopRequested: false,
	}
}

func (server *Server) Start(laddr *net.TCPAddr) error {
	// server.DoneCh = make(chan bool)

	// TODO probably move this stuff into the listenForConns logic.
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", laddr.Port))
	if err != nil {
		return fmt.Errorf("failed to start listener: %v", err)
	}
	server.Lis = lis

	go server.listenForConns(lis) // Server listens for connections separately, so we can return

	fmt.Printf("Server is ready to handle client connections and messages at port %d\n", laddr.Port)

	return nil
}

func (server *Server) listenForConns(lis net.Listener) {
	for {
		if server.StopRequested {
			break
		}

		conn, err := lis.Accept()
		if err != nil {
			// unclear what to do here. Log for server admins? maybe stdout is fine for now.
			fmt.Printf("failed to accept conn: %v\n", err)
			continue
		}

		id := server.NextId
		server.NextId++
		connWithMeta := &ConnWithMetadata{
			id:   id,
			conn: conn,
		}
		server.Connections[id] = connWithMeta
		go server.handleConn(connWithMeta)
	}
}

func (server *Server) handleConn(conn *ConnWithMetadata) error {
	// We loop while the connection remains open, handling any incoming requests.
	for {
		if server.StopRequested {
			break
		}

		// Read 1 byte initially to get the message type, then handle appropriately from there.
		// While we're waiting to receive a request, we don't lock the connection so that any messages
		// can still be relayed.
		firstByte, err := message.ReadBytes(conn.conn, 1)
		// As soon as we have 1 byte, we acquire a lock on the connection to read the message & write our response
		// to avoid clobbering any messages being relayed right now.
		conn.mu.Lock()
		if err != nil {
			return fmt.Errorf("read error: %v", err)
		}
		// TODO What if client closed conn here, need to clean up (maybe just break & have a defer somewhere
		// cus eventually this is all going to need to indicate in channels that it's closed so that server can stop)
		messageType := message.MessageType(firstByte[0])

		// TODO If a client sends a bad message, just kill the connection with that client

		switch messageType {
		case message.IdentityMessage:
			conn.conn.Write(message.UInt64ToBytes(conn.id))
		case message.ListMessage:
			// TODO error handling
			ids := server.ListClientIDs(conn.id)
			message.WriteClientIds(ids, conn.conn)
		case message.RelayMessage:
			clientIds, err := message.ReadClientIds(conn.conn)
			if err != nil {
				// TODO error handling (and unlock!)
			}
			server.Relaymessage(conn.id, clientIds, conn.conn)
		default:
			// TODO bad request type? What to do? ignore this? return an error somewhere? close the connection?
		}

		conn.mu.Unlock() // TODO if everything needed within the mutex lock is pulled out, we can be more idiomatic & less error prone by using defer
	}

	return nil
}

func (server *Server) Relaymessage(senderId uint64, clientIds []uint64, incomingConn net.Conn) error {
	// TODO acquire a mutex on each connection needed, and then buffer out message
	// Also set up a defer function here to release those mutexes. Eg if there's an error mid-relay

	// We expect the first 8 bytes to be a uint64 indicating length of message to relay.
	bs, err := message.ReadBytes(incomingConn, 8)
	if err != nil {
		return err
	}
	messageLength := message.BytesToUint64(bs)

	// We want to acquire a mutex on all clients we're relaying a message to.
	// if the message to relay includes the sender, we should
	// not look to acquire a lock on that conn as it's already locked
	// before this fn got called. We will still relay the message though.

	// First we write the senderId to the buffer, and then the message length
	for _, clientId := range clientIds {
		conn, ok := server.Connections[clientId]
		if !ok {
			fmt.Println("invalid clientId", clientId)
		}

		if clientId != senderId {
			conn.mu.Lock()
		}

		// TODO error handling
		conn.conn.Write(message.UInt64ToBytes(senderId)) // write the sender id
		conn.conn.Write(bs)                              // write the length of the message to relay
	}

	// We have a list of client Ids, and now we want to buffer through the rest of the bytes
	// to all those client connections. We'll read in 1024 bytes at a time, and write those out to
	// clients
	bufferSize := uint64(1024)
	for i := uint64(0); i < messageLength; i += bufferSize {
		// 1024 or remaining bytes
		bytesToRead := bufferSize
		if bytesToRead > messageLength-i {
			bytesToRead = messageLength - i
		}
		buffer, err := message.ReadBytes(incomingConn, bytesToRead)
		if err != nil {
			return err
		}

		// Write out the buffer
		for _, clientId := range clientIds {
			conn, ok := server.Connections[clientId]
			if !ok {
				fmt.Println("invalid clientId", clientId)
			}
			// TODO error handling
			conn.conn.Write(buffer)
		}
	}

	// release all the locks
	for _, clientId := range clientIds {
		conn, ok := server.Connections[clientId]
		if !ok {
			fmt.Println("invalid clientId", clientId)
		}

		if clientId != senderId {
			conn.mu.Unlock()
		}
	}

	return nil
}

func (server *Server) ListClientIDs(clientId uint64) []uint64 {
	ids := make([]uint64, 0, len(server.Connections))
	for id := range server.Connections {
		if id != clientId { // filter out the caller's clientId
			ids = append(ids, id)
		}
	}

	// Sort the clientIds since tests expect them sorted. Can use an ordered map for connections
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	return ids
}

func (server *Server) Stop() error {
	// prevent new connections
	server.StopRequested = true

	// Cleanup all existing connections
	for _, conn := range server.Connections {
		// Get a mutex on all open connections (wait for them to finish sending as needed)
		conn.mu.Lock()
		conn.conn.Close()
	}

	server.Lis.Close()
	// server.DoneCh <- true
	return nil
}

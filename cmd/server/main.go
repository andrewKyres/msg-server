package main

import (
	"net"

	"github.com/andrewKyres/msg-server/internal/server"
)

func main() {
	server := server.New()
	server.Start(&net.TCPAddr{
		Port: 3000,
	})

	// now wait forever
	<-make(chan bool)
}

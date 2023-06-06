package main

import (
	"flag"
	"fmt"
	"net"

	"github.com/andrewKyres/msg-server/internal/client"
)

func main() {
	shouldSendMsg := flag.Bool("sendMsg", false, "will send a message after init if true")
	flag.Parse()

	cli := client.New()
	cli.Connect(&net.TCPAddr{
		Port: 3000,
	})

	client2Ch := make(chan client.IncomingMessage)
	go cli.HandleIncomingMessages(client2Ch)

	if shouldSendMsg != nil && *shouldSendMsg {
		// grab all client Ids, and broadcast to all of them
		clientIds, _ := cli.ListClientIDs()
		cli.SendMsg(clientIds, []byte("Hello World"))
		fmt.Println("sent message out!", "Hello World")
	} else {
		fmt.Println("Expect a message! Print whoamI, then wait for message")
		fmt.Println(cli.WhoAmI())

		incomingMessage := <-client2Ch
		fmt.Println("received message!", string(incomingMessage.Body))
	}
}

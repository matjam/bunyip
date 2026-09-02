package network_test

import (
	"fmt"
	"time"

	"github.com/matjam/bunyip/network"
)

// Messages are plain structs; both ends register the same types in the
// same order.
type Chat struct{ From, Text string }

func Example() {
	reg := network.NewRegistry().Register(Chat{})
	server, err := network.Listen("127.0.0.1:0", reg)
	if err != nil {
		panic(err)
	}
	defer server.Close()
	client, err := network.Dial(server.Addr(), reg, time.Second)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	client.Send(Chat{From: "ann", Text: "hello"})

	// A game drains Poll once per frame; here we wait for the message.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		for _, ev := range server.Poll() {
			if m, ok := ev.Msg.(*Chat); ok {
				fmt.Println(m.From, "says", m.Text)
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	// Output:
	// ann says hello
}

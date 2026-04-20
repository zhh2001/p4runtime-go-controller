package client_test

import (
	"context"
	"fmt"
	"time"

	"github.com/zhh2001/p4runtime-go-controller/client"
)

func ExampleDial() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.Dial(ctx, "127.0.0.1:9559",
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
	)
	if err != nil {
		return
	}
	defer c.Close()

	if err := c.BecomePrimary(ctx); err != nil {
		return
	}
	fmt.Printf("primary=%v", c.IsPrimary())
}

func ExampleElectionID_Less() {
	a := client.ElectionID{Low: 1}
	b := client.ElectionID{Low: 2}
	fmt.Println(a.Less(b))
	// Output: true
}

// Ignore this file when building the project (it is a tiny WS helper compiled
// on demand by test_wallet_ws.sh).
//go:build ignore

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	u := flag.String("url", "", "wss URL")
	flag.Parse()
	if *u == "" {
		log.Fatal("usage: wsclient -url wss://...")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
	}
	h := http.Header{}
	h.Set("User-Agent", "lsm-ws-test/1")
	c, _, err := dialer.Dial(*u, h)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(20 * time.Second))
	c.SetPongHandler(func(string) error {
		c.SetReadDeadline(time.Now().Add(20 * time.Second))
		return nil
	})
	// Pings keep the connection alive and produce an ack per ping, which lets
	// our caller distinguish "connected and idle" from "failed to dial".
	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(2 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				if err := c.WriteControl(websocket.PingMessage,
					[]byte("ping"), time.Now().Add(2*time.Second)); err != nil {
					return
				}
			}
		}
	}()
	for {
		select {
		case <-sig:
			close(stop)
			return
		default:
		}
		mt, msg, err := c.ReadMessage()
		if err != nil {
			close(stop)
			return
		}
		if mt == websocket.TextMessage {
			fmt.Printf("WS %s\n", string(msg))
			c.SetReadDeadline(time.Now().Add(20 * time.Second))
		}
	}
}

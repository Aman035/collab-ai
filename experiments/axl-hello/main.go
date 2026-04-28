package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	mode := flag.String("mode", "", "listen | send")
	axl := flag.String("axl", "http://127.0.0.1:9002", "Axl daemon HTTP base URL")
	to := flag.String("to", "", "destination peer ID (send mode)")
	flag.Parse()

	switch *mode {
	case "listen":
		listen(*axl)
	case "send":
		if *to == "" || flag.NArg() == 0 {
			fatalf("send mode: -to <peer-id> <message>")
		}
		send(*axl, *to, flag.Arg(0))
	default:
		fmt.Fprintln(os.Stderr, "usage: axl-hello -mode listen|send [-axl URL] [-to PEER_ID] [message]")
		os.Exit(2)
	}
}

func listen(axl string) {
	key, err := myPeerID(axl)
	if err != nil {
		fatalf("topology: %v", err)
	}
	fmt.Println("my peer id:", key)
	fmt.Println("polling", axl+"/recv ...")

	for {
		resp, err := http.Get(axl + "/recv")
		if err != nil {
			fmt.Fprintf(os.Stderr, "recv: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) > 0 {
			fmt.Printf("from %s: %s\n", resp.Header.Get("X-From-Peer-Id"), body)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func send(axl, to, msg string) {
	req, err := http.NewRequest("POST", axl+"/send", bytes.NewBufferString(msg))
	if err != nil {
		fatalf("%v", err)
	}
	req.Header.Set("X-Destination-Peer-Id", to)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("send: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		fatalf("send: %s: %s", resp.Status, body)
	}
	fmt.Println("sent.")
}

func myPeerID(axl string) (string, error) {
	resp, err := http.Get(axl + "/topology")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var t struct {
		OurPublicKey string `json:"our_public_key"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return "", fmt.Errorf("parse topology: %w", err)
	}
	return t.OurPublicKey, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

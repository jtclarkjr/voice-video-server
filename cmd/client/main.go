package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/pion/webrtc/v4"
)

func main() {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("[connection] %s\n", s.String())
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			os.Exit(0)
		}
	})

	dc, err := pc.CreateDataChannel("echo", nil)
	if err != nil {
		log.Fatal(err)
	}

	ready := make(chan struct{})

	dc.OnOpen(func() {
		fmt.Println("[datachannel] open — type messages below, press Enter to send (ctrl+c to quit)")
		close(ready)
	})

	dc.OnClose(func() {
		fmt.Println("[datachannel] closed")
		os.Exit(0)
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		fmt.Printf("< %s\n", string(msg.Data))
	})

	// Create offer and gather ICE candidates
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		log.Fatal(err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)

	if err = pc.SetLocalDescription(offer); err != nil {
		log.Fatal(err)
	}

	<-gatherComplete

	// Send offer to server
	offerJSON, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("[signaling] sending offer to http://localhost:8080/offer ...")

	resp, err := http.Post("http://localhost:8080/offer", "application/json", bytes.NewReader(offerJSON))
	if err != nil {
		log.Fatal(err)
	}

	var answer webrtc.SessionDescription
	if err = json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		log.Fatal(err)
	}
	_ = resp.Body.Close()

	fmt.Println("[signaling] received answer, setting remote description...")

	if err = pc.SetRemoteDescription(answer); err != nil {
		log.Fatal(err)
	}

	// Wait for data channel to open, then read stdin in a loop
	<-ready

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		fmt.Printf("> %s\n", text)
		if err := dc.SendText(text); err != nil {
			log.Printf("send error: %v", err)
		}
	}
}

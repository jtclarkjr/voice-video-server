package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pion/webrtc/v4"
)

func HandleOffer(w http.ResponseWriter, r *http.Request) {
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	pc, err := newPeerConnection()
	if err != nil {
		http.Error(w, "could not create peer connection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			log.Printf("DataChannel '%s' open", dc.Label())
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			text := string(msg.Data)
			log.Printf("Received: %s", text)
			if err := dc.SendText("echo: " + text); err != nil {
				log.Printf("SendText error: %v", err)
			}
		})
	})

	if err = pc.SetRemoteDescription(offer); err != nil {
		http.Error(w, "set remote desc: "+err.Error(), http.StatusInternalServerError)
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		http.Error(w, "create answer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)

	if err = pc.SetLocalDescription(answer); err != nil {
		http.Error(w, "set local desc: "+err.Error(), http.StatusInternalServerError)
		return
	}

	<-gatherComplete

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pc.LocalDescription()); err != nil {
		log.Printf("encode response error: %v", err)
	}
}

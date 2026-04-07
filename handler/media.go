package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/pion/webrtc/v4"
)

// HandleMedia accepts an SDP offer containing audio/video tracks,
// mirrors them back to the sender, and optionally handles data channels.
func HandleMedia(w http.ResponseWriter, r *http.Request) {
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

	// When a remote track arrives, create a local track and loop packets back
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Track received: kind=%s codec=%s", remoteTrack.Kind(), remoteTrack.Codec().MimeType)

		localTrack, err := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			remoteTrack.ID(),
			remoteTrack.StreamID(),
		)
		if err != nil {
			log.Printf("failed to create local track: %v", err)
			return
		}

		sender, err := pc.AddTrack(localTrack)
		if err != nil {
			log.Printf("failed to add track: %v", err)
			return
		}

		// Read and discard RTCP packets (required to keep the sender alive)
		go func() {
			for {
				if _, _, err := sender.Read(make([]byte, 1500)); err != nil {
					return
				}
			}
		}()

		// Forward RTP packets from remote → local (echo media back)
		go func() {
			for {
				pkt, _, err := remoteTrack.ReadRTP()
				if err != nil {
					if err != io.EOF {
						log.Printf("track read error: %v", err)
					}
					return
				}
				if err := localTrack.WriteRTP(pkt); err != nil {
					if err != io.ErrClosedPipe {
						log.Printf("track write error: %v", err)
					}
					return
				}
			}
		}()
	})

	// Also support data channels if the client creates one
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

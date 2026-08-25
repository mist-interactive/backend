package realtime

import (
	"crypto/rsa"
	"dbBackend/handlers"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now, close down later
	},
}

// handles listening to incoming WS requests, upgrades connection to WS, and adds to Hub connection map
func (h *Hub) ServeWS(pubKey *rsa.PublicKey) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract & validate JWT from query string
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			http.Error(w, "Missing token query parameter", http.StatusUnauthorized)
			return
		}
		claims, err := handlers.ValidateToken(tokenStr, pubKey)
		if err != nil {
			http.Error(w, "Unauthorized: Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// upgrade HTTP connection to persistent WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Problem upgrading to websocket")
			return
		}

		client := NewClient(h, conn, claims.UserID, claims.Username)
		h.register <- client

		// start the read and write pumps in background goroutines
		go client.writePump()
		go client.readPump()
	}
}

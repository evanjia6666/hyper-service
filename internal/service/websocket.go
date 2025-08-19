package service

import (
	"encoding/json"
	"log"
	"net/http"
	_ "net/http/pprof"

	"github.com/gorilla/websocket"
	"github.com/uxuyprotocol/hyper-service/internal/model"
)

type EventDetail struct {
	Event string
	Data  model.Event
}

// startWebSocketServer starts the WebSocket server
func (s *Service) startWebSocketServer() {

	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/events", s.handleEventsQuery) // New endpoint for querying events

	log.Printf("WebSocket server starting on port %s", s.config.Port)
	err := http.ListenAndServe(":"+s.config.Port, nil)
	if err != nil {
		log.Printf("WebSocket server error: %v", err)
		panic(err)
	}
	log.Println("WebSocket server stopped")
}

// handleWebSocket handles WebSocket connections
func (s *Service) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	// Add client to the list with empty subscriptions
	s.clientsMux.Lock()
	s.clients[conn] = make(map[string]bool)
	s.clientsMux.Unlock()

	// Remove client when done
	defer func() {
		s.clientsMux.Lock()
		delete(s.clients, conn)
		s.clientsMux.Unlock()
	}()

	// Handle incoming messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			return
		}

		// Parse subscription message
		var subMsg SubscriptionMessage
		err = json.Unmarshal(message, &subMsg)
		if err != nil {
			log.Printf("Failed to parse subscription message: %v", err)
			continue
		}

		// Validate message
		if !allowEvent(subMsg.Event) {
			log.Printf("Invalid event type in subscription message: %s", subMsg.Event)
			continue
		}

		// Handle subscription/unsubscription for all users in whitelist
		s.clientsMux.Lock()
		switch subMsg.Action {
		case "subscribe":
			// Subscribe to all users in whitelist
			s.clients[conn][subMsg.Event] = true
			log.Printf("Client subscribed to %s", subMsg.Event)
		case "unsubscribe":
			// Unsubscribe from all users
			delete(s.clients[conn], subMsg.Event)
			log.Printf("Client unsubscribed %s", subMsg.Event)
		default:
			log.Printf("Invalid action in subscription message: %s", subMsg.Action)
		}
		s.clientsMux.Unlock()
	}
}

// handleEventsQuery handles event querying
func (s *Service) handleEventsQuery(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var query EventQuery
	err := json.NewDecoder(r.Body).Decode(&query)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate query parameters
	if query.UserAddress == "" {
		http.Error(w, "userAddress is required", http.StatusBadRequest)
		return
	}

	if query.Event == "" {
		http.Error(w, "event is required", http.StatusBadRequest)
		return
	}

	if query.StartBlock > query.EndBlock {
		http.Error(w, "startBlock must be less than or equal to endBlock", http.StatusBadRequest)
		return
	}

	evs, err := model.QueryEventsByBlock(s.db, query.UserAddress, query.Event, query.StartBlock, query.EndBlock)
	if err != nil {
		http.Error(w, "Failed to query events", http.StatusInternalServerError)
		return
	}
	// Return results
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(evs)
}

// broadcastEventToSubscribers sends an event to clients subscribed to the user
// NOTE: User has already been checked against the Bloom filter before calling this function
func (s *Service) broadcastEventToSubscribers(event model.Event, eventType string) {
	// s.clientsMux.RLock()
	// defer s.clientsMux.RUnlock()

	// for conn, subscriptions := range s.clients {
	// 	// Check if this client is subscribed to this event type
	// 	if subscribed, exists := subscriptions[eventType]; exists && subscribed {
	// 		err := conn.WriteJSON(event)
	// 		if err != nil {
	// 			log.Printf("Failed to send event to client: %v", err)
	// 			// Remove the client if there's an error
	// 			conn.Close()
	// 			// We can't modify s.clients here because we're holding a read lock
	// 			// The cleanup will happen in the WebSocket handler
	// 		}
	// 	}
	// }
	s.websocketMsgChan <- &EventDetail{
		Data:  event,
		Event: eventType,
	}
}

func (s *Service) WriteWebsocketMsg() {
	defer s.wg.Done()

	for msg := range s.websocketMsgChan {
		if msg == nil {
			continue
		}

		s.clientsMux.RLock()
		for conn, subscriptions := range s.clients {
			// Check if this client is subscribed to this event type
			if subscribed, exists := subscriptions[msg.Event]; exists && subscribed {
				err := conn.WriteJSON(msg.Data)
				if err != nil {
					log.Printf("Failed to send event to client: %v", err)
					// Remove the client if there's an error
					conn.Close()
					// We can't modify s.clients here because we're holding a read lock
					// The cleanup will happen in the WebSocket handler
				}
			}
		}
		s.clientsMux.RUnlock()
	}

	// Close all WebSocket connections
	s.clientsMux.Lock()
	for conn := range s.clients {
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server shutting down"))
		conn.Close()
	}
	s.clientsMux.Unlock()

	log.Println("Stopping write websocket server...")
}

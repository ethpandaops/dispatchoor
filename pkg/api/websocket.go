package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/auth"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

// createUpgrader creates a WebSocket upgrader with origin validation.
func createUpgrader(allowedOrigins []string) websocket.Upgrader {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"

	originSet := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		originSet[origin] = true
	}

	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// If no origins configured, reject all cross-origin requests.
			if len(allowedOrigins) == 0 {
				return r.Header.Get("Origin") == ""
			}

			// Allow all origins if configured with "*".
			if allowAll {
				return true
			}

			// Check if origin is in allowed list.
			origin := r.Header.Get("Origin")
			return originSet[origin]
		},
	}
}

// MessageType represents the type of WebSocket message.
type MessageType string

const (
	// Server -> Client messages.
	MessageTypeRunnerStatus MessageType = "runner_status"
	MessageTypeQueueUpdate  MessageType = "queue_update"
	MessageTypeJobState     MessageType = "job_state"
	MessageTypeDispatch     MessageType = "dispatch"
	MessageTypeSystemStatus MessageType = "system_status"
	MessageTypeError        MessageType = "error"
	MessageTypeSubscribed   MessageType = "subscribed"
	MessageTypeUnsubscribed MessageType = "unsubscribed"

	// Client -> Server messages.
	MessageTypeSubscribe   MessageType = "subscribe"
	MessageTypeUnsubscribe MessageType = "unsubscribe"
	MessageTypePing        MessageType = "ping"
)

// Message represents a WebSocket message.
type Message struct {
	Type    MessageType `json:"type"`
	GroupID string      `json:"group_id,omitempty"`
	Payload any         `json:"payload,omitempty"`
}

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	log logrus.FieldLogger

	// Registered clients.
	clients map[*Client]bool

	// Clients subscribed to specific groups.
	subscriptions map[string]map[*Client]bool

	// Register requests from clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// Broadcast messages to all clients.
	broadcast chan *Message

	// Broadcast messages to specific groups.
	groupBroadcast chan *groupMessage

	mu sync.RWMutex
}

type groupMessage struct {
	groupID string
	msg     *Message
}

// NewHub creates a new WebSocket hub.
func NewHub(log logrus.FieldLogger) *Hub {
	return &Hub{
		log:            log.WithField("component", "websocket"),
		clients:        make(map[*Client]bool),
		subscriptions:  make(map[string]map[*Client]bool),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		broadcast:      make(chan *Message, 256),
		groupBroadcast: make(chan *groupMessage, 256),
	}
}

// Run starts the hub's main loop.
func (h *Hub) Run(ctx context.Context) {
	h.log.Info("Starting WebSocket hub")

	for {
		select {
		case <-ctx.Done():
			h.log.Info("Stopping WebSocket hub")

			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

			h.log.WithField("client", client.id).Debug("Client registered")

		case client := <-h.unregister:
			h.mu.Lock()

			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// Remove from all subscriptions.
				for groupID, clients := range h.subscriptions {
					delete(clients, client)

					if len(clients) == 0 {
						delete(h.subscriptions, groupID)
					}
				}
			}

			h.mu.Unlock()

			h.log.WithField("client", client.id).Debug("Client unregistered")

		case msg := <-h.broadcast:
			h.mu.RLock()

			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}

			h.mu.RUnlock()

		case gm := <-h.groupBroadcast:
			h.mu.RLock()

			if clients, ok := h.subscriptions[gm.groupID]; ok {
				for client := range clients {
					select {
					case client.send <- gm.msg:
					default:
						close(client.send)
						delete(clients, client)
					}
				}
			}

			h.mu.RUnlock()
		}
	}
}

// Subscribe adds a client to a group's subscription list.
func (h *Hub) Subscribe(client *Client, groupID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.subscriptions[groupID]; !ok {
		h.subscriptions[groupID] = make(map[*Client]bool)
	}

	h.subscriptions[groupID][client] = true

	h.log.WithFields(logrus.Fields{
		"client":   client.id,
		"group_id": groupID,
	}).Debug("Client subscribed to group")
}

// Unsubscribe removes a client from a group's subscription list.
func (h *Hub) Unsubscribe(client *Client, groupID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.subscriptions[groupID]; ok {
		delete(clients, client)

		if len(clients) == 0 {
			delete(h.subscriptions, groupID)
		}
	}

	h.log.WithFields(logrus.Fields{
		"client":   client.id,
		"group_id": groupID,
	}).Debug("Client unsubscribed from group")
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg *Message) {
	select {
	case h.broadcast <- msg:
	default:
		h.log.Warn("Broadcast channel full, dropping message")
	}
}

// BroadcastToGroup sends a message to all clients subscribed to a group.
func (h *Hub) BroadcastToGroup(groupID string, msg *Message) {
	msg.GroupID = groupID

	select {
	case h.groupBroadcast <- &groupMessage{groupID: groupID, msg: msg}:
	default:
		h.log.Warn("Group broadcast channel full, dropping message")
	}
}

// BroadcastRunnerStatus broadcasts a runner status update.
func (h *Hub) BroadcastRunnerStatus(runner *store.Runner, groupID string) {
	h.BroadcastToGroup(groupID, &Message{
		Type:    MessageTypeRunnerStatus,
		Payload: runner,
	})
}

// BroadcastQueueUpdate broadcasts a queue update.
func (h *Hub) BroadcastQueueUpdate(groupID string, jobs []*store.Job) {
	h.BroadcastToGroup(groupID, &Message{
		Type:    MessageTypeQueueUpdate,
		Payload: jobs,
	})
}

// BroadcastJobState broadcasts a job state change.
func (h *Hub) BroadcastJobState(job *store.Job) {
	h.BroadcastToGroup(job.GroupID, &Message{
		Type:    MessageTypeJobState,
		Payload: job,
	})
}

// BroadcastDispatch broadcasts a dispatch event.
func (h *Hub) BroadcastDispatch(job *store.Job) {
	h.BroadcastToGroup(job.GroupID, &Message{
		Type:    MessageTypeDispatch,
		Payload: job,
	})
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}

// Client represents a WebSocket client connection.
type Client struct {
	id   string
	hub  *Hub
	conn *websocket.Conn
	user *store.User
	send chan *Message
}

// NewClient creates a new WebSocket client.
func NewClient(hub *Hub, conn *websocket.Conn, user *store.User, id string) *Client {
	return &Client{
		id:   id,
		hub:  hub,
		conn: conn,
		user: user,
		send: make(chan *Message, 256),
	}
}

// ReadPump pumps messages from the websocket connection to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)

	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}

	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.log.WithError(err).Warn("WebSocket read error")
			}

			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			c.hub.log.WithError(err).Warn("Failed to parse WebSocket message")

			continue
		}

		c.handleMessage(&msg)
	}
}

// WritePump pumps messages from the hub to the websocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}

			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})

				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				c.hub.log.WithError(err).Warn("Failed to marshal WebSocket message")

				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}

			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming messages from the client.
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case MessageTypeSubscribe:
		if msg.GroupID != "" {
			c.hub.Subscribe(c, msg.GroupID)
			c.send <- &Message{
				Type:    MessageTypeSubscribed,
				GroupID: msg.GroupID,
			}
		}

	case MessageTypeUnsubscribe:
		if msg.GroupID != "" {
			c.hub.Unsubscribe(c, msg.GroupID)
			c.send <- &Message{
				Type:    MessageTypeUnsubscribed,
				GroupID: msg.GroupID,
			}
		}

	case MessageTypePing:
		c.send <- &Message{Type: MessageTypeSystemStatus, Payload: map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}}

	default:
		c.hub.log.WithField("type", msg.Type).Warn("Unknown message type")
	}
}

// ServeWs handles WebSocket requests from the peer.
func ServeWs(hub *Hub, authSvc auth.Service, allowedOrigins []string, w http.ResponseWriter, r *http.Request) {
	// Authenticate the user.
	token := r.URL.Query().Get("token")
	if token == "" {
		// Try to get from cookie.
		if cookie, err := r.Cookie("session"); err == nil {
			token = cookie.Value
		}
	}

	var user *store.User

	if token != "" {
		var err error

		user, err = authSvc.ValidateSession(r.Context(), token)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			return
		}
	} else {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)

		return
	}

	// Create upgrader with origin validation.
	upgrader := createUpgrader(allowedOrigins)

	// Upgrade to WebSocket.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		hub.log.WithError(err).Error("Failed to upgrade WebSocket")

		return
	}

	// Generate client ID.
	clientID := r.Header.Get("X-Request-ID")
	if clientID == "" {
		clientID = user.ID
	}

	client := NewClient(hub, conn, user, clientID)
	hub.register <- client

	// Start pumps.
	go client.WritePump()
	go client.ReadPump()
}

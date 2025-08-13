package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

const (
	watchFills = "node_fills_by_block"
	watchTrade = "node_trades_by_block"
)

const (
	EventTrade = "trade"
	EventFill  = "fill"
)

// Event represents a single trade event
type Event struct {
	User  string                 `json:"user"`
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data"`
	// Coin          string  `json:"coin"`
	// Price         string  `json:"px"`
	// Size          string  `json:"sz"`
	// Side          string  `json:"side"`
	// Time          int64   `json:"time"`
	// StartPosition string  `json:"startPosition"`
	// Direction     string  `json:"dir"`
	// ClosedPnl     string  `json:"closedPnl"`
	// Hash          string  `json:"hash"`
	// OrderID       int64   `json:"oid"`
	// Crossed       bool    `json:"crossed"`
	// Fee           string  `json:"fee"`
	// TradeID       int64   `json:"tid"`
	// ClientOrderID string  `json:"cloid"`
	// FeeToken      string  `json:"feeToken"`
	// TwapID        *string `json:"twapId"`
}

// Data represents the structure of the JSON data file
type Data struct {
	LocalTime string          `json:"local_time"`
	BlockTime string          `json:"block_time"`
	BlockNum  int64           `json:"block_number"`
	Events    [][]interface{} `json:"events"`
}

// Service represents our main service
type Service struct {
	dataDir       string
	whitelistFile string
	port          string
	watcher       *fsnotify.Watcher
	whitelist     map[string]bool
	whitelistMux  sync.RWMutex
	upgrader      websocket.Upgrader
	clients       map[*websocket.Conn]map[string]bool // conn -> event type -> subscribed
	clientsMux    sync.RWMutex
	quit          chan struct{}
	//fileTails     map[string]*tailFile // filename -> tailFile
	currentFiles map[string]struct{}
	//fileTailsMux  sync.RWMutex
}

// tailFile represents a tailed file with its position
// type tailFile struct {
// 	filename string
// 	position int64
// }

// NewService creates a new service instance
func NewService(dataDir, whitelistFile, port string) (*Service, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	svc := &Service{
		dataDir:       dataDir,
		whitelistFile: whitelistFile,
		port:          port,
		watcher:       watcher,
		whitelist:     make(map[string]bool),
		clients:       make(map[*websocket.Conn]map[string]bool),
		//fileTails:     make(map[string]*tailFile),
		quit:         make(chan struct{}),
		currentFiles: make(map[string]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin for testing
			},
		},
	}

	for _, t := range []string{watchFills} {
		gPath, pPath, fPath := svc.currentDirFile(t)
		// 获取目录地址

		// 如果目录不存在，创建目录
		if _, err := os.Stat(pPath); os.IsNotExist(err) {
			err = os.MkdirAll(pPath, 0755)
			if err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", pPath, err)
			}
		}
		// watch 当前目录
		err := svc.watcher.Add(pPath)
		if err != nil {
			return nil, fmt.Errorf("failed to watch directory %s: %w", pPath, err)
		}

		// wath 父目录
		err = svc.watcher.Add(gPath)
		if err != nil {
			return nil, fmt.Errorf("failed to watch directory %s: %w", gPath, err)
		}
		svc.currentFiles[fPath] = struct{}{}
	}

	// Load initial whitelist
	err = svc.loadWhitelist()
	if err != nil {
		return nil, fmt.Errorf("failed to load whitelist: %w", err)
	}

	return svc, nil
}

// Start starts the service
func (s *Service) Start() error {

	// Start file watcher
	go s.watchFiles()

	// Start WebSocket server
	go s.startWebSocketServer()

	// Start whitelist file watcher
	go s.watchWhitelist()

	// Watch the current hour file
	// 转为 GMT 时间
	now := time.Now().UTC()
	hourFile := filepath.Join(s.dataDir, "node_fills_by_block", "hourly",
		now.Format("20060102"), fmt.Sprintf("%d", now.Hour()))

	log.Printf("Watching file: %s", hourFile)

	// Watch the whitelist file
	err := s.watcher.Add(s.whitelistFile)
	if err != nil {
		log.Printf("Warning: failed to watch whitelist file: %v", err)
	}

	return nil
}

// Stop stops the service
func (s *Service) Stop() {
	close(s.quit)
	s.watcher.Close()

	// Close all WebSocket connections
	s.clientsMux.Lock()
	for conn := range s.clients {
		conn.Close()
	}
	s.clientsMux.Unlock()
}

// loadWhitelist loads the whitelist from file
func (s *Service) loadWhitelist() error {
	content, err := os.ReadFile(s.whitelistFile)
	if err != nil {
		return err
	}

	s.whitelistMux.Lock()
	defer s.whitelistMux.Unlock()

	s.whitelist = make(map[string]bool)
	for _, line := range strings.Split(string(content), "\n") {
		user := strings.TrimSpace(line)
		if user != "" {
			s.whitelist[user] = true
		}
	}

	log.Printf("Loaded %d users from whitelist", len(s.whitelist))
	return nil
}

// watchWhitelist watches for changes to the whitelist file
func (s *Service) watchWhitelist() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.quit:
			return
		case <-ticker.C:
			err := s.loadWhitelist()
			if err != nil {
				log.Printf("Failed to reload whitelist: %v", err)
			}
		}
	}
}

// watchFiles watches for file changes
func (s *Service) watchFiles() {
	err := s.watcher.Add(s.dataDir) // Watch the data directory
	if err != nil {
		log.Printf("Error adding directory to watcher: %v, data dir: %s", err, s.dataDir)
		return
	}
	// 初次启动监听需要监听的文件
	for file := range s.currentFiles {
		go s.processFillsFile(file, true)
	}

	for {
		select {
		case <-s.quit:
			return
		case event := <-s.watcher.Events:
			if event.Op&fsnotify.Create != 0 {
				log.Printf("File created: %s", event.Name)
				// 如果是目录，则添加到 watcher
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					err = s.watcher.Add(event.Name)
					if err != nil {
						log.Printf("Error adding directory to watcher: %v, dir: %s", err, event.Name)
						continue
					}
					log.Printf("Watching directory: %s", event.Name)
					continue
				}

				// 监听到有新的文件创建，检查是否是关注的几个类型， 如果是，启动对新的文件的监听
				switch {
				case strings.Contains(event.Name, watchFills):
					s.currentFiles[event.Name] = struct{}{}
					go s.processFillsFile(event.Name, false)
				case strings.Contains(event.Name, watchTrade):
					go s.processFillsFile(event.Name, false)
				}

			}
		case err := <-s.watcher.Errors:
			fmt.Println("fsnotify 错误:", err)
		}
	}
}

// updateWatchedFile updates the watched file based on current time
// func (s *Service) updateWatchedFile() {
// 	now := time.Now()
// 	hourFile := filepath.Join(s.dataDir, "node_fills_by_block", "hourly",
// 		now.Format("20060102"), fmt.Sprintf("%d", now.Hour()))

// 	// Create file if it doesn't exist
// 	if _, err := os.Stat(hourFile); err == nil {
// 		// _, err = os.Create(hourFile)
// 		// if err != nil {
// 		// 	log.Printf("Failed to create file %s: %v", hourFile, err)
// 		// 	return
// 		// }
// 		s.fileTailsMux.Lock()
// 		s.fileTails[hourFile] = &tailFile{
// 			filename: hourFile,
// 			position: 0,
// 		}
// 		s.fileTailsMux.Unlock()
// 		log.Printf("Now watching file: %s", hourFile)
// 	}
// }

// processFillsFile processes a data file
func (s *Service) processFillsFile(filename string, isFirst bool) {
	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Failed to open file %s: %v", filename, err)
		return
	}
	defer file.Close()

	// 从文件末尾开始（如果要从头读可以 Seek(0,0)）
	if stat, err := file.Stat(); err == nil {
		if isFirst {
			// 首次启动从末尾开始读
			file.Seek(stat.Size(), io.SeekStart)
		} else {
			// 如果是新文件，则从文件开始读起
			file.Seek(0, io.SeekStart)
		}
	}

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// 如果不是今天的文件，EOF 表示可以结束
				if !isCurrentHourFile(filename) {
					return
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}
			log.Printf("Failed to read line from file %s: %v", filename, err)
			return
		}

		if line == "" {
			continue
		}

		// Try to parse as JSON
		var data Data
		err = json.Unmarshal([]byte(line), &data)
		if err != nil {
			log.Printf("Failed to parse JSON line in file %s: %v", filename, err)
			continue
		}

		// Process events
		for _, event := range data.Events {
			if len(event) < 2 {
				continue
			}

			user, ok := event[0].(string)
			if !ok {
				continue
			}

			eventData, ok := event[1].(map[string]interface{})
			if !ok {
				continue
			}

			// Check if user is in whitelist
			s.whitelistMux.RLock()
			_, whitelisted := s.whitelist[user]
			s.whitelistMux.RUnlock()

			if whitelisted {
				// Convert to our Event struct
				ev := Event{
					User:  user,
					Event: EventFill,
					Data:  eventData,
					// Coin:          getString(eventData, "coin"),
					// Price:         getString(eventData, "px"),
					// Size:          getString(eventData, "sz"),
					// Side:          getString(eventData, "side"),
					// Time:          getInt64(eventData, "time"),
					// StartPosition: getString(eventData, "startPosition"),
					// Direction:     getString(eventData, "dir"),
					// ClosedPnl:     getString(eventData, "closedPnl"),
					// Hash:          getString(eventData, "hash"),
					// OrderID:       getInt64(eventData, "oid"),
					// Crossed:       getBool(eventData, "crossed"),
					// Fee:           getString(eventData, "fee"),
					// TradeID:       getInt64(eventData, "tid"),
					// ClientOrderID: getString(eventData, "cloid"),
					// FeeToken:      getString(eventData, "feeToken"),
					// TwapID:        getStringPtr(eventData, "twapId"),
				}

				// Send to WebSocket clients who are subscribed to this user
				s.broadcastEventToSubscribers(ev, EventFill)
			}
		}
	}

	// // Update the position
	// pos, err := file.Seek(0, 1) // Get current position
	// if err != nil {
	// 	log.Printf("Failed to get file position for %s: %v", filename, err)
	// } else {
	// 	s.fileTailsMux.Lock()
	// 	tailInfo.position = pos
	// 	s.fileTailsMux.Unlock()
	// }

	// if err := scanner.Err(); err != nil {
	// 	log.Printf("Error reading file %s: %v", filename, err)
	// }
}

// Helper functions for extracting data from map[string]interface{}
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getInt64(m map[string]interface{}, key string) int64 {
	if val, ok := m[key]; ok {
		if f, ok := val.(float64); ok {
			return int64(f)
		}
		if i, ok := val.(int64); ok {
			return i
		}
	}
	return 0
}

func getBool(m map[string]interface{}, key string) bool {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

func getStringPtr(m map[string]interface{}, key string) *string {
	if val, ok := m[key]; ok {
		if val == nil {
			return nil
		}
		if str, ok := val.(string); ok {
			return &str
		}
	}
	return nil
}

// broadcastEventToSubscribers sends an event to clients subscribed to the user
func (s *Service) broadcastEventToSubscribers(event Event, eventType string) {
	s.clientsMux.RLock()
	defer s.clientsMux.RUnlock()

	for conn, subscriptions := range s.clients {
		// Check if this client is subscribed to this user
		if subscribed, exists := subscriptions[eventType]; exists && subscribed {
			err := conn.WriteJSON(event)
			if err != nil {
				log.Printf("Failed to send event to client: %v", err)
				// Remove the client if there's an error
				conn.Close()
				// We can't modify s.clients here because we're holding a read lock
				// The cleanup will happen in the WebSocket handler
			}
		}
	}
}

// startWebSocketServer starts the WebSocket server
func (s *Service) startWebSocketServer() {
	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/whitelist", s.handleWhitelist)

	log.Printf("WebSocket server starting on port %s", s.port)
	err := http.ListenAndServe(":"+s.port, nil)
	if err != nil {
		log.Printf("WebSocket server error: %v", err)
	}
}

// SubscriptionMessage represents a subscription/unsubscription message
type SubscriptionMessage struct {
	Action string `json:"action"` // "subscribe" or "unsubscribe"
	Event  string `json:"event"`  // event type (e.g., trade, fills)
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
			break
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

// handleWhitelist handles whitelist management
func (s *Service) handleWhitelist(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.whitelistMux.RLock()
		users := make([]string, 0, len(s.whitelist))
		for user := range s.whitelist {
			users = append(users, user)
		}
		s.whitelistMux.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)

	case http.MethodPost:
		// For simplicity, we'll just reload the whitelist file
		err := s.loadWhitelist()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) currentDirFile(msg string) (string, string, string) {
	now := time.Now().UTC()
	// return filepath.Join(s.dataDir, msg, "hourly",
	// 	now.Format("20060102"), fmt.Sprintf("%d", now.Hour()))
	return filepath.Join(s.dataDir, msg, "hourly"), filepath.Join(s.dataDir, msg, "hourly", now.Format("20060102")), filepath.Join(s.dataDir, msg, "hourly", now.Format("20060102"), fmt.Sprintf("%d", now.Hour()))
}

func isCurrentHourFile(path string) bool {
	// 判断给定的路径是否是当前小时的文件
	base := filepath.Base(path)
	now := time.Now().UTC()
	return strconv.Itoa(now.Hour()) == base
}

func allowEvent(msg string) bool {
	switch msg {
	case EventFill, EventTrade:
		return true
	default:
		return false
	}
}

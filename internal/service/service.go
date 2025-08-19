package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	config "github.com/uxuyprotocol/hyper-service/configs"
	"github.com/uxuyprotocol/hyper-service/internal/model"
	wredis "github.com/uxuyprotocol/hyper-service/redis"
)

const (
	WATCH_FILLS = "node_fills_by_block"
	WATCH_MISC  = "misc_events_by_block"
)

const (
	EVENT_FILL = "fill"
	EVENT_MISC = "misc"
)

// FillData represents the structure of the JSON data file
type FillData struct {
	LocalTime string          `json:"local_time"`
	BlockTime string          `json:"block_time"`
	BlockNum  int64           `json:"block_number"`
	Events    [][]interface{} `json:"events"`
}

type MiscData struct {
	LocalTime string          `json:"local_time"`
	BlockTime string          `json:"block_time"`
	BlockNum  uint64          `json:"block_number"`
	Events    []MiscEventData `json:"events"`
}

// MiscEventData represents the structure of misc event data
type MiscEventData struct {
	Time  string                 `json:"time"`
	Hash  string                 `json:"hash"`
	Inner map[string]interface{} `json:"inner"`
}

type LedgerUpdate struct {
	Users []string `json:"users"`
}

// EventQuery represents the parameters for querying events
type EventQuery struct {
	// UserAddress string `json:"userAddress"`
	// Event       string `json:"event"`
	StartBlock uint64 `json:"startBlock"`
	EndBlock   uint64 `json:"endBlock"`
	Page       int    `json:"page"`
	Size       int    `json:"pageSize"`
}

// Service represents our main service
type Service struct {
	config  *config.Config
	watcher *fsnotify.Watcher

	upgrader         websocket.Upgrader
	clients          map[*websocket.Conn]map[string]bool // conn -> event type -> subscribed
	clientsMux       sync.RWMutex
	websocketMsgChan chan *EventDetail

	quit chan struct{}
	//	currentFiles map[string]struct{}
	db *sql.DB

	redisClient *redis.Client

	bloomFilter    *bloom.BloomFilter
	bloomFilterMux sync.RWMutex
	lastScore      string // last score of zset for all addresss

	// filePositions map[string]int64 // filename -> position
	// filePosMux    sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	currentFillFile string
	fillPosition    uint64
	currentMiscFile string
	miscPosition    uint64

	wg sync.WaitGroup
}

// NewService creates a new service instance
func NewService(cfg *config.Config) (*Service, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	var db *sql.DB

	aStr := strings.Split(cfg.PostgresAddr, ":")
	if len(aStr) != 2 {
		return nil, fmt.Errorf("invalid PostgreSQL address: %s", cfg.PostgresAddr)
	}
	// Connect to PostgreSQL
	db, err = model.NewDB(aStr[0], aStr[1], cfg.PostgresUser, cfg.PostgresPassword, cfg.PostgresDB)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	if err := model.InitSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// Create a Bloom filter with configured parameters
	bf := bloom.NewWithEstimates(cfg.BloomExpectedItems, cfg.BloomFalsePositiveRate)

	ctx, cancel := context.WithCancel(context.Background())

	svc := &Service{
		config:           cfg,
		watcher:          watcher,
		bloomFilter:      bf,
		clients:          make(map[*websocket.Conn]map[string]bool),
		websocketMsgChan: make(chan *EventDetail, 50),
		quit:             make(chan struct{}),
		//	currentFiles: make(map[string]struct{}),
		db:          db,
		redisClient: redisClient,
		// filePositions: make(map[string]int64),
		ctx:    ctx,
		cancel: cancel,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin for testing
			},
		},
	}

	// Initialize bloom filter from Redis

	err = svc.initBloomFilterFromRedis()
	if err != nil {
		log.Printf("Warning: failed to initialize bloom filter from Redis: %v", err)
		// Fall back to loading from whitelist file
		return nil, err
	}

	// Setup directory watching
	for _, t := range []string{WATCH_FILLS, WATCH_MISC} {
		gPath, pPath, fPath := svc.currentDirFile(t)

		// Create directories if they don't exist
		if _, err := os.Stat(pPath); os.IsNotExist(err) {
			err = os.MkdirAll(pPath, 0755)
			if err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", pPath, err)
			}
		}

		// Watch current directory
		err := svc.watcher.Add(pPath)
		if err != nil {
			return nil, fmt.Errorf("failed to watch directory %s: %w", pPath, err)
		}

		// Watch parent directory
		err = svc.watcher.Add(gPath)
		if err != nil {
			return nil, fmt.Errorf("failed to watch directory %s: %w", gPath, err)
		}

		if t == WATCH_FILLS {
			svc.currentFillFile = fPath
			svc.fillPosition = 0
		} else {
			svc.currentMiscFile = fPath
			svc.miscPosition = 0
		}
	}

	svc.CatchUpEvents()

	return svc, nil
}

// Start starts the service
func (s *Service) Start() error {

	// Start file watcher
	s.wg.Add(1)
	go s.watchFiles()

	// Start redis new addresses watcher
	s.wg.Add(1)
	go s.watchNewAddress()

	// Start database cleanup task
	s.wg.Add(1)
	go s.cleanupOldEvents()

	// Start websocket message writer
	s.wg.Add(1)
	go s.WriteWebsocketMsg()

	// Start WebSocket server
	go s.startWebSocketServer()

	// Load file positions from Redis (for restart recovery)
	//go s.loadFilePositions()

	for _, eventType := range []string{WATCH_FILLS, WATCH_MISC} {
		if eventType == WATCH_FILLS {
			go s.processFillsFile()
		}

		if eventType == WATCH_MISC {
			go s.processMiscFile()
		}
	}

	return nil
}

// Stop stops the service
func (s *Service) Stop() {
	// Close the file watcher
	s.watcher.Close()

	// close
	close(s.quit)

	time.Sleep(100 * time.Millisecond)
	// close websocket message channel and conn
	close(s.websocketMsgChan)

	s.wg.Wait()

	// Save file positions to Redis for restart recovery
	s.saveFilePositions()

	s.cancel()

	// Close database connection
	s.db.Close()

	// Close Redis connection
	s.redisClient.Close()
}

// initBloomFilterFromRedis initializes the Bloom filter from Redis
func (s *Service) initBloomFilterFromRedis() error {
	addrs, lastScore, err := wredis.QueryAddressFromZset(s.ctx, s.redisClient, "-inf", "+inf")
	if err != nil {
		return err
	}
	s.lastScore = lastScore

	for _, addr := range addrs {
		s.bloomFilter.AddString(addr)
	}
	return nil
}

// loadNewAddress loads the new address from redis into the Bloom filter
func (s *Service) loadNewAddress() error {
	end := time.Now().Unix()
	newAddrs, lastScore, err := wredis.QueryAddressFromZset(s.ctx, s.redisClient, s.lastScore, strconv.FormatInt(end, 10))
	if err != nil {
		return err
	}

	if len(newAddrs) > 0 {
		s.bloomFilterMux.Lock()
		for i := range newAddrs {
			s.bloomFilter.AddString(newAddrs[i])
		}
		s.lastScore = lastScore
		s.bloomFilterMux.Unlock()
	}
	log.Printf("Loaded %d users from Redis into Bloom filter", len(newAddrs))
	return nil
}

// watchNewAddress watches new addresses in redis zset
func (s *Service) watchNewAddress() {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.quit:
			log.Println("Stopping watchNewAddress")
			return
		case <-ticker.C:
			err := s.loadNewAddress()
			if err != nil {
				log.Printf("Failed to reload new address: %v", err)
			}
		}
	}
}

// watchFiles watches for file changes
func (s *Service) watchFiles() {
	defer s.wg.Done()

	err := s.watcher.Add(s.config.DataDir) // Watch the data directory
	if err != nil {
		log.Printf("Error adding directory to watcher: %v, data dir: %s", err, s.config.DataDir)
		return
	}

	// Initially process the current files
	// for file := range s.currentFiles {
	// 	go s.processFile(file, true)
	// }

	for {
		select {
		case <-s.quit:
			log.Println("Stopping file watcher")
			return
		case event := <-s.watcher.Events:
			if event.Op&fsnotify.Create != 0 {
				log.Printf("File created: %s", event.Name)
				// If it's a directory, add it to watcher
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					err = s.watcher.Add(event.Name)
					if err != nil {
						log.Printf("Error adding directory to watcher: %v, dir: %s", err, event.Name)
						continue
					}
					log.Printf("Watching directory: %s", event.Name)
					continue
				}

				// Check if it's a file we're interested in
				switch {
				case strings.Contains(event.Name, WATCH_FILLS):
					if isCurrentHourFile(event.Name) {
						s.currentFillFile = event.Name
						go s.processFillsFile()
					}
				case strings.Contains(event.Name, WATCH_MISC):
					if isCurrentHourFile(event.Name) {
						s.currentMiscFile = event.Name
						go s.processMiscFile()
					}
				}
			}
		case err := <-s.watcher.Errors:
			log.Printf("fsnotify error: %v", err)
		}
	}
}

// SubscriptionMessage represents a subscription/unsubscription message
type SubscriptionMessage struct {
	Action string `json:"action"` // "subscribe" or "unsubscribe"
	Event  string `json:"event"`  // event type (e.g., fill, misc)
}

// currentDirFile returns the current directory paths for a given event type
func (s *Service) currentDirFile(eventType string) (string, string, string) {
	now := time.Now().UTC()
	return filepath.Join(s.config.DataDir, eventType, "hourly"),
		filepath.Join(s.config.DataDir, eventType, "hourly", now.Format("20060102")),
		filepath.Join(s.config.DataDir, eventType, "hourly", now.Format("20060102"), fmt.Sprintf("%d", now.Hour()))
}

// isCurrentHourFile checks if a given path is for the current hour
func isCurrentHourFile(path string) bool {
	// Determine if the given path is for the current hour
	base := filepath.Base(path)
	now := time.Now().UTC()
	return strconv.Itoa(now.Hour()) == base
}

// allowEvent checks if an event type is valid
func allowEvent(event string) bool {
	switch event {
	case EVENT_FILL, EVENT_MISC:
		return true
	default:
		return false
	}
}

// cleanupOldEvents periodically removes old events from the database
func (s *Service) cleanupOldEvents() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.quit:
			log.Println("Stopping cleanup old events")
			return
		case <-ticker.C:
			// Delete events older than the retention period
			cutoffTime := time.Now().Add(-s.config.EventRetention)
			model.CleanUp(s.db, cutoffTime)
		}
	}
}

// updateFilePosition saves the current position in a file to Redis
func (s *Service) updateFilePosition(filename string, position int64) {
	switch filename {
	case s.currentFillFile:
		s.fillPosition += uint64(position)
	case s.currentMiscFile:
		s.miscPosition += uint64(position)
	}
}

// saveFilePositions saves all file positions to Redis
func (s *Service) saveFilePositions() {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		// write fills event position
		fill := &FilePosition{
			FilePath: s.currentFillFile,
			Position: s.fillPosition,
		}
		fbyte, err := fill.Marshal()
		if err != nil {
			log.Printf("Failed to marshal fills event position: %v", err)
			return
		}

		if err := wredis.WriteString(s.ctx, s.redisClient, WATCH_FILLS, string(fbyte)); err != nil {
			log.Printf("Failed to save file position to Redis: %v", err)
			return
		}
		log.Println("Saved fill position to Redis", "file", fill.FilePath, "position", fill.Position)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		misc := &FilePosition{
			FilePath: s.currentMiscFile,
			Position: s.miscPosition,
		}

		mbyte, err := misc.Marshal()
		if err != nil {
			log.Printf("Failed to marshal misc event position: %v", err)
			return
		}

		if err := wredis.WriteString(s.ctx, s.redisClient, WATCH_MISC, string(mbyte)); err != nil {
			log.Printf("Failed to save file position to Redis: %v", err)
			return
		}
		log.Println("Saved misc position to Redis", "file", misc.FilePath, "position", misc.Position)
	}()

	wg.Wait()
}

// CatchUpEvents when the service starts, it needs to catch up with the events that were missed during the previous run.
func (s *Service) CatchUpEvents() {

	lastFIlls, err1 := wredis.ReadString(s.ctx, s.redisClient, WATCH_FILLS)
	lastMisc, err2 := wredis.ReadString(s.ctx, s.redisClient, WATCH_MISC)

	if err1 != nil || err2 != nil {
		log.Println("Failed to read last event positions from Redis", err1, err2)
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()

		if lastFIlls != "" {
			f := new(FilePosition)
			if err := f.Unmarshal([]byte(lastFIlls)); err != nil {
				log.Println("Failed to unmarshal last fills event position")
				return
			}
			log.Println("Catching up fills events", "file", f.FilePath, "position", f.Position)

			if f.FilePath == s.currentFillFile {
				s.fillPosition = f.Position
				log.Println("Same with current file, skip catch up", "start position", f.Position)
				return
			}

			files, err := FilesInDuration(s.config.DataDir, f.FilePath)
			if err != nil {
				log.Println("Failed to get files in duration")
				return
			}
			s.quickProcessFills(*f, files)
		}

	}()

	go func() {
		defer wg.Done()

		if lastMisc != "" {
			f := new(FilePosition)
			if err := f.Unmarshal([]byte(lastMisc)); err != nil {
				log.Println("Failed to unmarshal last fills event position")
				return
			}
			log.Println("Catching up miscs events", "file", f.FilePath, "position", f.Position)
			if f.FilePath == s.currentMiscFile {
				s.miscPosition = f.Position
				log.Println("Same with current file, skip catch up", "start position", f.Position)
				return
			}

			files, err := FilesInDuration(s.config.DataDir, f.FilePath)
			if err != nil {
				log.Println("Failed to get files in duration")
				return
			}
			s.quickProcessMiscs(*f, files)
		}
	}()

	wg.Wait()
}

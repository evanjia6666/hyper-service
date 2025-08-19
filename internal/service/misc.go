package service

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"os"
	"time"

	"github.com/uxuyprotocol/hyper-service/internal/model"
)

// processMiscFile processes a misc file
func (s *Service) processMiscFile() {
	filename := s.currentMiscFile
	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Failed to open file %s: %v", filename, err)
		return
	}
	defer file.Close()

	startPos := int64(s.miscPosition)

	// Seek to the starting position
	_, err = file.Seek(startPos, io.SeekStart)
	if err != nil {
		log.Printf("Failed to seek in file %s: %v", filename, err)
		return
	}
	log.Println("processing file........: ", filename, "positon", startPos)
	reader := bufio.NewReader(file)
	for {
		select {
		case <-s.quit:
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// If not the current hour file, EOF means we can stop
				if !isCurrentHourFile(filename) {
					log.Printf("Finished read line from file %s: %v", filename, err)
					return
				}
				// For current hour files, sleep and continue watching
				time.Sleep(200 * time.Millisecond)
				continue
			}
			log.Printf("Failed to read line from file %s: %v", filename, err)
			return
		}

		s.updateFilePosition(filename, int64(len(line)))

		if line == "" {
			continue
		}

		// Try to parse as JSON
		var data MiscData
		err = json.Unmarshal([]byte(line), &data)
		if err != nil {
			log.Printf("Failed to parse JSON line in file %s: %v", filename, err)
			continue
		}

		for _, event := range data.Events {
			inFilter := false
			watchUsers := []string{}
			ledger, ok := event.Inner["LedgerUpdate"]
			if ok {
				if obj, ok := ledger.(map[string]interface{}); ok {
					if users, ok := obj["users"].([]interface{}); ok {
						// Check if at least a user in Bloom filter
						s.bloomFilterMux.RLock()
						for _, user := range users {
							if s.bloomFilter.TestString(user.(string)) {
								inFilter = true
								watchUsers = append(watchUsers, user.(string))
							}
						}
						s.bloomFilterMux.RUnlock()
					}

				}
			}

			if inFilter && len(watchUsers) > 0 {
				event.Inner["hash"] = event.Hash
				event.Inner["time"] = event.Time
				// Convert to our Event struct
				var datas []model.Event
				for u := range watchUsers {
					ev := model.Event{
						User:  watchUsers[u],
						Event: EVENT_MISC,
						Block: data.BlockNum,
						Data:  event.Inner,
					}
					datas = append(datas, ev)
					s.broadcastEventToSubscribers(ev, EVENT_MISC)
				}
				if len(datas) > 0 {
					err := model.InsertEvent(s.db, datas)
					if err != nil {
						log.Println("Error inserting event:", err)
					}
				}
			}
		}
	}
}

func (s *Service) quickProcessMiscs(start FilePosition, files []string) {
	for _, file := range files {
		position := uint64(0)
		if file == start.FilePath {
			// 需要从 start.Position 开始处理
			position = start.Position
		}
		if file == s.currentMiscFile {
			continue
		}

		err := batchProcessFile(file, position, func(data []string) error {
			var evs []model.Event
			for _, line := range data {
				if line == "" {
					continue
				}

				// Try to parse as JSON
				var mdata MiscData
				err := json.Unmarshal([]byte(line), &mdata)
				if err != nil {
					log.Printf("Failed to parse JSON line  %v", err)
					continue
				}

				for _, event := range mdata.Events {
					inFilter := false
					watchUsers := []string{}
					ledger, ok := event.Inner["LedgerUpdate"]
					if ok {
						if obj, ok := ledger.(map[string]interface{}); ok {
							if users, ok := obj["users"].([]string); ok {
								// Check if at least a user in Bloom filter
								s.bloomFilterMux.RLock()
								for _, user := range users {
									if s.bloomFilter.Test([]byte(user)) {
										inFilter = true
										watchUsers = append(watchUsers, user)
									}
								}
								s.bloomFilterMux.RUnlock()
							}

						}
					}

					if inFilter && len(watchUsers) > 0 {
						event.Inner["hash"] = event.Hash
						event.Inner["time"] = event.Time
						// Convert to our Event struct
						for u := range watchUsers {
							ev := model.Event{
								User:  watchUsers[u],
								Event: EVENT_MISC,
								Block: mdata.BlockNum,
								Data:  event.Inner,
							}
							evs = append(evs, ev)
						}
					}
				}
			}
			return model.InsertEvent(s.db, evs)
		})
		if err != nil {
			// TODO: save file position
			log.Printf("Failed to process fills file: %s, %v", file, err)
		}
	}

}

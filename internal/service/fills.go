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

const (
	BatchSize = 50
)

// processFillsFile processes a data file
func (s *Service) processFillsFile() {
	filename := s.currentFillFile
	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Failed to open file %s: %v", filename, err)
		return
	}
	defer file.Close()

	// Determine starting position
	startPos := int64(s.fillPosition)

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
		var data FillData
		err = json.Unmarshal([]byte(line), &data)
		if err != nil {
			log.Printf("Failed to parse JSON line in file %s: %v", filename, err)
			continue
		}

		s.currentBlockHeight.Store(data.BlockNum)

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

			// Check if user is in Bloom filter
			s.bloomFilterMux.RLock()
			inFilter := s.bloomFilter.TestString(user)
			s.bloomFilterMux.RUnlock()

			if inFilter {
				// Convert to our Event struct
				ev := model.Event{
					User:  user,
					Event: EVENT_FILL,
					Block: uint64(data.BlockNum),
					Data:  eventData,
				}

				// Send to WebSocket clients who are subscribed
				s.broadcastEventToSubscribers(ev, EVENT_FILL)

				// Save to database
				err = model.InsertEvent(s.db, []model.Event{ev})
				if err != nil {
					log.Printf("Failed to save event to database: %v", err)
				}
			}
		}
	}
}

func (s *Service) quickProcessFills(start FilePosition, files []string) {
	for _, file := range files {
		position := uint64(0)
		if file == start.FilePath {
			// 需要从 start.Position 开始处理
			position = start.Position
		}

		if file == s.currentFillFile {
			// 跳过当前文件
			continue
		}

		err := batchProcessFile(file, position, func(data []string) error {
			var evs []model.Event
			for _, line := range data {
				if line == "" {
					continue
				}
				// Try to parse as JSON
				var fd FillData
				err := json.Unmarshal([]byte(line), &fd)
				if err != nil {
					log.Printf("Failed to parse JSON line : %v", err)
					continue
				}

				for _, event := range fd.Events {
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

					// Check if user is in Bloom filter
					s.bloomFilterMux.RLock()
					inFilter := s.bloomFilter.TestString(user)
					s.bloomFilterMux.RUnlock()

					if inFilter {
						// Convert to our Event struct
						ev := model.Event{
							User:  user,
							Event: EVENT_FILL,
							Block: uint64(fd.BlockNum),
							Data:  eventData,
						}
						evs = append(evs, ev)
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
func batchProcessFile(file string, position uint64, call func(data []string) error) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Seek(int64(position), io.SeekStart)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	bData := make([]string, 0, BatchSize)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		bData = append(bData, line)
		if len(bData) == BatchSize {
			if err := call(bData); err != nil {
				return err
			}
			bData = bData[:0]
		}
	}
	if len(bData) > 0 {
		if err := call(bData); err != nil {
			return err
		}
		for i := range bData {
			position += uint64(len(bData[i]))
		}
	}
	return nil
}

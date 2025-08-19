package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"encoding/json"
)

type FilePosition struct {
	FilePath string `json:"file_path"`
	Position uint64 `json:"position"`
}

func (f *FilePosition) Marshal() ([]byte, error) {
	return json.Marshal(f)
}

func (f *FilePosition) Unmarshal(data []byte) error {
	return json.Unmarshal(data, f)
}

// FilesInDuration returns all files in a duration
func FilesInDuration(dataDir, startFile string) ([]string, error) {
	startFile = strings.TrimPrefix(startFile, dataDir)
	startFile = strings.TrimPrefix(startFile, "/")
	parts := strings.Split(startFile, "/")
	if len(parts) != 4 || parts[1] != "hourly" {
		return nil, fmt.Errorf("invalid file path format: %s", startFile)
	}
	eventType := parts[0]
	dateStr := parts[2]
	hourStr := parts[3]

	// Parse date
	if len(dateStr) != 8 {
		return nil, fmt.Errorf("invalid date format: %s", dateStr)
	}
	year, err := strconv.Atoi(dateStr[:4])
	if err != nil {
		return nil, fmt.Errorf("invalid year in date: %s", dateStr)
	}
	monthInt, err := strconv.Atoi(dateStr[4:6])
	if err != nil {
		return nil, fmt.Errorf("invalid month in date: %s", dateStr)
	}
	month := time.Month(monthInt)
	if month < time.January || month > time.December {
		return nil, fmt.Errorf("invalid month value: %d", monthInt)
	}
	day, err := strconv.Atoi(dateStr[6:8])
	if err != nil {
		return nil, fmt.Errorf("invalid day in date: %s", dateStr)
	}

	// Parse hour
	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		return nil, fmt.Errorf("invalid hour: %s", hourStr)
	}
	if hour < 0 || hour > 23 {
		return nil, fmt.Errorf("invalid hour value: %d", hour)
	}

	// Create start time in UTC
	tStart := time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
	now := time.Now().UTC()

	var files []string
	for t := tStart; !t.After(now); t = t.Add(time.Hour) {
		dateStr := t.Format("20060102")
		hourStr := strconv.Itoa(t.Hour())
		files = append(files, fmt.Sprintf("%s/%s/hourly/%s/%s", dataDir, eventType, dateStr, hourStr))
	}

	return files, nil
}

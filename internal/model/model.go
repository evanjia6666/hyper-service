package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

func NewDB(host, port, user, password, dbname string) (*sql.DB, error) {
	db, err := sql.Open("postgres", fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	return db, db.Ping()
}

func InitSchema(db *sql.DB) error {
	_, err := db.Exec(`
		-- Events table to store recent events (last week)
		CREATE TABLE IF NOT EXISTS events (
			id SERIAL PRIMARY KEY,
			user_address VARCHAR(64) NOT NULL,
			event_type VARCHAR(32) NOT NULL,
			block_number BIGINT NOT NULL,
			data JSONB NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);

		-- Union Index useraddress, event_type, block_number
		CREATE INDEX IF NOT EXISTS events_user_address_event_type_block_number_idx ON events (user_address, event_type, block_number);
	`)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}

func CleanUp(db *sql.DB, before time.Time) error {
	result, err := db.Exec(
		"DELETE FROM events WHERE created_at < $1", before)
	if err != nil {
		log.Printf("Failed to cleanup old events: %v", err)
		return err
	}
	aff, err := result.RowsAffected()
	if err != nil {
		log.Printf("Failed to get rows affected: %v", err)
		return nil
	}
	log.Printf("Cleaned up %d old events", aff)
	return nil
}

// Event represents a single trade event
type Event struct {
	User  string `json:"user"`
	Event string `json:"event"`
	Block uint64
	Data  map[string]interface{} `json:"data"`
}

func InsertEvent(db *sql.DB, data []Event) error {
	if len(data) == 0 {
		return nil
	}

	sql := "INSERT INTO events (user_address, event_type, block_number, data) VALUES "
	// Insert into database
	for _, event := range data {
		eventData, err := json.Marshal(event.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal event data: %w", err)
		}
		sql += fmt.Sprintf("('%s', '%s', %d, '%s'),", event.User, event.Event, event.Block, eventData)
	}

	_, err := db.Exec(sql[:len(sql)-1])
	if err != nil {
		return fmt.Errorf("failed to insert event into database: %w", err)
	}
	return nil
}

func QueryEventsByBlock(db *sql.DB, userAddress, event string, startBlock, endBlock uint64) ([]Event, error) {
	// Query database
	rows, err := db.Query(
		"SELECT user_address, event_type, block_number, data FROM events WHERE user_address = $1 AND event_type = $2 AND block_number >= $3 AND block_number <= $4 ORDER BY block_number",
		userAddress, event, startBlock, endBlock)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect results
	var events []Event
	for rows.Next() {
		var userAddress, eventType string
		var blockNumber uint64
		var data []byte

		err := rows.Scan(&userAddress, &eventType, &blockNumber, &data)
		if err != nil {
			return nil, err
		}

		// Parse JSON data
		var eventData map[string]interface{}
		err = json.Unmarshal(data, &eventData)
		if err != nil {
			return nil, err
		}

		// Create result objec
		result := Event{
			User:  userAddress,
			Event: eventType,
			Block: blockNumber,
			Data:  eventData,
		}
		events = append(events, result)
	}
	return events, nil
}

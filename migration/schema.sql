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
CREATE INDEX IF NOT EXISTS events_block_number_idx ON events (block_number);

-- Query to delete old events (older than 1 week)
-- This should be run periodically as a maintenance task
-- DELETE FROM events WHERE created_at < NOW() - INTERVAL '7 days';
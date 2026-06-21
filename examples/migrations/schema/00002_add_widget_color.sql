-- +goose Up
ALTER TABLE widgets ADD COLUMN color TEXT NOT NULL DEFAULT 'gray';

-- +goose Down
ALTER TABLE widgets DROP COLUMN color;

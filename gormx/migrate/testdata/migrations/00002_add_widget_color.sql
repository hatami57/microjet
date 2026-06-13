-- +goose Up
ALTER TABLE widgets ADD COLUMN color TEXT;

-- +goose Down
ALTER TABLE widgets DROP COLUMN color;

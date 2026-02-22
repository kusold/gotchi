-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS example_records (
	id UUID PRIMARY KEY,
	name TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS example_records;
-- +goose StatementEnd

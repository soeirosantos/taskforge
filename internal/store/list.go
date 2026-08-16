package store

import (
	"context"
	"fmt"
	"strings"

	"taskforge/internal/jobs"
)

// ListFilter narrows List to jobs matching Status and/or Type. A nil field
// means "no filter on that column". Non-nil fields are validated against
// jobs.Status.Valid / jobs.Type.Valid — List rejects unrecognized values
// with ErrInvalidFilter rather than silently matching nothing; translating
// that into an HTTP 400 is the API layer's job, not the store's.
type ListFilter struct {
	Status *jobs.Status
	Type   *jobs.Type
}

// List returns jobs matching the filter, ordered by created_at DESC, id ASC
// — newest first, with ties on created_at broken by id so the order is
// total and deterministic.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]*jobs.Job, error) {
	var conditions []string
	var args []any

	if filter.Status != nil {
		if !filter.Status.Valid() {
			return nil, ErrInvalidFilter
		}
		conditions = append(conditions, "status = ?")
		args = append(args, string(*filter.Status))
	}
	if filter.Type != nil {
		if !filter.Type.Valid() {
			return nil, ErrInvalidFilter
		}
		conditions = append(conditions, "type = ?")
		args = append(args, string(*filter.Type))
	}

	query := "SELECT " + jobColumns + " FROM jobs"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	defer rows.Close()

	result := make([]*jobs.Job, 0)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list jobs: %w", err)
		}
		result = append(result, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}

	return result, nil
}

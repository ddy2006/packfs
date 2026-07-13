package arcset

import "time"

// Arcset represents a row in the t_arcset table.
type Arcset struct {
	ID          int
	Name        string
	Label       string
	Metadata    map[string]any
	Status      string
	CurrentPath string
	LastCheck   time.Time
	Comment     string
}

// Filter for querying arcsets.
type Filter struct {
	Status *string
}

// Update fields for partial updates.
type Update struct {
	Status    *string
	Label     *string
	Comment   *string
	LastCheck *time.Time
	Metadata  map[string]any
}

// DatasetRef is a lightweight reference to a linked dataset.
type DatasetRef struct {
	ID   int
	Name string
}


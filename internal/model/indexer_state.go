package model

// IndexerState persists the last block processed by the event listener.
// A single row (ID=1) is used as a key-value checkpoint.
type IndexerState struct {
	ID               uint   `gorm:"primaryKey"`
	LastIndexedBlock uint64 `gorm:"column:last_indexed_block;not null;default:0"`
}

func (IndexerState) TableName() string {
	return "t_indexer_state"
}

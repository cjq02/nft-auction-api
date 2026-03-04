package model

// IndexerState persists the last block processed by the event listener, per contract.
type IndexerState struct {
	ID               uint   `gorm:"primaryKey"`
	ContractAddress  string `gorm:"column:contract_address;size:42;not null;uniqueIndex"`
	LastIndexedBlock uint64 `gorm:"column:last_indexed_block;not null;default:0"`
}

func (IndexerState) TableName() string {
	return "t_indexer_state"
}

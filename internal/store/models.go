package store

// ProfileFact is one deduplicated memory unit.
type ProfileFact struct {
	ID          int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	OwnerID     string  `gorm:"size:128;not null;uniqueIndex:uidx_owner_hash;index:idx_facts_owner" json:"owner_id"`
	Category    string  `gorm:"size:64;not null;index:idx_facts_owner_cat" json:"category"`
	Content     string  `gorm:"type:text;not null" json:"content"`
	ContentNorm string  `gorm:"type:text;not null" json:"content_norm"`
	ContentHash string  `gorm:"size:64;not null;uniqueIndex:uidx_owner_hash" json:"content_hash"`
	Source      string  `gorm:"size:32;not null;default:chat" json:"source"`
	Confidence  float64 `gorm:"not null;default:1" json:"confidence"`
	CreatedAt   string  `gorm:"size:64;not null" json:"created_at"`
	UpdatedAt   string  `gorm:"size:64;not null" json:"updated_at"`
}

func (ProfileFact) TableName() string { return "profile_facts" }

// ProfileRow is the aggregated person meta row.
type ProfileRow struct {
	OwnerID     string `gorm:"primaryKey;size:128" json:"owner_id"`
	DisplayName string `gorm:"size:256;not null;default:''" json:"display_name"`
	Summary     string `gorm:"type:text;not null;default:''" json:"summary"`
	BotContact  string `gorm:"size:512;not null;default:''" json:"bot_contact"`
	MetaJSON    string `gorm:"type:text;not null;default:'{}'" json:"meta_json"`
	UpdatedAt   string `gorm:"size:64;not null" json:"updated_at"`
}

func (ProfileRow) TableName() string { return "profiles" }

// ChatMessage persists turn transcripts.
type ChatMessage struct {
	ID        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID string `gorm:"size:128;not null;index:idx_chat_session" json:"session_id"`
	Role      string `gorm:"size:32;not null" json:"role"`
	Content   string `gorm:"type:text;not null" json:"content"`
	CreatedAt string `gorm:"size:64;not null" json:"created_at"`
}

func (ChatMessage) TableName() string { return "chat_messages" }

// Checkpoint stores ADK interrupt/resume blobs.
type Checkpoint struct {
	CheckpointID string `gorm:"primaryKey;size:256" json:"checkpoint_id"`
	Payload      []byte `gorm:"type:blob;not null" json:"payload"`
	UpdatedAt    string `gorm:"size:64;not null" json:"updated_at"`
}

func (Checkpoint) TableName() string { return "checkpoints" }

// JobRequirementRow persists recruiting requirements.
type JobRequirementRow struct {
	ID                string `gorm:"primaryKey;size:64" json:"id"`
	OwnerID           string `gorm:"size:128;not null;index" json:"owner_id"`
	Title             string `gorm:"size:256;not null" json:"title"`
	RequirementsJSON  string `gorm:"type:text;not null" json:"requirements_json"`
	Status            string `gorm:"size:32;not null;default:open" json:"status"`
	CreatedAt         string `gorm:"size:64;not null" json:"created_at"`
	UpdatedAt         string `gorm:"size:64;not null" json:"updated_at"`
}

func (JobRequirementRow) TableName() string { return "job_requirements" }

// CandidateRow persists scored candidates.
type CandidateRow struct {
	ID          string  `gorm:"primaryKey;size:64" json:"id"`
	JobID       string  `gorm:"size:64;not null;index:idx_candidates_job" json:"job_id"`
	Name        string  `gorm:"size:256;not null" json:"name"`
	Source      string  `gorm:"size:64;not null" json:"source"`
	GitHubLogin string  `gorm:"size:128;not null;default:''" json:"github_login"`
	BotEndpoint string  `gorm:"size:512;not null;default:''" json:"bot_endpoint"`
	ProfileJSON string  `gorm:"type:text;not null;default:'{}'" json:"profile_json"`
	Score       float64 `gorm:"not null;default:0;index:idx_candidates_job" json:"score"`
	RankPos     int     `gorm:"not null;default:0" json:"rank_pos"`
	Notes       string  `gorm:"type:text;not null;default:''" json:"notes"`
	CreatedAt   string  `gorm:"size:64;not null" json:"created_at"`
	UpdatedAt   string  `gorm:"size:64;not null" json:"updated_at"`
}

func (CandidateRow) TableName() string { return "candidates" }

// A2AMessageRow persists agent-to-agent traffic.
type A2AMessageRow struct {
	ID           string `gorm:"primaryKey;size:64" json:"id"`
	Direction    string `gorm:"size:16;not null" json:"direction"`
	PeerBotID    string `gorm:"size:128;not null" json:"peer_bot_id"`
	PeerEndpoint string `gorm:"size:512;not null;default:''" json:"peer_endpoint"`
	Intent       string `gorm:"size:128;not null" json:"intent"`
	PayloadJSON  string `gorm:"type:text;not null" json:"payload_json"`
	Status       string `gorm:"size:64;not null" json:"status"`
	CreatedAt    string `gorm:"size:64;not null" json:"created_at"`
}

func (A2AMessageRow) TableName() string { return "a2a_messages" }

// MarketRecruitTaskRow tracks a delegated recruiting task on an A2A market agent.
type MarketRecruitTaskRow struct {
	ID            string `gorm:"primaryKey;size:64" json:"id"`
	JobID         string `gorm:"size:64;not null;index:idx_market_task_job" json:"job_id"`
	AgentURL      string `gorm:"size:512;not null" json:"agent_url"`
	AgentBotID    string `gorm:"size:128;not null;default:''" json:"agent_bot_id"`
	Status        string `gorm:"size:32;not null;default:pending;index" json:"status"` // pending|running|completed|failed
	CallbackToken string `gorm:"size:128;not null;default:''" json:"callback_token"`
	ResultJSON    string `gorm:"type:text;not null;default:'[]'" json:"result_json"`
	ErrorMsg      string `gorm:"type:text;not null;default:''" json:"error_msg"`
	CreatedAt     string `gorm:"size:64;not null" json:"created_at"`
	UpdatedAt     string `gorm:"size:64;not null" json:"updated_at"`
}

func (MarketRecruitTaskRow) TableName() string { return "market_recruit_tasks" }

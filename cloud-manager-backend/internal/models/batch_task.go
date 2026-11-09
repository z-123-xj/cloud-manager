package models

import "time"

// 批量任务状态
const (
	BatchTaskRunning   = "running"
	BatchTaskSucceeded = "succeeded"
	BatchTaskPartial   = "partial"
	BatchTaskFailed    = "failed"
	BatchTaskCanceled  = "canceled"
)

// 单台主机执行结果状态
const (
	BatchResultPending  = "pending"
	BatchResultRunning  = "running"
	BatchResultSuccess  = "success"
	BatchResultFailed   = "failed"
	BatchResultCanceled = "canceled"
)

// BatchTask 批量命令/脚本执行任务。
// Script 明文落库用于审计（谁在哪台机器上执行了什么），因此不要在此传入密钥类内容。
type BatchTask struct {
	ID             uint64     `json:"id" gorm:"primaryKey"`
	Title          string     `json:"title" gorm:"type:varchar(200);not null"`
	Script         string     `json:"script" gorm:"type:mediumtext;not null"`
	Shell          string     `json:"shell" gorm:"type:varchar(20);not null"`
	Network        string     `json:"network" gorm:"type:varchar(20);not null"`
	CredentialID   uint       `json:"credential_id"`
	CredentialName string     `json:"credential_name" gorm:"type:varchar(100);not null"`
	Concurrency    int        `json:"concurrency"`
	TimeoutSec     int        `json:"timeout_sec"`
	Status         string     `json:"status" gorm:"type:varchar(20);not null;index:idx_status"`
	Total          int        `json:"total"`
	Succeeded      int        `json:"succeeded"`
	Failed         int        `json:"failed"`
	CreatedBy      uint       `json:"created_by"`
	CreatedName    string     `json:"created_name" gorm:"type:varchar(100);not null"`
	ClientIP       string     `json:"client_ip" gorm:"type:varchar(64);not null"`
	ErrMsg         string     `json:"err_msg" gorm:"type:varchar(1000);not null"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (BatchTask) TableName() string {
	return "batch_tasks"
}

// BatchTaskResult 单台主机的一次执行结果。
// ExitCode 用指针区分「未执行」与「退出码 0」。
type BatchTaskResult struct {
	ID         uint64     `json:"id" gorm:"primaryKey"`
	TaskID     uint64     `json:"task_id" gorm:"index:idx_task_id"`
	EcsID      uint64     `json:"ecs_id" gorm:"index:idx_ecs_id"`
	AssetName  string     `json:"asset_name" gorm:"type:varchar(200);not null"`
	Host       string     `json:"host" gorm:"type:varchar(128);not null"`
	Port       int        `json:"port"`
	Status     string     `json:"status" gorm:"type:varchar(20);not null"`
	ExitCode   *int       `json:"exit_code"`
	Stdout     string     `json:"stdout" gorm:"type:text"`
	Stderr     string     `json:"stderr" gorm:"type:text"`
	ErrMsg     string     `json:"err_msg" gorm:"type:varchar(1000);not null"`
	DurationMs int64      `json:"duration_ms"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (BatchTaskResult) TableName() string {
	return "batch_task_results"
}

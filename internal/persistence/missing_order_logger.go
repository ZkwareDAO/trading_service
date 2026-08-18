package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MissingOrderLogger 专门记录 RPC 失败导致的缺失 uprunning_order。
// 日志文件路径: {dataDir}/logs/missing_uprunning_orders.log
// 日志格式: JSON (便于后续批量解析和恢复)
type MissingOrderLogger struct {
	logFile *os.File
	mu      sync.Mutex
}

// NewMissingOrderLogger 创建缺失订单日志记录器。
func NewMissingOrderLogger(dataDir string) (*MissingOrderLogger, error) {
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create logs directory: %w", err)
	}

	logPath := filepath.Join(logDir, "missing_uprunning_orders.log")
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open missing order log file: %w", err)
	}

	return &MissingOrderLogger{logFile: file}, nil
}

// LogMissingOrder 记录缺失的 uprunning_order 数据。
// 格式: JSON + 时间戳,便于后续解析和批量恢复。
func (l *MissingOrderLogger) LogMissingOrder(orderData map[string]interface{}) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"order":     orderData,
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal missing order data: %w", err)
	}

	line := fmt.Sprintf("%s\n", jsonData)
	if _, err := l.logFile.WriteString(line); err != nil {
		return fmt.Errorf("write missing order log: %w", err)
	}

	return nil
}

// Close 关闭日志文件。
func (l *MissingOrderLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}
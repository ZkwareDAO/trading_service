package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMissingOrderLogger(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewMissingOrderLogger(dir)
	require.NoError(t, err)
	defer logger.Close()

	orderData := map[string]interface{}{
		"user_id":              uint64(123),
		"relation_id":          uint64(456),
		"relation_type":        "user_orders",
		"exchange":             "binance",
		"symbol":               "BTCUSDT",
		"pos_type":             2,
		"exchange_order_id":    uint64(789),
		"exchange_order_status": "NEW",
		"exchange_order_price": 45000.50,
		"exchange_order_qty":   0.001,
		"side":                 0,
	}

	err = logger.LogMissingOrder(orderData)
	require.NoError(t, err)

	// 验证日志文件存在且可读
	logPath := filepath.Join(dir, "logs", "missing_uprunning_orders.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "user_id")
	require.Contains(t, string(content), "123")

	// 验证 JSON 格式可解析
	var entry map[string]interface{}
	err = json.Unmarshal(content[:len(content)-1], &entry) // 去掉末尾换行符
	require.NoError(t, err)
	require.Contains(t, entry, "timestamp")
	require.Contains(t, entry, "order")

	order := entry["order"].(map[string]interface{})
	require.Equal(t, float64(123), order["user_id"])
	require.Equal(t, "user_orders", order["relation_type"])
}

func TestMissingOrderLoggerMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewMissingOrderLogger(dir)
	require.NoError(t, err)
	defer logger.Close()

	// 记录多条缺失订单
	for i := 1; i <= 3; i++ {
		orderData := map[string]interface{}{
			"user_id":     uint64(i),
			"relation_id": uint64(i * 100),
			"exchange":    "binance",
		}
		err = logger.LogMissingOrder(orderData)
		require.NoError(t, err)
	}

	// 验证日志文件包含 3 条记录
	logPath := filepath.Join(dir, "logs", "missing_uprunning_orders.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	lines := splitLines(string(content))
	require.Len(t, lines, 3)

	// 验证每条记录都是有效的 JSON
	for _, line := range lines {
		var entry map[string]interface{}
		err = json.Unmarshal([]byte(line), &entry)
		require.NoError(t, err)
		require.Contains(t, entry, "timestamp")
		require.Contains(t, entry, "order")
	}
}

func TestMissingOrderLoggerDirectoryCreation(t *testing.T) {
	// 测试 logs 目录不存在时自动创建
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")

	// 确保 logs 目录不存在
	_ = os.RemoveAll(logsDir)

	logger, err := NewMissingOrderLogger(dir)
	require.NoError(t, err)
	defer logger.Close()

	// 验证 logs 目录已创建
	info, err := os.Stat(logsDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
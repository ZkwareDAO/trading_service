package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

// listUserStrategiesHandler handles GET /api/v1/user-strategies
func (s *Server) listUserStrategiesHandler(w http.ResponseWriter, r *http.Request) {
	// 记录请求日志
	log.Printf("[/api/v1/user-strategies] Received request: method=%s, remote=%s, user-agent=%s, query=%s",
		r.Method, r.RemoteAddr, r.Header.Get("User-Agent"), r.URL.RawQuery)

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	userIDStr := query.Get("user_id")
	userName := query.Get("user_name")
	strategyName := query.Get("strategy_name")

	// 解析并验证user_id参数（提前解析，避免循环内重复解析）
	var filterUserID uint64
	var hasUserIDFilter bool
	if userIDStr != "" {
		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "invalid user_id format")
			return
		}
		filterUserID = userID
		hasUserIDFilter = true
	}

	// 解析user_name参数（提前解析，避免N+1查询）
	var filterByUserName uint64
	var hasUserNameFilter bool
	if userName != "" && !hasUserIDFilter {
		// 需要exchange参数来查找user
		// 从strategies中推断exchange，或使用第一个匹配的user
		strategies := s.repo.ListUserStrategies()
		if len(strategies) == 0 {
			// 没有任何策略，返回空结果
			writeAPISuccess(w, []map[string]interface{}{})
			return
		}

		// 尝试从第一个策略获取exchange（简化逻辑）
		exchange := strategies[0].Exchange
		userID, err := s.repo.FindUserIDByName(userName, exchange)
		if err != nil {
			writeAPIError(w, 1001, fmt.Sprintf("user '%s' not found", userName))
			return
		}
		filterByUserName = userID
		hasUserNameFilter = true
	}

	// 获取所有user strategies
	strategies := s.repo.ListUserStrategies()

	// 初始化filtered为空数组（而不是nil）
	filtered := make([]map[string]interface{}, 0)

	// 应用过滤器
	for _, us := range strategies {
		// 按user_id过滤
		if hasUserIDFilter && us.UserID != filterUserID {
			continue
		}

		// 按user_name过滤
		if hasUserNameFilter && us.UserID != filterByUserName {
			continue
		}

		// 按strategy_name过滤（精确匹配）
		if strategyName != "" && us.Name != strategyName {
			continue
		}

		// 只返回安全字段（排除敏感信息）
		filtered = append(filtered, map[string]interface{}{
			"id":                 us.ID,
			"user_id":            us.UserID,
			"name":               us.Name,
			"exchange":           us.Exchange,
			"cash":               us.Cash,
			"parts":              us.Parts,
			"status":             us.Status,
			"risk_strategy_type": us.RiskStrategyType,
			"orders_num":         us.OrdersNum,
			"valid_before":       us.ValidBefore,
			"created_at":         us.CreatedAt,
			"updated_at":         us.UpdatedAt,
		})
	}

	// 返回标准响应格式
	writeAPISuccess(w, filtered)
}

// writeAPIError 返回标准错误响应格式
func writeAPIError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	if code >= 5000 {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    code,
		"message": message,
		"data":    nil,
	})
}

// writeAPISuccess 返回标准成功响应格式
func writeAPISuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

type trackingEvent struct {
	Date        string `json:"date"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type trackingStep struct {
	Step    string          `json:"step"`
	Status  string          `json:"status"`
	History []trackingEvent `json:"history"`
}

// GetHistoryOrderTracking resolves the original account from the saved record.
func GetHistoryOrderTracking(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.HistoryMu.Lock()
		var accountID, orderID string
		matches := 0
		for _, h := range state.History {
			if h.ID == c.Param("id") {
				matches++
				if h.Status == "success" {
					accountID, orderID = h.AccountID, h.OrderID
				}
			}
		}
		state.HistoryMu.Unlock()
		if matches != 1 || orderID == "" {
			c.JSON(http.StatusConflict, gin.H{"error": "找不到唯一的成功订单记录"})
			return
		}
		readOrderTracking(c, state, accountID, orderID)
	}
}

// GetOrderTracking requires an explicit account instead of the active account.
func GetOrderTracking(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		accounts := c.QueryArray("account")
		if len(accounts) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请指定下单账户"})
			return
		}
		readOrderTracking(c, state, accounts[0], c.Param("order_id"))
	}
}

func readOrderTracking(c *gin.Context, state *app.State, accountID, orderID string) {
	c.Header("Cache-Control", "private, no-store")
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != orderID || strings.TrimSpace(accountID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请指定明确的下单账户和有效订单号"})
		return
	}
	acc, ok := state.FindAccount(accountID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "原下单账户不存在"})
		return
	}
	client, err := state.OVH.IsolatedClientForAccount(acc)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "创建订单查询客户端失败: " + ovh.Explain(err)})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	client.Timeout = 30 * time.Second
	var status string
	if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/status", id), &status); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取订单状态失败: " + ovh.Explain(err)})
		return
	}
	if status == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "OVH 未返回订单状态"})
		return
	}
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	steps := []trackingStep{}
	if err := client.GetWithContext(ctx, fmt.Sprintf("/me/order/%d/followUp", id), &steps); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取订单处理历史失败: " + ovh.Explain(err)})
		return
	}
	detailState := "available"
	if len(steps) == 0 {
		detailState = "empty"
	}
	c.JSON(http.StatusOK, gin.H{
		"orderId": orderID, "orderStatus": status, "orderStatusAt": observedAt, "statusSource": "live",
		"detailState": detailState, "steps": steps, "checkedAt": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

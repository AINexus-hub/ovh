package handlers

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

func TestTelegramRecentMatchesPurchaseHistory(t *testing.T) {
	// SQLite loads newest first, then a newly completed attempt is appended.
	state := &app.State{History: []types.PurchaseHistoryEntry{
		{PlanCode: "delivering-plan", Status: "success", OrderStatus: "delivering", OrderID: "42", PurchaseTime: "2026-11-25T23:40:00.914972"},
		{PlanCode: "old-plan", Status: "success", OrderStatus: "notPaid", OrderID: "41", PurchaseTime: "2026-11-24T16:46:00"},
		{PlanCode: "new-plan", Status: "success", OrderStatus: "notPaid", OrderID: "43", PurchaseTime: "2026-11-26T05:14:47.412725"},
	}}
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	GetPurchaseHistory(state)(c)
	var web []types.PurchaseHistoryEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &web); err != nil || len(web) != 3 {
		t.Fatalf("unexpected history response: %s (%v)", rr.Body.String(), err)
	}
	text, kb := recentPage(state)
	rows := kb["inline_keyboard"].([][]map[string]interface{})
	if len(rows) != 3 {
		t.Fatalf("unexpected buttons: %v", rows)
	}
	previous := -1
	for i, plan := range []string{"new-plan", "delivering-plan", "old-plan"} {
		position := strings.Index(text, plan)
		if web[i].PlanCode != plan || position <= previous || !strings.Contains(rows[i][0]["text"].(string), plan) {
			t.Fatalf("row %d: expected %s in web, Telegram text, and buttons", i, plan)
		}
		previous = position
	}
	if !strings.Contains(text, "订单 42 · 已付款 · 交付中\n") || strings.Contains(rows[1][0]["text"].(string), "待付款") {
		t.Fatalf("delivering must not appear as unpaid: %s, %v", text, rows[1])
	}
	if !strings.Contains(text, "订单 43 · 待付款（逾期未付款会作废）") || !strings.Contains(rows[0][0]["text"].(string), "待付款") {
		t.Fatalf("unpaid order must still show a warning and payment button: %s, %v", text, rows[0])
	}
	if state.History[0].PlanCode != "delivering-plan" || state.History[2].PlanCode != "new-plan" {
		t.Fatal("view must not mutate stored history")
	}
}

func TestTelegramRecentButtonsOnlyForVisibleOrders(t *testing.T) {
	state := &app.State{History: []types.PurchaseHistoryEntry{
		{PlanCode: "hidden-plan", Status: "success", OrderStatus: "notPaid", OrderID: "1", PurchaseTime: "2026-11-01T00:00:00"},
	}}
	for i := 0; i < tgMaxListItems; i++ {
		state.History = append(state.History, types.PurchaseHistoryEntry{PlanCode: "failed-plan", Status: "failed", PurchaseTime: fmt.Sprintf("2026-11-%02dT00:00:00", i+2)})
	}
	text, kb := recentPage(state)
	if kb != nil || strings.Contains(text, "hidden-plan") || strings.Count(text, "failed-plan") != tgMaxListItems {
		t.Fatalf("Telegram must show only the most recent %d entries and their buttons: %s, %v", tgMaxListItems, text, kb)
	}
}

func TestTelegramOrderStatusLabels(t *testing.T) {
	for status, want := range map[string]string{
		"notPaid": "待付款（逾期未付款会作废）", "checking": "付款核验中",
		"delivering": "已付款 · 交付中", "delivered": "已付款 · 已交付",
		"cancelling": "取消中", "cancelled": "已取消", "documentsRequested": "需补材料",
		"unknown": "状态未知，请到 OVH 面板确认", "unrecognized": "状态未知，请到 OVH 面板确认", "": "状态未查到，请到 OVH 面板确认",
	} {
		if got := telegramOrderStatusLabel(status); got != want {
			t.Errorf("status %q: got %q, want %q", status, got, want)
		}
	}
}

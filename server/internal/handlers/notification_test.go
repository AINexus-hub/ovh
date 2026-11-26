package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/types"
)

func TestNotifyChannelsVerifyDoesNotSendWebhook(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	st, _ := newWatchTestMonitor(t)
	if err := st.Config.Set(types.Config{NotifyWebhookURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/notify/channels", GetNotifyChannels(st))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/notify/channels?verify=true", nil))
	var result struct {
		AnyAvailable bool             `json:"anyAvailable"`
		Channels     []notify.Channel `json:"channels"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !result.AnyAvailable || len(result.Channels) != 2 || !result.Channels[1].ConfigOnly {
		t.Fatalf("unexpected channel status: %d %+v", response.Code, result)
	}
	if requests.Load() != 0 {
		t.Fatalf("GET status sent %d Webhook requests", requests.Load())
	}
}

func TestNotificationUsesActualDeliveryWithoutSecondWebhook(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		status, delivered int
		ok                bool
	}{
		{"accepted", `{"code":0,"msg":"success"}`, 200, 1, true},
		{"business rejected", `{"code":19002,"msg":"msg_type need"}`, 500, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); io.WriteString(w, tc.body) }))
			defer srv.Close()
			st, _ := newWatchTestMonitor(t)
			st.Config.Set(types.Config{NotifyWebhookURL: srv.URL})
			router := gin.New()
			router.POST("/test-notification", TestNotification(st))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/test-notification", nil))
			var result struct {
				Delivered int              `json:"delivered"`
				Channels  []notify.Channel `json:"channels"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != tc.status || result.Delivered != tc.delivered || len(result.Channels) != 2 || result.Channels[1].OK != tc.ok || !result.Channels[1].Configured {
				t.Fatalf("incorrect delivery result: %d %+v", response.Code, result)
			}
			if !tc.ok && !strings.Contains(result.Channels[1].Detail, "19002") {
				t.Fatal("business rejection reason missing")
			}
			if requests.Load() != 1 {
				t.Fatalf("test must send exactly once, got %d", requests.Load())
			}
		})
	}
}

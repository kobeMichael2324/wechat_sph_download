package wxchannelsadapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type feedShareURLResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func TestHandleFetchFeedShareURLRequiresOID(t *testing.T) {
	routes := NewWebsocketRoutes(0, nil, "")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channels/feed/share_url", nil)

	routes.HandleFetchFeedShareUrl(ctx)

	response := decodeFeedShareURLResponse(t, recorder)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if response.Msg != "missing oid" {
		t.Fatalf("response message = %q, want %q", response.Msg, "missing oid")
	}
}

func TestHandleFetchFeedShareURLForwardsFrontendResponse(t *testing.T) {
	routes := NewWebsocketRoutes(0, nil, "")
	server := httptest.NewServer(http.HandlerFunc(routes.client.ServeWebsocket))
	t.Cleanup(server.Close)

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatalf("dial frontend websocket: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		routes.client.Stop()
	})
	waitForChannelsClient(t, routes)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channels/feed/share_url?oid=15011675752678627734",
		nil,
	)

	handlerDone := make(chan struct{})
	go func() {
		routes.HandleFetchFeedShareUrl(ctx)
		close(handlerDone)
	}()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set websocket read deadline: %v", err)
	}
	var request struct {
		Type string `json:"type"`
		Data struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Data struct {
				OID string `json:"oid"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := conn.ReadJSON(&request); err != nil {
		t.Fatalf("read frontend request: %v", err)
	}
	if request.Type != "api_call" {
		t.Fatalf("request type = %q, want %q", request.Type, "api_call")
	}
	if request.Data.Key != "key:channels:feed_share_url" {
		t.Fatalf("request key = %q, want %q", request.Data.Key, "key:channels:feed_share_url")
	}
	if request.Data.Data.OID != "15011675752678627734" {
		t.Fatalf("request oid = %q, want %q", request.Data.Data.OID, "15011675752678627734")
	}

	frontendResponse := map[string]any{
		"id": request.Data.ID,
		"data": map[string]any{
			"errCode": 0,
			"errMsg":  "ok",
			"data": map[string]any{
				"feedH5Url": "https://weixin.qq.com/sph/AZzeuhbLhU",
			},
		},
	}
	if err := conn.WriteJSON(frontendResponse); err != nil {
		t.Fatalf("write frontend response: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("share URL handler did not return")
	}

	response := decodeFeedShareURLResponse(t, recorder)
	if response.Code != 0 {
		t.Fatalf("response code = %d, want 0; message = %q", response.Code, response.Msg)
	}
	var data struct {
		Data struct {
			FeedH5URL string `json:"feedH5Url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("decode response data: %v", err)
	}
	if data.Data.FeedH5URL != "https://weixin.qq.com/sph/AZzeuhbLhU" {
		t.Fatalf("feed share URL = %q, want %q", data.Data.FeedH5URL, "https://weixin.qq.com/sph/AZzeuhbLhU")
	}
}

func TestHandleFetchFeedShareURLReportsUnavailableFrontend(t *testing.T) {
	routes := NewWebsocketRoutes(0, nil, "")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channels/feed/share_url?oid=15011675752678627734",
		nil,
	)

	routes.HandleFetchFeedShareUrl(ctx)

	response := decodeFeedShareURLResponse(t, recorder)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if response.Msg != "please initialize the client socket connection first" {
		t.Fatalf("response message = %q, want unavailable frontend error", response.Msg)
	}
}

func decodeFeedShareURLResponse(t *testing.T, recorder *httptest.ResponseRecorder) feedShareURLResponse {
	t.Helper()
	var response feedShareURLResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode HTTP response: %v", err)
	}
	return response
}

func waitForChannelsClient(t *testing.T, routes *WebsocketRoutes) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !routes.client.Available() {
		if time.Now().After(deadline) {
			t.Fatal("frontend websocket did not connect")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

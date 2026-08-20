package mgmt

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeHandler 记录分发调用。
type fakeHandler struct {
	mu     sync.Mutex
	method string
	params json.RawMessage
}

func (h *fakeHandler) Handle(method string, params json.RawMessage) (json.RawMessage, *RPCError) {
	h.mu.Lock()
	h.method, h.params = method, params
	h.mu.Unlock()
	return json.RawMessage(`{"ok":true}`), nil
}

// pair 建立 net.Pipe 对,一端喂 Server.handleConn,另一端模拟客户端。
func pair(t *testing.T, token string, h Handler) (net.Conn, *Server) {
	t.Helper()
	c1, c2 := net.Pipe()
	s := &Server{ln: nil, h: h, token: token, log: nil}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleConn(c1)
	}()
	t.Cleanup(func() {
		_ = c2.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return c2, s
}

func TestDispatchRoundTrip(t *testing.T) {
	conn, _ := pair(t, "", &fakeHandler{})
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	req := Request{Method: MethodStatus, ID: 42}
	if err := enc.Encode(req); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 42 {
		t.Errorf("ID 应回显,得到 %d", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("不应有错误: %+v", resp.Error)
	}
	var got map[string]bool
	if err := json.Unmarshal(resp.Result, &got); err != nil || !got["ok"] {
		t.Errorf("结果不符: %s (%v)", resp.Result, err)
	}
}

func TestDispatchAuthRequired(t *testing.T) {
	h := &fakeHandler{}
	conn, _ := pair(t, "s3cret-token", h)
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	// 无 token → 拒绝
	_ = enc.Encode(Request{Method: MethodStop, ID: 1})
	var resp1 Response
	_ = dec.Decode(&resp1)
	if resp1.Error == nil || resp1.Error.Code != -32001 {
		t.Fatalf("无 token 应拒绝(-32001),得到 %+v", resp1.Error)
	}
	// 错误 token → 拒绝
	_ = enc.Encode(Request{Method: MethodStop, ID: 2, Auth: "wrong"})
	var resp2 Response
	_ = dec.Decode(&resp2)
	if resp2.Error == nil || resp2.Error.Code != -32001 {
		t.Fatalf("错误 token 应拒绝(-32001),得到 %+v", resp2.Error)
	}
	// 正确 token → 放行
	// 注意:每次解码须用全新 Response 结构(Go json 对缺失字段保留旧值,
	// 复用同一变量会使成功响应的 Error 残留上一次的错误)。
	_ = enc.Encode(Request{Method: MethodStop, ID: 3, Auth: "s3cret-token"})
	var resp3 Response
	_ = dec.Decode(&resp3)
	if resp3.Error != nil {
		t.Fatalf("正确 token 应放行: %+v", resp3.Error)
	}
	if h.method != MethodStop {
		t.Errorf("应分发到 daemon.stop,得到 %s", h.method)
	}
}

func TestDispatchEmptyTokenSkipsAuth(t *testing.T) {
	conn, _ := pair(t, "", &fakeHandler{})
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	_ = enc.Encode(Request{Method: MethodStatus, ID: 1})
	var resp Response
	_ = dec.Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("空 token(未配置)应跳过鉴权: %+v", resp.Error)
	}
}

// TestDispatchParamsForward 参数透传。
func TestDispatchParamsForward(t *testing.T) {
	h := &fakeHandler{}
	conn, _ := pair(t, "", h)
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	params := json.RawMessage(`{"lines":50}`)
	_ = enc.Encode(Request{Method: MethodLogTail, ID: 7, Params: params})
	var resp Response
	_ = dec.Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("不应有错误: %+v", resp.Error)
	}
	if string(h.params) != string(params) {
		t.Errorf("参数应原样透传,得到 %s", h.params)
	}
}

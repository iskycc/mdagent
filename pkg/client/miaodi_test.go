package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newTestServer(response string) (*httptest.Server, *MiaodiClient) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	return srv, NewMiaodiClientWithHTTP(srv.Client())
}

func TestMiaodiClient_Check_True(t *testing.T) {
	srv, client := newTestServer(`{"code": 20000}`)
	defer srv.Close()

	// 覆盖 url 为测试服务器：这里通过替换 client.client.Transport 实现
	client.client.Transport = &rewriteTransport{base: srv.URL}

	if !client.Check("key") {
		t.Error("expected Check true")
	}
}

func TestMiaodiClient_Check_False(t *testing.T) {
	srv, client := newTestServer(`{"code": 50000}`)
	defer srv.Close()
	client.client.Transport = &rewriteTransport{base: srv.URL}

	if client.Check("key") {
		t.Error("expected Check false")
	}
}

func TestMiaodiClient_GetInfo(t *testing.T) {
	srv, client := newTestServer(`{"code": 20000, "data": {"user": "test"}}`)
	defer srv.Close()
	client.client.Transport = &rewriteTransport{base: srv.URL}

	res, err := client.GetInfo("key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["code"] != float64(20000) {
		t.Errorf("unexpected code: %v", res["code"])
	}
}

func TestMiaodiClient_PutText(t *testing.T) {
	srv, client := newTestServer(`{"code": 20000}`)
	defer srv.Close()
	client.client.Transport = &rewriteTransport{base: srv.URL}

	res, err := client.PutText("key", "book", "chapter", "title", "content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["code"] != float64(20000) {
		t.Errorf("unexpected code: %v", res["code"])
	}
}

func TestMiaodiClient_SendEmail(t *testing.T) {
	srv, client := newTestServer(`{"code": 20000}`)
	defer srv.Close()
	client.client.Transport = &rewriteTransport{base: srv.URL}

	res, err := client.SendEmail("a@b.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["code"] != float64(20000) {
		t.Errorf("unexpected code: %v", res["code"])
	}
}

func TestMiaodiClient_GetKey(t *testing.T) {
	srv, client := newTestServer(`{"code": 20000, "key": "abc"}`)
	defer srv.Close()
	client.client.Transport = &rewriteTransport{base: srv.URL, keepHost: true}

	res, err := client.GetKey("a@b.com", "1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["code"] != float64(20000) {
		t.Errorf("unexpected code: %v", res["code"])
	}
}

func TestMiaodiClient_UpImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		file, _, err := r.FormFile("image")
		if err != nil {
			t.Errorf("read file failed: %v", err)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "imagebytes" {
			t.Errorf("unexpected file content: %s", string(data))
		}
		if r.FormValue("token") != "token1" {
			t.Errorf("unexpected token: %s", r.FormValue("token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": 20000}`))
	}))
	defer srv.Close()

	client := NewMiaodiClientWithHTTP(srv.Client())
	client.client.Transport = &rewriteTransport{base: srv.URL, keepHost: true}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")
	if err := os.WriteFile(path, []byte("imagebytes"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	res, err := client.UpImage("token1", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["code"] != float64(20000) {
		t.Errorf("unexpected code: %v", res["code"])
	}
}

func TestDecodeResponse_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if _, err := decodeResponse(resp); err == nil {
		t.Error("expected error for invalid json")
	}
}

// rewriteTransport 把对固定域名的请求重写到测试服务器
type rewriteTransport struct {
	base     string
	keepHost bool
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.keepHost {
		req.URL.Scheme = "http"
		req.URL.Host = t.base[len("http://"):]
	} else {
		req.URL.Scheme = "http"
		req.URL.Host = t.base[len("http://"):]
		req.Host = ""
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestNewMiaodiClient(t *testing.T) {
	c := NewMiaodiClient()
	if c.client == nil {
		t.Error("expected http client")
	}
}

func TestMiaodiClient_UpImage_ReadFileError(t *testing.T) {
	client := NewMiaodiClient()
	_, err := client.UpImage("token", "/nonexistent/path.jpg")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMiaodiClient_postForm_Error(t *testing.T) {
	client := NewMiaodiClientWithHTTP(&http.Client{Transport: &errorTransport{}})
	_, err := client.GetInfo("key")
	if err == nil {
		t.Fatal("expected error")
	}
}

type errorTransport struct{}

func (e *errorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("network error")
}

func TestMiaodiClient_UpImage_InvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := NewMiaodiClientWithHTTP(srv.Client())
	client.client.Transport = &rewriteTransport{base: srv.URL, keepHost: true}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")
	_ = os.WriteFile(path, []byte("imagebytes"), 0644)

	_, err := client.UpImage("token", path)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMiaodiClient_Check_Error(t *testing.T) {
	client := NewMiaodiClientWithHTTP(&http.Client{Transport: &errorTransport{}})
	if client.Check("key") {
		t.Error("expected Check false on error")
	}
}

func TestMiaodiClient_UpImage_DoError(t *testing.T) {
	client := NewMiaodiClientWithHTTP(&http.Client{Transport: &errorTransport{}})
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")
	_ = os.WriteFile(path, []byte("imagebytes"), 0644)
	_, err := client.UpImage("token", path)
	if err == nil {
		t.Fatal("expected error")
	}
}

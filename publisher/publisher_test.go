package publisher

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TRACE-REQ: DOM-140
func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) string {
	root := t.TempDir()
	p := filepath.Join(root, "工具 测试")
	put(t, filepath.Join(p, "index.html"), "<title>您好</title><!-- 本地测试注入开始 -->\r\n<script>window.hivePanel = { access_token: 'TEST_ONLY' }</script>\r\n<!-- 本地测试注入结束 -->")
	put(t, filepath.Join(p, "api.json"), "{}")
	put(t, filepath.Join(p, "PANEL.md"), "# 用法")
	put(t, filepath.Join(p, "图片", "报警.png"), "img")
	return root
}
func TestPackage(t *testing.T) {
	root := fixture(t)
	p := panel{"样例", "工具 测试"}
	b, err := makePackage(root, p)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		src, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(src)
	}
	if entries["index.html"] != "<title>您好</title>" || entries["图片/报警.png"] != "img" {
		t.Fatal("ZIP路径或脱敏错误")
	}
	original, _ := os.ReadFile(filepath.Join(root, p.Source, "index.html"))
	if !bytes.Contains(original, []byte("TEST_ONLY")) {
		t.Fatal("源文件已改变")
	}
	for _, sample := range []string{"<!-- 本地测试注入开始 -->", "<!-- 本地测试注入结束 -->", "<!-- 本地测试注入开始 --><script></script><script></script><!-- 本地测试注入结束 -->"} {
		if _, err := cleanHTML([]byte(sample)); err == nil {
			t.Fatal("损坏标记通过检查")
		}
	}
	put(t, filepath.Join(root, p.Source, ".env"), "secret")
	if _, err := makePackage(root, p); err == nil {
		t.Fatal("隐藏文件进入ZIP")
	}
}
func TestDirectoryLink(t *testing.T) {
	root := fixture(t)
	if err := os.Symlink(filepath.Join(root, "工具 测试"), filepath.Join(root, "alias")); err != nil {
		t.Skip("当前账号不允许创建符号链接")
	}
	if _, err := makePackage(root, panel{"链接", "alias"}); err == nil {
		t.Fatal("跟随了目录链接")
	}
}
func TestPopup(t *testing.T) {
	before := "/* 本地测试注入：本地预览 */\nif (!window.hivePanel) {\n  const testContext = {access_token: 'TEST_ONLY'};\n  window.hivePanel = Object.freeze({async getSystemContext(systemKey) {return testContext;}\n  });\n}\n"
	after := "var styleInjected = false;\nrender()"
	got, err := cleanPopup([]byte(before + after))
	if err != nil || strings.TrimSpace(string(got)) != after {
		t.Fatal("弹窗注入清理失败", err)
	}
	if _, err := cleanPopup([]byte(strings.Replace(before, "return testContext;", "return somethingElse;", 1) + after)); err == nil {
		t.Fatal("损坏的边界应停止打包")
	}
}

type fakeHive struct {
	server  *httptest.Server
	rows    map[string]resource
	writes  []string
	mode    string
	logouts int
}

func mock(t *testing.T, rows ...resource) *fakeHive {
	h := &fakeHive{rows: map[string]resource{}}
	for _, r := range rows {
		h.rows[r.ID] = r
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/prefix/api/admin/v1")
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		if path == "/session" && r.Method == "POST" {
			reply(map[string]string{"audience": "ADMIN", "session_token": "session"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer session" {
			http.Error(w, "", 401)
			return
		}
		switch path {
		case "/session":
			if r.Method == "DELETE" {
				h.logouts++
				w.WriteHeader(204)
				return
			}
		case "/business-systems":
			reply(map[string]any{"systems": []map[string]string{{"system_key": "lighting", "business_system_id": "sys", "status": "ACTIVE"}}})
			return
		case resources + "/inspect":
			var body struct{ File struct{ Name string } }
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				http.Error(w, "", 400)
				return
			}
			description := ""
			if h.mode == "frontmatter" {
				description = "来自包的新描述"
			}
			reply(map[string]string{"resource_key": body.File.Name, "description": description})
			return
		case resources:
			if r.URL.Query().Get("system") != "sys" || r.URL.Query().Get("kind") != "PANEL" {
				http.Error(w, "", 400)
				return
			}
			var a []resource
			for _, row := range h.rows {
				a = append(a, row)
			}
			reply(map[string]any{"resources": a})
			return
		}
		ids := strings.Split(strings.TrimPrefix(path, resources+"/"), "/")
		row, ok := h.rows[ids[0]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method == "GET" && len(ids) == 1 {
			reply(row)
			return
		}
		if len(ids) != 2 || r.Header.Get("x-request-id") == "" {
			http.Error(w, "", 400)
			return
		}
		var body struct {
			Revision int `json:"expected_revision"`
			File     struct {
				Name  string
				Bytes []byte `json:"content_base64"`
			}
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Revision != row.Revision {
			http.Error(w, "", 409)
			return
		}
		h.writes = append(h.writes, row.Key+"/"+ids[1])
		switch ids[1] {
		case "versions":
			if body.File.Name != row.Key || !bytes.HasPrefix(body.File.Bytes, []byte("PK")) {
				http.Error(w, "", 422)
				return
			}
			row.Draft = "new-" + row.ID
			row.Revision++
			h.rows[row.ID] = row
			if h.mode == "lost-upload" {
				w.WriteHeader(502)
				return
			}
			reply(row)
		case "publish":
			if h.mode == "conflict" {
				w.WriteHeader(409)
				return
			}
			row.Current = row.Draft
			row.Number++
			row.Revision++
			h.rows[row.ID] = row
			if h.mode == "lost-publish" {
				w.WriteHeader(502)
				return
			}
			reply(row)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}
func active(key string) resource {
	return resource{ID: "id-" + key, Key: key, Kind: "PANEL", Status: "ACTIVE", Display: key, Revision: 3, Draft: "old", Current: "old", Number: 2}
}
func TestBatch(t *testing.T) {
	root := fixture(t)
	a := mock(t, active("alpha"), active("gamma"))
	blocked := active("beta")
	blocked.Draft = "another"
	b := mock(t, blocked)
	c := config{Panels: map[string]panel{"main": {"工作台", "工具 测试"}, "extra": {"额外", "工具 测试"}}, Environments: map[string]environment{
		"one": {URL: a.server.URL + "/prefix", Bindings: map[string]string{"main": "lighting/alpha", "extra": "lighting/gamma"}},
		"two": {URL: b.server.URL + "/prefix", Bindings: map[string]string{"main": "lighting/beta"}}}}
	raw, _ := json.Marshal(c)
	file := filepath.Join(root, "cli.json")
	put(t, file, string(raw))
	var out bytes.Buffer
	u := terminal{bufio.NewReader(strings.NewReader("1,2\n*\nadmin\nadmin\ny\n")), &out, func(terminal) (string, error) { return "password", nil }}
	err := run([]string{"publish", "--config", file, "--root", root}, u)
	if err == nil || !reflect.DeepEqual(a.writes, []string{"gamma/versions", "gamma/publish", "alpha/versions", "alpha/publish"}) || len(b.writes) != 0 || a.logouts != 1 || b.logouts != 1 {
		t.Fatalf("批次结果异常 %v %v %v\n%s", err, a.writes, b.writes, out.String())
	}
	for _, want := range []string{"two / extra 未配置", "有待发布草稿", "已发布", "/prefix/panels/lighting/alpha", "v3"} {
		if !strings.Contains(out.String(), want) {
			t.Error("结果缺少", want)
		}
	}
}
func TestEmptyDescriptionIsPreserved(t *testing.T) {
	r := active("alpha")
	h := mock(t, r)
	h.mode = "frontmatter"
	c := newClient(h.server.URL + "/prefix")
	if err := c.login("a", "p"); err != nil {
		t.Fatal(err)
	}
	got := c.publish(target{"one", "main", "lighting", "alpha"}, r, []byte("PK"))
	if got.status != "未执行" || len(h.writes) > 0 {
		t.Fatal("不应创建会改变空描述的草稿", got, h.writes)
	}
}

func TestReconciliation(t *testing.T) {
	for _, tc := range []struct {
		mode, status string
		writes       int
	}{{"lost-upload", "待核对", 1}, {"lost-publish", published, 2}, {"conflict", "仅保存草稿", 2}} {
		t.Run(tc.mode, func(t *testing.T) {
			r := active("alpha")
			h := mock(t, r)
			h.mode = tc.mode
			c := newClient(h.server.URL + "/prefix")
			if err := c.login("a", "p"); err != nil {
				t.Fatal(err)
			}
			got := c.publish(target{"one", "main", "lighting", "alpha"}, r, []byte("PK"))
			if got.status != tc.status || len(h.writes) != tc.writes {
				t.Fatal(got, h.writes)
			}
		})
	}
}
func TestNoWritesWithoutConfirmation(t *testing.T) {
	for _, cmd := range []string{"plan", "check", "publish"} {
		t.Run(cmd, func(t *testing.T) {
			root := fixture(t)
			h := mock(t, active("alpha"))
			file := filepath.Join(root, "settings.json")
			put(t, file, fmt.Sprintf(`{"panels":{"p":{"name":"P","source":"工具 测试"}},"environments":{"e":{"url":%q,"bindings":{"p":"lighting/alpha"}}}}`, h.server.URL+"/prefix"))
			var out bytes.Buffer
			u := terminal{bufio.NewReader(strings.NewReader("admin\nn\n")), &out, func(terminal) (string, error) { return "pass", nil }}
			err := run([]string{cmd, "--config", file, "--root", root, "--env", "e", "--panel", "p"}, u)
			if (cmd == "publish" && err != errCancel) || (cmd != "publish" && err != nil) || len(h.writes) != 0 {
				t.Fatal(cmd, err, h.writes)
			}
			if cmd == "plan" && h.logouts != 0 {
				t.Fatal("本地演练访问了网络")
			}
		})
	}
}

package publisher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// 模拟管理台的正式HTTP字段，检查草稿、正式版本、知识库修订与一次性确认。
// TRACE-REQ: DOM-140
func TestAdminUploads(t *testing.T) {
	writes := 0
	logouts := 0
	publishedSkill := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/admin/v1")
		var out any
		switch {
		case path == "/session" && r.Method == "POST":
			out = map[string]string{"session_token": "demo", "audience": "ADMIN"}
		case r.Header.Get("Authorization") != "Bearer demo":
			http.Error(w, "session required", 401)
			return
		case path == "/session" && r.Method == "DELETE":
			logouts++
			w.WriteHeader(204)
			return
		case path == resources+"/inspect":
			out = map[string]string{"resource_key": "reports", "display_name": "报告", "example_prompt": "整理报告"}
		case path == resources && r.Method == "GET":
			out = map[string]any{"resources": []any{}}
		case path == "/business-systems":
			out = map[string]any{"systems": []any{map[string]string{"business_system_id": "biz", "display_name": "工单", "status": "ACTIVE"}}}
		case path == resources && r.Method == "POST":
			var body struct {
				Kind   string
				File   map[string]string
				Scopes []string `json:"environment_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Kind != "SKILL" || len(body.Scopes) != 1 || body.Scopes[0] != "biz" || body.File["display_name"] != "报告" || body.File["content_base64"] != "Zml4dHVyZQ==" {
				t.Errorf("新建Skill的内容或范围有误")
			}
			writes++
			out = map[string]any{"resource_id": "rs", "draft_version_id": "dv", "revision": 1}
		case path == resources+"/rs/publish":
			publishedSkill = true
			writes++
			out = map[string]string{"resource_id": "rs"}
		case path == resources+"/rs":
			row := map[string]any{"resource_id": "rs", "resource_key": "reports", "kind": "SKILL", "draft_version_id": "dv", "revision": 1}
			if publishedSkill {
				row["current_version_id"] = "dv"
				row["current_version_number"] = 1
			}
			out = row
		case path == "/knowledge-bases":
			out = map[string]any{"items": []any{map[string]string{"knowledge_base_id": "kb", "name": "操作手册"}}}
		case path == "/knowledge-bases/kb/documents" && r.Method == "GET":
			out = map[string]any{"items": []any{map[string]any{"document_id": "doc", "knowledge_base_id": "kb", "filename": "guide.md", "revision": 3, "status": "READY"}}}
		case path == "/knowledge-bases/kb/documents/doc" && r.Method == "PUT":
			var body struct {
				Revision int `json:"expected_revision"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Revision != 3 {
				t.Errorf("修订未绑定到检查时版本")
			}
			writes++
			out = map[string]any{"knowledge_base_id": "kb", "document_id": "doc", "filename": "guide.md", "revision": 4, "published_revision": 4, "status": "READY"}
		default:
			t.Errorf("意外请求 %s %s", r.Method, path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()
	c := config{map[string]panel{"one": {"测试", "a"}}, map[string]environment{"demo": {URL: server.URL, Bindings: map[string]string{"one": "ops/one"}}}}
	w, v := desktopFixture(t, c)
	creds := Credentials{"user", "demo"}
	for _, kind := range []string{"skill", "knowledge"} {
		name := "SKILL.md"
		if kind == "knowledge" {
			name = "guide.md"
		}
		path := filepath.Join(t.TempDir(), name)
		put(t, path, "fixture")
		if _, err := w.SelectFile(kind, path); err != nil {
			t.Fatal(err)
		}
		if _, err := w.InspectUpload("stale", kind, "demo", creds); err == nil {
			t.Fatal("接受了过期配置")
		}
		checked, err := w.InspectUpload(v.Stamp, kind, "demo", creds)
		if err != nil {
			t.Fatal(err)
		}
		if kind == "skill" {
			if writes != 0 || checked.Key != "reports" || len(checked.Options) != 1 {
				t.Fatal(checked)
			}
			// 取消这次检查必须登出，旧确认不能再用。
			if err = w.Cancel(); err != nil {
				t.Fatal(err)
			}
			if _, err = w.CommitUpload(checked.ID, UploadOptions{}); err == nil {
				t.Fatal("使用了已取消的确认")
			}
			checked, err = w.InspectUpload(v.Stamp, kind, "demo", creds)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err = w.Library(checked.ID, "other"); err == nil {
				t.Fatal("接受了未列出的知识库")
			}
			checked, err = w.Library(checked.ID, "kb")
			if err != nil || !checked.Existing || checked.Revision != 3 {
				t.Fatal(checked, err)
			}
		}
		put(t, path, "changed after preview")
		res, err := w.CommitUpload(checked.ID, UploadOptions{Scope: "biz", Display: "报告", Example: "整理报告", Publish: true})
		if err != nil || res.Status != map[string]string{"skill": "已发布", "knowledge": "可检索"}[kind] {
			t.Fatal(res, err)
		}
		if _, err = w.CommitUpload(checked.ID, UploadOptions{}); err == nil {
			t.Fatal("二次提交被接受")
		}
	}
	if writes != 3 || logouts != 3 {
		t.Fatal(writes, logouts)
	}
}

func TestUploadFileLimits(t *testing.T) {
	w := NewWorkspace("", "")
	for _, c := range []struct {
		name, kind string
		size       int
		allowed    bool
	}{
		{"skill.md", "skill", 1, false}, {"SKILL.md", "skill", 2 << 20, true}, {"SKILL.md", "skill", (2 << 20) + 1, false}, {"assets.ZIP", "skill", 15 << 20, true}, {"assets.zip", "skill", (15 << 20) + 1, false}, {" guide.md", "knowledge", 1, false},
	} {
		path := filepath.Join(t.TempDir(), c.name)
		put(t, path, strings.Repeat("s", c.size))
		_, err := w.SelectFile(c.kind, path)
		if (err == nil) != c.allowed {
			t.Fatalf("%s %d: %v", c.name, c.size, err)
		}
	}
}
func TestUploadReceiptsIdentifyThisWrite(t *testing.T) {
	for _, stage := range []string{"readDraft", "readPublished", "case", "newer"} {
		t.Run(stage, func(t *testing.T) {
			details := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := strings.TrimPrefix(r.URL.Path, "/api/admin/v1")
				var out any
				switch {
				case path == resources:
					out = map[string]any{"resource_id": "r", "draft_version_id": "saved-draft", "revision": 1}
				case path == resources+"/r/publish":
					out = map[string]any{}
				case path == resources+"/r":
					details++
					if stage == "readDraft" || details == 2 {
						w.WriteHeader(403)
						return
					}
					out = map[string]any{"resource_id": "r", "kind": "SKILL", "resource_key": "work", "draft_version_id": "saved-draft", "revision": 1}
				case r.Method == "GET":
					out = map[string]any{"items": []any{map[string]any{"knowledge_base_id": "kb", "filename": "Guide.md", "document_id": "different", "revision": 2, "status": "READY"}}}
				case r.Method == "POST" && path == "/knowledge-bases/kb/documents":
					version := 1
					if stage == "newer" {
						version = 2
					}
					out = map[string]any{"knowledge_base_id": "kb", "filename": "guide.md", "document_id": "new", "revision": version, "published_revision": version, "status": "READY"}
				default:
					t.Errorf("意外的写入 %s %s", r.Method, path)
					http.NotFound(w, r)
					return
				}
				json.NewEncoder(w).Encode(out)
			}))
			defer server.Close()
			p := &uploadPlan{c: newClient(server.URL), file: uploadFile{info: UploadFile{Name: "guide.md"}, data: []byte("demo")}, review: UploadReview{ID: "checked", Key: "work", Kind: "knowledge", File: UploadFile{Name: "guide.md"}, Options: []UploadOption{{"kb", "资料库"}}}}
			if strings.HasPrefix(stage, "read") {
				result, err := p.saveSkill(UploadOptions{Scope: "*", Display: "工作", Example: "整理内容", Publish: true})
				if err != nil || result.Status != "待核对" || !strings.Contains(result.Detail, "saved-draft") {
					t.Fatal(result, err)
				}
			} else {
				w := &Workspace{upload: p}
				v, err := w.Library("checked", "kb")
				if err != nil || v.Existing {
					t.Fatal(v, err)
				}
				result, err := p.saveDocument()
				want := "可检索"
				if stage == "newer" {
					want = "待核对"
				}
				if err != nil || result.Status != want {
					t.Fatal(result, err)
				}
			}
		})
	}
}

package publisher

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedConfigEnvironmentsAndResources(t *testing.T) {
	skillStatus := "ACTIVE"
	skillRev := 2
	skillCurrent := "v1"
	skillDraft := "v2"
	skillDisplay := "值班报告"
	kbName := "操作手册"
	kbRev := 1
	docs := map[string]KnowledgeDocItem{
		"doc-1": {ID: "doc-1", LibraryID: "kb-1", Filename: "guide.md", ByteSize: 12, Status: "FAILED", ErrorCode: "PARSE_ERR", Revision: 2, PublishedRevision: 1},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/admin/v1")
		if p == "/session" && r.Method == "POST" {
			_ = json.NewEncoder(w).Encode(map[string]string{"session_token": "tok", "audience": "ADMIN"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauth", 401)
			return
		}
		if p == "/session" && r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		switch {
		case p == "/business-systems" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"systems": []map[string]string{
				{"business_system_id": "sys-1", "display_name": "业务系统A", "status": "ACTIVE"},
			}})
		case p == "/system-resources" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"resources": []map[string]any{{
				"resource_id": "sk-1", "resource_key": "duty-report", "display_name": skillDisplay,
				"description": "生成值班总结", "example_prompt": "整理今晚值班记录", "kind": "SKILL",
				"status": skillStatus, "revision": skillRev,
				"current_version_id": skillCurrent, "current_version_number": 1,
				"draft_version_id": skillDraft, "draft_version_number": 2,
				"environment_ids": []string{"sys-1"}, "environment_names": []string{"业务系统A"},
			}}})
		case p == "/system-resources/sk-1" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource_id": "sk-1", "resource_key": "duty-report", "display_name": skillDisplay,
				"description": "生成值班总结", "example_prompt": "整理今晚值班记录", "kind": "SKILL",
				"status": skillStatus, "revision": skillRev,
				"current_version_id": skillCurrent, "current_version_number": 1,
				"draft_version_id": skillDraft, "draft_version_number": 2,
				"environment_ids": []string{"sys-1"}, "environment_names": []string{"业务系统A"},
				"versions": []map[string]any{
					{"version_id": "v2", "version_number": 2, "created_at": "2026-11-24T00:00:00Z"},
					{"version_id": "v1", "version_number": 1, "created_at": "2026-11-23T00:00:00Z"},
				},
			})
		case p == "/system-resources/sk-1/versions" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version_id": r.URL.Query().Get("version"),
				"entries": []map[string]string{
					{"path": "SKILL.md", "content_base64": base64.StdEncoding.EncodeToString([]byte("# Duty Report"))},
					{"path": "scripts/run.sh", "content_base64": base64.StdEncoding.EncodeToString([]byte("echo ok"))},
				},
			})
		case p == "/system-resources/sk-1/publish" && r.Method == "POST":
			skillCurrent = skillDraft
			skillRev++
			_ = json.NewEncoder(w).Encode(map[string]any{"resource_id": "sk-1", "revision": skillRev})
		case p == "/system-resources/sk-1/disable" && r.Method == "POST":
			skillStatus = "DISABLED"
			skillRev++
			_ = json.NewEncoder(w).Encode(map[string]any{"resource_id": "sk-1", "status": skillStatus, "revision": skillRev})
		case p == "/system-resources/sk-1/enable" && r.Method == "POST":
			skillStatus = "ACTIVE"
			skillRev++
			_ = json.NewEncoder(w).Encode(map[string]any{"resource_id": "sk-1", "status": skillStatus, "revision": skillRev})
		case p == "/system-resources/sk-1/metadata" && r.Method == "PATCH":
			var body struct {
				Display string `json:"display_name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			skillDisplay = body.Display
			skillRev++
			_ = json.NewEncoder(w).Encode(map[string]any{"resource_id": "sk-1", "revision": skillRev})
		case p == "/knowledge-bases" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []KnowledgeBaseItem{
				{ID: "kb-1", Name: kbName, Description: "运维手册库", Revision: kbRev, DocumentCount: len(docs), ReadyCount: 1},
			}})
		case p == "/knowledge-bases" && r.Method == "POST":
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]bool{"saved": true})
		case p == "/knowledge-bases/kb-1" && r.Method == "PUT":
			var body struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			kbName = body.Name
			kbRev++
			_ = json.NewEncoder(w).Encode(map[string]bool{"saved": true})
		case p == "/knowledge-bases/kb-1/documents" && r.Method == "GET":
			items := make([]KnowledgeDocItem, 0, len(docs))
			for _, d := range docs {
				items = append(items, d)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case p == "/knowledge-bases/kb-1/documents/doc-1/original" && r.Method == "GET":
			_, _ = w.Write([]byte("# Original Guide"))
		case p == "/knowledge-bases/kb-1/documents/doc-1/retry" && r.Method == "POST":
			d := docs["doc-1"]
			d.Status = "READY"
			d.ErrorCode = ""
			d.PublishedRevision = d.Revision
			docs["doc-1"] = d
			_ = json.NewEncoder(w).Encode(d)
		case p == "/knowledge-bases/kb-1/documents" && r.Method == "POST":
			var body struct{ Filename string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			d := KnowledgeDocItem{ID: "doc-2", LibraryID: "kb-1", Filename: body.Filename, Status: "READY", Revision: 1, PublishedRevision: 1}
			docs["doc-2"] = d
			_ = json.NewEncoder(w).Encode(d)
		case p == "/knowledge-bases/kb-1/documents/doc-1" && r.Method == "PUT":
			d := docs["doc-1"]
			d.Revision++
			d.PublishedRevision = d.Revision
			d.Status = "READY"
			docs["doc-1"] = d
			_ = json.NewEncoder(w).Encode(d)
		case p == "/knowledge-bases/kb-1/documents/doc-2" && r.Method == "DELETE":
			delete(docs, "doc-2")
			_ = json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, p)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	managed := EnsureManagedConfig(filepath.Join(dir, "hivebridge.json"), "")
	w := NewWorkspace(managed, filepath.Join(dir, "source-root"))
	v, err := w.View()
	if err != nil || len(v.Environments) != 0 {
		t.Fatalf("empty managed config: %+v, %v", v, err)
	}

	// 1. 界面化添加环境并一步登录记住
	if err := w.SaveEnvironment(v.Stamp, "", EnvInput{
		Name: "测试环境", URL: server.URL, Tag: "test", Account: "admin", Password: "pw", Login: true,
	}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	v, err = w.View()
	if err != nil || len(v.Environments) != 1 || v.Environments[0].Name != "测试环境" || v.Environments[0].Tag != "test" || !v.Environments[0].LoggedIn {
		t.Fatalf("unexpected env view: %+v, %v", v.Environments, err)
	}

	// 2. Skill 列表、详情、下载 ZIP、发布草稿、改元信息、启停
	cat, err := w.ListSkills("测试环境")
	if err != nil || len(cat.Skills) != 1 || !cat.Skills[0].HasDraft || len(cat.Systems) != 1 {
		t.Fatalf("ListSkills: %+v, %v", cat, err)
	}
	detail, err := w.GetSkillDetail("测试环境", "sk-1")
	if err != nil || len(detail.Versions) != 2 {
		t.Fatalf("GetSkillDetail: %+v, %v", detail, err)
	}
	zipOut := filepath.Join(dir, "duty-report.zip")
	if _, err := w.DownloadSkillVersion("测试环境", "sk-1", "v2", zipOut); err != nil {
		t.Fatalf("DownloadSkillVersion: %v", err)
	}
	zr, err := zip.OpenReader(zipOut)
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("downloaded zip invalid: %v", err)
	}
	_ = zr.Close()

	if err := w.PublishSkillDraft("测试环境", "sk-1", detail.Revision); err != nil {
		t.Fatalf("PublishSkillDraft: %v", err)
	}
	if err := w.UpdateSkillMetadata("测试环境", "sk-1", SkillMetadataInput{
		Display: "值班总结增强版", Example: "帮我整理今晚值班情况", ScopeIDs: []string{"sys-1"}, ExpectedRevision: skillRev,
	}); err != nil {
		t.Fatalf("UpdateSkillMetadata: %v", err)
	}
	if err := w.SetSkillStatus("测试环境", "sk-1", skillRev, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := w.SetSkillStatus("测试环境", "sk-1", skillRev, true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// 3. 知识库列表、改名、文档列表、下载原件、重试、批量上传（新增+替换）、删除
	kbs, err := w.ListKnowledgeBases("测试环境")
	if err != nil || len(kbs) != 1 || kbs[0].Name != "操作手册" {
		t.Fatalf("ListKnowledgeBases: %+v, %v", kbs, err)
	}
	if err := w.SaveKnowledgeBase("测试环境", "kb-1", "运维操作手册", "更新说明", kbRev); err != nil {
		t.Fatalf("SaveKnowledgeBase: %v", err)
	}
	docOut := filepath.Join(dir, "guide.md")
	if _, err := w.DownloadKnowledgeDocument("测试环境", "kb-1", "doc-1", docOut); err != nil {
		t.Fatalf("DownloadKnowledgeDocument: %v", err)
	}
	if got, _ := os.ReadFile(docOut); string(got) != "# Original Guide" {
		t.Fatalf("downloaded doc mismatch: %q", string(got))
	}
	if retried, err := w.RetryKnowledgeDocument("测试环境", "kb-1", "doc-1", 2); err != nil || retried.Status != "READY" {
		t.Fatalf("RetryKnowledgeDocument: %+v, %v", retried, err)
	}
	batchRes, err := w.UploadKnowledgeBatch("测试环境", "kb-1", []BatchUploadItem{
		{Filename: "guide.md", ContentBase64: base64.StdEncoding.EncodeToString([]byte("v3"))},
		{Filename: "new-doc.md", ContentBase64: base64.StdEncoding.EncodeToString([]byte("v1"))},
	})
	if err != nil || len(batchRes) != 2 || !batchRes[0].Replaced || batchRes[0].Status != "可检索" || batchRes[1].Replaced || batchRes[1].Status != "可检索" {
		t.Fatalf("UploadKnowledgeBatch: %+v, %v", batchRes, err)
	}
	if err := w.DeleteKnowledgeDocument("测试环境", "kb-1", "doc-2", 1); err != nil {
		t.Fatalf("DeleteKnowledgeDocument: %v", err)
	}

	// 4. 导出配置与删除环境
	exported := filepath.Join(dir, "export.json")
	if err := w.ExportConfig(exported); err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if err := w.DeleteEnvironment(v.Stamp, "测试环境"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	vAfter, _ := w.View()
	if len(vAfter.Environments) != 0 {
		t.Fatalf("expected env deleted: %+v", vAfter.Environments)
	}
}

func TestSelectSkillDirPacksAndInspects(t *testing.T) {
	var inspectedFilename string
	var inspectedBytes []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/admin/v1")
		if p == "/session" && r.Method == "POST" {
			_ = json.NewEncoder(w).Encode(map[string]string{"session_token": "tok", "audience": "ADMIN"})
			return
		}
		if p == "/session" && r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		switch {
		case p == "/system-resources/inspect" && r.Method == "POST":
			var body struct {
				File struct {
					Filename string `json:"filename"`
					Content  []byte `json:"content_base64"`
				} `json:"file"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			inspectedFilename = body.File.Filename
			inspectedBytes = body.File.Content
			_ = json.NewEncoder(w).Encode(map[string]string{
				"resource_key":   "patrol-check",
				"display_name":   "巡检助手",
				"example_prompt": "生成今日巡检报告",
			})
		case p == "/system-resources" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"resources": []any{}})
		case p == "/business-systems" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"systems": []map[string]string{
				{"business_system_id": "sys-1", "display_name": "业务系统A", "status": "ACTIVE"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "patrol-check")
	put(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: patrol-check\n---\n# 巡检助手")
	put(t, filepath.Join(skillDir, "scripts", "run.sh"), "#!/bin/sh\necho ok\n")
	put(t, filepath.Join(skillDir, ".DS_Store"), "junk")
	put(t, filepath.Join(skillDir, ".git", "HEAD"), "ref: refs/heads/main\n")

	managed := EnsureManagedConfig(filepath.Join(dir, "hivebridge.json"), "")
	w := NewWorkspace(managed, filepath.Join(dir, "source-root"))
	v, _ := w.View()
	if err := w.SaveEnvironment(v.Stamp, "", EnvInput{
		Name: "开发环境", URL: server.URL, Tag: "dev", Account: "admin", Password: "pw", Login: true,
	}); err != nil {
		t.Fatal(err)
	}
	v, _ = w.View()

	// 1. 选目录自动打包 ZIP 并跳过 .DS_Store / .git
	info, err := w.SelectSkillDir(skillDir)
	if err != nil || info.Name != "patrol-check.zip" || info.Bytes == 0 {
		t.Fatalf("SelectSkillDir failed: %+v, %v", info, err)
	}
	review, err := w.InspectUpload(v.Stamp, "skill", "开发环境", Credentials{})
	if err != nil || review.Key != "patrol-check" || inspectedFilename != "patrol-check.zip" {
		t.Fatalf("InspectUpload failed: %+v, %v", review, err)
	}
	zr, err := zip.NewReader(strings.NewReader(string(inspectedBytes)), int64(len(inspectedBytes)))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("unexpected zipped skill entries: %v, len=%d", err, len(zr.File))
	}
	_ = w.Cancel()

	// 2. 缺少 SKILL.md 或包含 .env 隐藏文件应拒绝
	emptyDir := filepath.Join(dir, "no-skill-md")
	put(t, filepath.Join(emptyDir, "README.md"), "hello")
	if _, err := w.SelectSkillDir(emptyDir); err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("expected missing SKILL.md error, got %v", err)
	}
	put(t, filepath.Join(skillDir, ".env"), "SECRET=1")
	if _, err := w.SelectSkillDir(skillDir); err == nil || !strings.Contains(err.Error(), "隐藏项") {
		t.Fatalf("expected hidden file error, got %v", err)
	}

	// 3. 拖放目录入口 InspectSkillDirUpload
	rev2, err := w.InspectSkillDirUpload(v.Stamp, "开发环境", "patrol-check", []SkillDirEntry{
		{Path: "SKILL.md", ContentBase64: base64.StdEncoding.EncodeToString([]byte("# 巡检"))},
		{Path: ".DS_Store", ContentBase64: base64.StdEncoding.EncodeToString([]byte("junk"))},
		{Path: "scripts/check.py", ContentBase64: base64.StdEncoding.EncodeToString([]byte("print(1)"))},
	})
	if err != nil || rev2.Key != "patrol-check" || rev2.File.Name != "patrol-check.zip" {
		t.Fatalf("InspectSkillDirUpload failed: %+v, %v", rev2, err)
	}
	_ = w.Cancel()
}

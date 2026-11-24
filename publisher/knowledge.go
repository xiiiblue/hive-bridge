package publisher

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type KnowledgeBaseItem struct {
	ID            string `json:"knowledge_base_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Revision      int    `json:"revision"`
	DocumentCount int    `json:"document_count"`
	ReadyCount    int    `json:"ready_count"`
	GroupCount    int    `json:"group_count"`
	UpdatedAt     string `json:"updated_at"`
}

type KnowledgeDocItem struct {
	ID                string `json:"document_id"`
	LibraryID         string `json:"knowledge_base_id"`
	Filename          string `json:"filename"`
	ByteSize          int    `json:"byte_size"`
	Status            string `json:"status"`
	ErrorCode         string `json:"error_code"`
	Warning           string `json:"warning"`
	Revision          int    `json:"revision"`
	PublishedRevision int    `json:"published_revision"`
	UpdatedAt         string `json:"updated_at"`
}

type BatchUploadItem struct {
	Filename      string
	ContentBase64 string
}

type BatchUploadResult struct {
	Filename string
	Status   string
	Detail   string
	Replaced bool
	Revision int
}

func (w *Workspace) ListKnowledgeBases(env string) ([]KnowledgeBaseItem, error) {
	items := []KnowledgeBaseItem{}
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		var list struct {
			Items []KnowledgeBaseItem `json:"items"`
		}
		if err := c.call("GET", "/knowledge-bases", nil, &list); err != nil {
			return err
		}
		if list.Items != nil {
			items = list.Items
		}
		return nil
	})
	return items, err
}

func (w *Workspace) SaveKnowledgeBase(env, id, name, description string, expectedRevision int) error {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return errors.New("请输入知识库名称")
	}
	return w.withEnvClient(env, func(c *client, _ environment) error {
		body := map[string]any{
			"name":              name,
			"description":       description,
			"expected_revision": expectedRevision,
		}
		if strings.TrimSpace(id) == "" {
			body["expected_revision"] = 0
			return c.call("POST", "/knowledge-bases", body, nil)
		}
		return c.call("PUT", "/knowledge-bases/"+url.PathEscape(id), body, nil)
	})
}

func (w *Workspace) DeleteKnowledgeBase(env, id string, expectedRevision int) error {
	return w.withEnvClient(env, func(c *client, _ environment) error {
		return c.call("DELETE", "/knowledge-bases/"+url.PathEscape(id), map[string]int{
			"expected_revision": expectedRevision,
		}, nil)
	})
}

func (w *Workspace) ListKnowledgeDocuments(env, baseID string) ([]KnowledgeDocItem, error) {
	items := []KnowledgeDocItem{}
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		var list struct {
			Items []KnowledgeDocItem `json:"items"`
		}
		if err := c.call("GET", "/knowledge-bases/"+url.PathEscape(baseID)+"/documents", nil, &list); err != nil {
			return err
		}
		if list.Items != nil {
			items = list.Items
		}
		return nil
	})
	return items, err
}

func (w *Workspace) DownloadKnowledgeDocument(env, baseID, docID, savePath string) (string, error) {
	if strings.TrimSpace(savePath) == "" {
		return "", errors.New("未指定保存路径")
	}
	var summary string
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		raw, err := c.callRaw("GET", "/knowledge-bases/"+url.PathEscape(baseID)+"/documents/"+url.PathEscape(docID)+"/original")
		if err != nil {
			return err
		}
		if err := os.WriteFile(savePath, raw, 0644); err != nil {
			return err
		}
		summary = fmt.Sprintf("已保存到 %s（%d 字节）", filepath.Base(savePath), len(raw))
		return nil
	})
	return summary, err
}

func (w *Workspace) RetryKnowledgeDocument(env, baseID, docID string, expectedRevision int) (KnowledgeDocItem, error) {
	var out KnowledgeDocItem
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		return c.call("POST", "/knowledge-bases/"+url.PathEscape(baseID)+"/documents/"+url.PathEscape(docID)+"/retry", map[string]int{
			"expected_revision": expectedRevision,
		}, &out)
	})
	return out, err
}

func (w *Workspace) DeleteKnowledgeDocument(env, baseID, docID string, expectedRevision int) error {
	return w.withEnvClient(env, func(c *client, _ environment) error {
		return c.call("DELETE", "/knowledge-bases/"+url.PathEscape(baseID)+"/documents/"+url.PathEscape(docID), map[string]int{
			"expected_revision": expectedRevision,
		}, nil)
	})
}

func (w *Workspace) UploadKnowledgeBatch(env, baseID string, files []BatchUploadItem) ([]BatchUploadResult, error) {
	if len(files) == 0 {
		return nil, errors.New("请至少选择一个文档")
	}
	results := make([]BatchUploadResult, 0, len(files))
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		var list struct {
			Items []KnowledgeDocItem `json:"items"`
		}
		if err := c.call("GET", "/knowledge-bases/"+url.PathEscape(baseID)+"/documents", nil, &list); err != nil {
			return err
		}
		byName := map[string]KnowledgeDocItem{}
		for _, d := range list.Items {
			byName[d.Filename] = d
		}
		for _, item := range files {
			name := filepath.Base(strings.TrimSpace(item.Filename))
			ext := strings.ToLower(filepath.Ext(name))
			if name == "" || name == "." || (ext != ".md" && ext != ".docx" && ext != ".pdf") {
				results = append(results, BatchUploadResult{Filename: item.Filename, Status: "上传失败", Detail: "仅支持 Markdown、DOCX 或 PDF 文件"})
				continue
			}
			raw, decErr := base64.StdEncoding.DecodeString(item.ContentBase64)
			if decErr != nil || len(raw) < 1 || len(raw) > 10<<20 {
				results = append(results, BatchUploadResult{Filename: name, Status: "上传失败", Detail: "文件内容无效或超过 10 MiB 上限"})
				continue
			}
			existing, exists := byName[name]
			if exists && existing.Status == "PROCESSING" {
				results = append(results, BatchUploadResult{Filename: name, Status: "未执行", Detail: "同名文档正在处理中，请稍后重试", Replaced: true})
				continue
			}
			method := "POST"
			path := "/knowledge-bases/" + url.PathEscape(baseID) + "/documents"
			expectedRev := 0
			if exists {
				method = "PUT"
				path += "/" + url.PathEscape(existing.ID)
				expectedRev = existing.Revision
			}
			var saved KnowledgeDocItem
			callErr := c.call(method, path, map[string]any{
				"filename":          name,
				"content_base64":    item.ContentBase64,
				"expected_revision": expectedRev,
			}, &saved)
			if callErr != nil {
				r := uncertain(callErr)
				results = append(results, BatchUploadResult{Filename: name, Status: r.Status, Detail: r.Detail, Replaced: exists})
				continue
			}
			byName[name] = saved
			switch saved.Status {
			case "READY":
				results = append(results, BatchUploadResult{
					Filename: name,
					Status:   "可检索",
					Detail:   fmt.Sprintf("修订 r%d 已处理完成", saved.Revision),
					Replaced: exists,
					Revision: saved.Revision,
				})
			case "PROCESSING":
				results = append(results, BatchUploadResult{
					Filename: name,
					Status:   "处理中",
					Detail:   "文件已上传，服务端正在转换",
					Replaced: exists,
					Revision: saved.Revision,
				})
			case "FAILED":
				results = append(results, BatchUploadResult{
					Filename: name,
					Status:   "处理失败",
					Detail:   "转换失败：" + saved.ErrorCode,
					Replaced: exists,
					Revision: saved.Revision,
				})
			default:
				results = append(results, BatchUploadResult{
					Filename: name,
					Status:   "待核对",
					Detail:   "已收到回执，请刷新确认状态",
					Replaced: exists,
					Revision: saved.Revision,
				})
			}
		}
		return nil
	})
	return results, err
}

package publisher

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type SkillItem struct {
	ID               string
	Key              string
	Display          string
	Description      string
	Example          string
	Status           string
	Revision         int
	CurrentVersionID string
	CurrentNumber    int
	DraftVersionID   string
	DraftNumber      int
	HasDraft         bool
	Tags             []string
	ScopeIDs         []string
	ScopeNames       []string
	UpdatedAt        string
}

type SkillVersion struct {
	VersionID     string
	VersionNumber int
	CreatedAt     string
}

type SkillDetail struct {
	SkillItem
	Versions []SkillVersion
}

type SkillCatalog struct {
	Env     string
	URL     string
	Skills  []SkillItem
	Systems []UploadOption
}

type SkillMetadataInput struct {
	Display          string
	Example          string
	ScopeIDs         []string
	Tags             []string
	ExpectedRevision int
}

type rawSkillRow struct {
	ID               string   `json:"resource_id"`
	Key              string   `json:"resource_key"`
	Display          string   `json:"display_name"`
	Description      string   `json:"description"`
	Example          string   `json:"example_prompt"`
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	Revision         int      `json:"revision"`
	CurrentVersionID string   `json:"current_version_id"`
	CurrentNumber    int      `json:"current_version_number"`
	DraftVersionID   string   `json:"draft_version_id"`
	DraftNumber      int      `json:"draft_version_number"`
	Tags             []string `json:"tags"`
	ScopeIDs         []string `json:"environment_ids"`
	ScopeNames       []string `json:"environment_names"`
	UpdatedAt        string   `json:"updated_at"`
}

func (r rawSkillRow) toItem() SkillItem {
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	scopeIDs := r.ScopeIDs
	if scopeIDs == nil {
		scopeIDs = []string{}
	}
	scopeNames := r.ScopeNames
	if scopeNames == nil {
		scopeNames = []string{}
	}
	return SkillItem{
		ID:               r.ID,
		Key:              r.Key,
		Display:          r.Display,
		Description:      r.Description,
		Example:          r.Example,
		Status:           r.Status,
		Revision:         r.Revision,
		CurrentVersionID: r.CurrentVersionID,
		CurrentNumber:    r.CurrentNumber,
		DraftVersionID:   r.DraftVersionID,
		DraftNumber:      r.DraftNumber,
		HasDraft:         r.DraftVersionID != "" && r.DraftVersionID != r.CurrentVersionID,
		Tags:             tags,
		ScopeIDs:         scopeIDs,
		ScopeNames:       scopeNames,
		UpdatedAt:        r.UpdatedAt,
	}
}

func (w *Workspace) ListSkills(env string) (SkillCatalog, error) {
	out := SkillCatalog{Env: env, Skills: []SkillItem{}, Systems: []UploadOption{}}
	err := w.withEnvClient(env, func(c *client, e environment) error {
		out.URL = e.URL
		var listed struct {
			Resources []rawSkillRow `json:"resources"`
		}
		if err := c.call("GET", resources+"?kind=SKILL", nil, &listed); err != nil {
			return err
		}
		for _, r := range listed.Resources {
			if r.Kind == "" || r.Kind == "SKILL" {
				out.Skills = append(out.Skills, r.toItem())
			}
		}
		var sysList struct {
			Systems []struct {
				ID     string `json:"business_system_id"`
				Name   string `json:"display_name"`
				Status string `json:"status"`
			} `json:"systems"`
		}
		if err := c.call("GET", "/business-systems", nil, &sysList); err != nil {
			return err
		}
		for _, s := range sysList.Systems {
			if s.Status == "ACTIVE" && s.ID != "" {
				out.Systems = append(out.Systems, UploadOption{ID: s.ID, Name: s.Name})
			}
		}
		return nil
	})
	return out, err
}

func (w *Workspace) GetSkillDetail(env, resourceID string) (SkillDetail, error) {
	var out SkillDetail
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		var raw struct {
			rawSkillRow
			Versions []struct {
				VersionID     string `json:"version_id"`
				VersionNumber int    `json:"version_number"`
				CreatedAt     string `json:"created_at"`
			} `json:"versions"`
		}
		if err := c.call("GET", resources+"/"+url.PathEscape(resourceID), nil, &raw); err != nil {
			return err
		}
		out.SkillItem = raw.rawSkillRow.toItem()
		out.Versions = make([]SkillVersion, 0, len(raw.Versions))
		for _, v := range raw.Versions {
			out.Versions = append(out.Versions, SkillVersion{
				VersionID:     v.VersionID,
				VersionNumber: v.VersionNumber,
				CreatedAt:     v.CreatedAt,
			})
		}
		return nil
	})
	return out, err
}

func (w *Workspace) DownloadSkillVersion(env, resourceID, versionID, savePath string) (string, error) {
	if strings.TrimSpace(savePath) == "" {
		return "", errors.New("未指定保存路径")
	}
	var savedSummary string
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		var payload struct {
			Entries []struct {
				Path          string `json:"path"`
				ContentBase64 string `json:"content_base64"`
			} `json:"entries"`
		}
		path := resources + "/" + url.PathEscape(resourceID) + "/versions?version=" + url.QueryEscape(versionID)
		if err := c.call("GET", path, nil, &payload); err != nil {
			return err
		}
		if len(payload.Entries) == 0 {
			return errors.New("该版本没有可下载的文件内容")
		}
		// 如果仅含单个 SKILL.md 且目标文件名以 .md 结尾，直接保存为 Markdown 文件；否则打包为标准 ZIP。
		if strings.EqualFold(filepath.Ext(savePath), ".md") && len(payload.Entries) == 1 && payload.Entries[0].Path == "SKILL.md" {
			raw, err := base64.StdEncoding.DecodeString(payload.Entries[0].ContentBase64)
			if err != nil {
				return fmt.Errorf("解码文件内容失败：%w", err)
			}
			if err := os.WriteFile(savePath, raw, 0644); err != nil {
				return err
			}
			savedSummary = fmt.Sprintf("已保存到 %s（%d 字节）", filepath.Base(savePath), len(raw))
			return nil
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, entry := range payload.Entries {
			clean := filepath.ToSlash(strings.TrimLeft(entry.Path, "/"))
			if clean == "" || strings.Contains(clean, "..") {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(entry.ContentBase64)
			if err != nil {
				return fmt.Errorf("解码 %s 失败：%w", clean, err)
			}
			fw, err := zw.Create(clean)
			if err != nil {
				return err
			}
			if _, err := fw.Write(raw); err != nil {
				return err
			}
		}
		if err := zw.Close(); err != nil {
			return err
		}
		if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
			return err
		}
		savedSummary = fmt.Sprintf("已保存到 %s（%d 个文件）", filepath.Base(savePath), len(payload.Entries))
		return nil
	})
	return savedSummary, err
}

func (w *Workspace) PublishSkillDraft(env, resourceID string, expectedRevision int) error {
	return w.withEnvClient(env, func(c *client, _ environment) error {
		return c.call("POST", resources+"/"+url.PathEscape(resourceID)+"/publish", map[string]int{
			"expected_revision": expectedRevision,
		}, nil)
	})
}

func (w *Workspace) SetSkillStatus(env, resourceID string, expectedRevision int, enable bool) error {
	action := "disable"
	if enable {
		action = "enable"
	}
	return w.withEnvClient(env, func(c *client, _ environment) error {
		return c.call("POST", resources+"/"+url.PathEscape(resourceID)+"/"+action, map[string]int{
			"expected_revision": expectedRevision,
		}, nil)
	})
}

func (w *Workspace) UpdateSkillMetadata(env, resourceID string, input SkillMetadataInput) error {
	display := strings.TrimSpace(input.Display)
	example := strings.TrimSpace(input.Example)
	if display == "" || example == "" {
		return errors.New("请填写显示名称和示例提问")
	}
	scopeIDs := input.ScopeIDs
	if scopeIDs == nil {
		scopeIDs = []string{}
	}
	tags := input.Tags
	if tags == nil {
		tags = []string{}
	}
	return w.withEnvClient(env, func(c *client, _ environment) error {
		return c.call("PATCH", resources+"/"+url.PathEscape(resourceID)+"/metadata", map[string]any{
			"environment_ids":   scopeIDs,
			"display_name":      display,
			"example_prompt":    example,
			"tags":              tags,
			"expected_revision": input.ExpectedRevision,
		}, nil)
	})
}

// InspectSkillUpload 支持点击选择或直接拖拽上传的 Skill 文件检查，结果随后交由 CommitUpload 提交。
func (w *Workspace) InspectSkillUpload(stamp, env, filename, contentBase64 string) (v UploadReview, err error) {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || strings.TrimSpace(filename) != filename {
		return v, errors.New("无效的文件名")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if name != "SKILL.md" && ext != ".zip" {
		return v, errors.New("请选择 SKILL.md 或技能 ZIP 包")
	}
	data, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return v, errors.New("读取文件内容失败")
	}
	limit := 2 << 20
	if ext == ".zip" {
		limit = 15 << 20
	}
	if len(data) < 1 || len(data) > limit {
		return v, fmt.Errorf("文件大小需在 1 字节至 %d MiB 之间", limit>>20)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.discard(); err != nil {
		return v, err
	}
	cfg, current, err := w.read()
	if err != nil {
		return v, err
	}
	e, ok := cfg.Environments[env]
	if current != stamp || !ok {
		return v, errChanged
	}
	cred, found := w.creds.get(e.URL)
	if !found {
		return v, fmt.Errorf("请先登录环境 %s", env)
	}
	w.file = uploadFile{kind: "skill", info: UploadFile{Name: name, Bytes: len(data)}, data: data}
	c := newClient(e.URL)
	defer func() {
		if err != nil {
			err = errors.Join(err, c.logout())
		}
	}()
	if err = c.login(cred.Name, cred.Password); err != nil {
		return v, err
	}
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	p := &uploadPlan{
		c:      c,
		stamp:  stamp,
		file:   w.file,
		review: UploadReview{ID: hex.EncodeToString(nonce[:]), Env: env, Kind: "skill", File: w.file.info, Options: []UploadOption{}},
	}
	if err = p.inspectSkill(); err != nil {
		return v, err
	}
	w.upload = p
	return p.review, nil
}

const (
	maxSkillZipBytes   = 15 << 20
	maxSkillTotalBytes = 30 << 20
	maxSkillFileCount  = 512
)

type SkillDirEntry struct {
	Path          string
	ContentBase64 string
}

func isIgnoredSkillEntry(name string, isDir bool) bool {
	if isDir {
		return name == ".git" || name == "__MACOSX"
	}
	return name == ".DS_Store" || strings.EqualFold(name, "Thumbs.db")
}

func validateSkillRelPath(rel string) error {
	if rel == "" || rel == "." || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return fmt.Errorf("无效的文件路径：%s", rel)
	}
	if strings.ContainsAny(rel, "\\:") || strings.IndexFunc(rel, unicode.IsControl) >= 0 || !utf8.ValidString(rel) || len([]rune(rel)) > 512 {
		return fmt.Errorf("路径包含非法字符或过长：%s", rel)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return fmt.Errorf("目录中包含隐藏项或无效路径：%s", rel)
		}
	}
	return nil
}

func skillZipFilename(dirName string) string {
	base := strings.TrimSpace(filepath.Base(strings.TrimSpace(dirName)))
	if base == "" || base == "." || base == "/" || base == `\` {
		return "skill.zip"
	}
	if strings.EqualFold(filepath.Ext(base), ".zip") {
		return base
	}
	return base + ".zip"
}

func packSkillDir(dirPath string) (string, []byte, error) {
	abs, err := filepath.Abs(strings.TrimSpace(dirPath))
	if err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("请选择有效的 Skill 文件夹（不能是文件或符号链接）")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return "", nil, err
	}
	defer root.Close()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := map[string]bool{}
	total, count := 0, 0
	hasSkillMD := false

	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			return nil
		}
		if isIgnoredSkillEntry(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Skill 目录中不能包含符号链接：%s", rel)
		}
		if err := validateSkillRelPath(rel); err != nil {
			return err
		}
		key := strings.ToLower(rel)
		if _, seen := names[key]; seen {
			return fmt.Errorf("目录中存在大小写重复的路径：%s", rel)
		}
		names[key] = true
		if d.IsDir() {
			return nil
		}
		finfo, err := root.Lstat(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		if !finfo.Mode().IsRegular() || finfo.Size() > maxFile {
			return fmt.Errorf("非常规文件或单文件超过 10 MiB：%s", rel)
		}
		count++
		if count > maxSkillFileCount {
			return fmt.Errorf("文件数量超过 %d 个上限", maxSkillFileCount)
		}
		f, err := root.Open(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
		f.Close()
		if err != nil {
			return err
		}
		if len(b) > maxFile {
			return fmt.Errorf("单文件超过 10 MiB：%s", rel)
		}
		if key == "skill.md" {
			if rel != "SKILL.md" || len(bytes.TrimSpace(b)) == 0 || !utf8.Valid(b) {
				return errors.New("目录根目录下的 SKILL.md 须为非空 UTF-8 文件")
			}
			hasSkillMD = true
		}
		total += len(b)
		if total > maxSkillTotalBytes {
			return errors.New("Skill 目录总内容超过 30 MiB 上限")
		}
		h := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		if finfo.Mode().Perm()&0111 != 0 {
			h.SetMode(0755)
		} else {
			h.SetMode(0644)
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = fw.Write(b)
		return err
	})
	if err != nil {
		return "", nil, err
	}
	if !hasSkillMD {
		return "", nil, errors.New("所选目录根部缺少 SKILL.md，请确认选择的是具体 Skill 文件夹")
	}
	if err := zw.Close(); err != nil {
		return "", nil, err
	}
	if buf.Len() == 0 || buf.Len() > maxSkillZipBytes {
		return "", nil, errors.New("打包后的 Skill ZIP 大小超过 15 MiB 上限")
	}
	return skillZipFilename(abs), buf.Bytes(), nil
}

// SelectSkillDir 将本地 Skill 目录自动打包为内存 ZIP，供随后的 InspectUpload / CommitUpload 复用。
func (w *Workspace) SelectSkillDir(dirPath string) (UploadFile, error) {
	zipName, data, err := packSkillDir(dirPath)
	if err != nil {
		return UploadFile{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.discard(); err != nil {
		return UploadFile{}, err
	}
	w.file = uploadFile{kind: "skill", info: UploadFile{Name: zipName, Bytes: len(data)}, data: data}
	return w.file.info, nil
}

// InspectSkillDirUpload 支持拖放 Skill 文件夹：将前端读取到的目录文件列表在服务端安全校验、打 ZIP 并检查。
func (w *Workspace) InspectSkillDirUpload(stamp, env, dirName string, entries []SkillDirEntry) (v UploadReview, err error) {
	if len(entries) == 0 {
		return v, errors.New("拖入的目录为空")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := map[string]bool{}
	total, count := 0, 0
	hasSkillMD := false

	for _, entry := range entries {
		rel := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(entry.Path)), "./")
		if rel == "" {
			continue
		}
		segs := strings.Split(rel, "/")
		ignored := false
		for i, seg := range segs {
			if isIgnoredSkillEntry(seg, i < len(segs)-1) {
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}
		if err := validateSkillRelPath(rel); err != nil {
			return v, err
		}
		key := strings.ToLower(rel)
		if _, seen := names[key]; seen {
			return v, fmt.Errorf("目录中存在大小写重复的路径：%s", rel)
		}
		names[key] = true
		count++
		if count > maxSkillFileCount {
			return v, fmt.Errorf("文件数量超过 %d 个上限", maxSkillFileCount)
		}
		b, err := base64.StdEncoding.DecodeString(entry.ContentBase64)
		if err != nil {
			return v, fmt.Errorf("读取 %s 失败", rel)
		}
		if len(b) > maxFile {
			return v, fmt.Errorf("单文件超过 10 MiB：%s", rel)
		}
		if key == "skill.md" {
			if rel != "SKILL.md" || len(bytes.TrimSpace(b)) == 0 || !utf8.Valid(b) {
				return v, errors.New("目录根目录下的 SKILL.md 须为非空 UTF-8 文件")
			}
			hasSkillMD = true
		}
		total += len(b)
		if total > maxSkillTotalBytes {
			return v, errors.New("Skill 目录总内容超过 30 MiB 上限")
		}
		h := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		h.SetMode(0644)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return v, err
		}
		if _, err := fw.Write(b); err != nil {
			return v, err
		}
	}
	if !hasSkillMD {
		return v, errors.New("拖入的目录根部缺少 SKILL.md，请确认拖入的是具体 Skill 文件夹")
	}
	if err := zw.Close(); err != nil {
		return v, err
	}
	if buf.Len() == 0 || buf.Len() > maxSkillZipBytes {
		return v, errors.New("打包后的 Skill ZIP 大小超过 15 MiB 上限")
	}
	return w.InspectSkillUpload(stamp, env, skillZipFilename(dirName), base64.StdEncoding.EncodeToString(buf.Bytes()))
}

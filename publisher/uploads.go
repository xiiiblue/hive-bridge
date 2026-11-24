package publisher

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type UploadFile struct {
	Name  string
	Bytes int
}
type uploadFile struct {
	kind string
	info UploadFile
	data []byte
}
type UploadOption struct{ ID, Name string }
type UploadReview struct {
	ID, Env, Kind, Key, Display, Example, Target, TargetID string
	File                                                   UploadFile
	Options                                                []UploadOption
	Existing                                               bool
	Revision                                               int
}
type UploadOptions struct {
	Scope, Display, Example string
	Publish                 bool
}
type UploadReceipt struct{ Status, Detail, Warning string }
type uploadPlan struct {
	review   UploadReview
	c        *client
	stamp    string
	previous resource
	doc      document
	file     uploadFile
}
type document struct {
	ID                string `json:"document_id"`
	Library           string `json:"knowledge_base_id"`
	Name              string `json:"filename"`
	Revision          int
	Status            string
	PublishedRevision int    `json:"published_revision"`
	Error             string `json:"error_code"`
}

func (w *Workspace) SelectFile(kind, path string) (UploadFile, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	name := filepath.Base(path)
	if strings.TrimSpace(name) != name {
		return UploadFile{}, errors.New("请先去掉文件名首尾的空白")
	}
	limit := int64(10 << 20)
	ext := strings.ToLower(filepath.Ext(name))
	switch kind {
	case "skill":
		if name != "SKILL.md" && ext != ".zip" {
			return UploadFile{}, errors.New("请选择SKILL.md或技能ZIP包")
		}
		limit = 2 << 20
		if ext == ".zip" {
			limit = 15 << 20
		}
	case "knowledge":
		if ext != ".md" && ext != ".docx" && ext != ".pdf" {
			return UploadFile{}, errors.New("请选择Markdown、DOCX或PDF文件")
		}
	default:
		return UploadFile{}, errors.New("未知上传类型")
	}
	f, err := os.Open(path)
	if err != nil {
		return UploadFile{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return UploadFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return UploadFile{}, fmt.Errorf("请选择有效文件，大小上限为%d MiB", limit>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return UploadFile{}, err
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return UploadFile{}, errors.New("文件大小已变化，请重选")
	}
	if err = w.discard(); err != nil {
		return UploadFile{}, err
	}
	w.file = uploadFile{kind, UploadFile{name, len(data)}, data}
	return w.file.info, nil
}
func (p *uploadPlan) payload() map[string]any {
	return map[string]any{"filename": p.file.info.Name, "content_base64": p.file.data}
}
func (w *Workspace) InspectUpload(stamp, kind, env string, cred Credentials) (v UploadReview, err error) {
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
	if w.file.kind != kind || len(w.file.data) == 0 {
		return v, errors.New("请先选择要上传的文件")
	}
	if strings.TrimSpace(cred.Name) == "" || cred.Password == "" {
		if saved, found := w.creds.get(e.URL); found {
			cred = saved
		} else {
			return v, fmt.Errorf("请先登录环境 %s", env)
		}
	}
	c := newClient(e.URL)
	defer func() {
		if err != nil {
			err = errors.Join(err, c.logout())
		}
	}()
	if err = c.login(strings.TrimSpace(cred.Name), cred.Password); err != nil {
		return v, err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	p := &uploadPlan{c: c, stamp: stamp, file: w.file, review: UploadReview{ID: hex.EncodeToString(nonce[:]), Env: env, Kind: kind, File: w.file.info, Options: []UploadOption{}}}
	if kind == "skill" {
		err = p.inspectSkill()
	} else {
		err = p.libraries()
	}
	if err != nil {
		return v, err
	}
	w.upload = p
	return p.review, nil
}
func (p *uploadPlan) inspectSkill() error {
	var meta struct {
		Key     string `json:"resource_key"`
		Display string `json:"display_name"`
		Example string `json:"example_prompt"`
	}
	if err := p.c.call("POST", resources+"/inspect", map[string]any{"kind": "SKILL", "file": p.payload()}, &meta); err != nil {
		return err
	}
	if !aliasPattern.MatchString(meta.Key) {
		return errors.New("服务端未识别有效的Skill标识")
	}
	v := &p.review
	v.Key, v.Display, v.Example = meta.Key, meta.Display, meta.Example
	var listed struct{ Resources []resource }
	if err := p.c.call("GET", resources+"?kind=SKILL", nil, &listed); err != nil {
		return err
	}
	for _, r := range listed.Resources {
		if r.Kind != "SKILL" || r.Key != meta.Key {
			continue
		}
		if p.previous.ID != "" || r.ID == "" || r.Revision < 1 || r.Status != "ACTIVE" {
			return errors.New("同名Skill已停用或目标无法唯一确定")
		}
		if r.Draft != "" && r.Draft != r.Current {
			return errors.New("此Skill有待发布草稿，请先在管理台核对")
		}
		p.previous = r
		v.Existing = true
		v.Target = r.Display
		v.TargetID = r.ID
		v.Revision = r.Revision
	}
	if v.Existing {
		return nil
	}
	var list struct {
		Systems []struct {
			ID     string `json:"business_system_id"`
			Name   string `json:"display_name"`
			Status string
		}
	}
	if err := p.c.call("GET", "/business-systems", nil, &list); err != nil {
		return err
	}
	for _, s := range list.Systems {
		if s.Status == "ACTIVE" && s.ID != "" {
			v.Options = append(v.Options, UploadOption{s.ID, s.Name})
		}
	}
	return nil
}
func (p *uploadPlan) libraries() error {
	var list struct {
		Items []struct {
			ID   string `json:"knowledge_base_id"`
			Name string
		}
	}
	if err := p.c.call("GET", "/knowledge-bases", nil, &list); err != nil {
		return err
	}
	for _, lib := range list.Items {
		if lib.ID != "" {
			p.review.Options = append(p.review.Options, UploadOption{lib.ID, lib.Name})
		}
	}
	if len(p.review.Options) == 0 {
		return errors.New("此环境还没有知识库，请在管理台建立后重试")
	}
	return nil
}
func optionName(items []UploadOption, id string) (string, bool) {
	for _, item := range items {
		if item.ID == id {
			return item.Name, true
		}
	}
	return "", false
}
func (w *Workspace) Library(id, library string) (UploadReview, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.upload
	if p == nil || p.review.ID != id || p.review.Kind != "knowledge" {
		return UploadReview{}, errors.New("请重新检查文件")
	}
	p.review.TargetID = ""
	p.review.Existing = false
	p.review.Revision = 0
	p.doc = document{}
	name, ok := optionName(p.review.Options, library)
	if !ok {
		return UploadReview{}, errors.New("请选择当前环境的知识库")
	}
	var list struct{ Items []document }
	if err := p.c.call("GET", "/knowledge-bases/"+url.PathEscape(library)+"/documents", nil, &list); err != nil {
		return UploadReview{}, err
	}
	for _, d := range list.Items {
		if d.Name != p.file.info.Name {
			continue
		}
		if d.Library != library || d.ID == "" || d.Revision < 1 || d.Status == "PROCESSING" || p.doc.ID != "" {
			return UploadReview{}, errors.New("同名文档正在处理或目标不明确，请在管理台核对")
		}
		p.doc = d
		p.review.Existing = true
		p.review.Revision = d.Revision
	}
	p.review.TargetID = library
	p.review.Target = name
	return p.review, nil
}
func (w *Workspace) CommitUpload(id string, opts UploadOptions) (receipt UploadReceipt, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.upload
	if p == nil || p.review.ID != id || id == "" {
		return receipt, errors.New("检查已失效，请重新检查文件")
	}
	w.upload = nil
	defer func() {
		if p.c.logout() != nil {
			receipt.Warning = "管理员会话的登出回执异常"
		}
	}()
	_, stamp, err := w.read()
	if err != nil || stamp != p.stamp {
		return receipt, errors.Join(errChanged, err)
	}
	if p.review.Kind == "skill" {
		return p.saveSkill(opts)
	}
	return p.saveDocument()
}
func uncertain(err error) UploadReceipt {
	if definiteRejection(err) {
		return UploadReceipt{Status: "上传失败", Detail: err.Error()}
	}
	return UploadReceipt{Status: "待核对", Detail: "未取得可靠回执，请到所选环境的管理台核对后再重试。 " + err.Error()}
}
func verifyDraft(id string, err error) UploadReceipt {
	return UploadReceipt{Status: "待核对", Detail: fmt.Sprintf("已取得草稿编号 %s，未确认最终状态。请在管理台核对：%v", id, err)}
}
func (p *uploadPlan) saveSkill(o UploadOptions) (UploadReceipt, error) {
	v := p.review
	file := p.payload()
	path := resources
	body := map[string]any{"kind": "SKILL", "file": file}
	if v.Existing {
		path += "/" + url.PathEscape(v.TargetID) + "/versions"
		body["expected_revision"] = v.Revision
	} else {
		if o.Scope != "*" {
			if _, ok := optionName(v.Options, o.Scope); !ok {
				return UploadReceipt{}, errors.New("请选择适用的业务系统")
			}
			body["environment_ids"] = []string{o.Scope}
		} else {
			body["environment_ids"] = []string{}
		}
		if strings.TrimSpace(o.Display) == "" || strings.TrimSpace(o.Example) == "" {
			return UploadReceipt{}, errors.New("请填写显示名称和示例提问")
		}
		file["display_name"] = strings.TrimSpace(o.Display)
		file["example_prompt"] = strings.TrimSpace(o.Example)
	}
	var saved resource
	if err := p.c.call("POST", path, body, &saved); err != nil {
		return uncertain(err), nil
	}
	if saved.ID == "" || saved.Draft == "" || saved.Revision != v.Revision+1 || (v.Existing && saved.ID != v.TargetID) {
		return uncertain(errors.New("草稿回执与目标不符")), nil
	}
	now, err := p.c.detail(saved.ID)
	if err != nil {
		return verifyDraft(saved.Draft, err), nil
	}
	if now.Kind != "SKILL" || now.Key != v.Key || now.Draft != saved.Draft || now.Revision != saved.Revision {
		return verifyDraft(saved.Draft, errors.New("草稿读回与本次上传不符")), nil
	}
	if !o.Publish {
		return UploadReceipt{Status: "已保存草稿", Detail: v.Key + " · 草稿 " + saved.Draft}, nil
	}
	pubErr := p.c.call("POST", resources+"/"+url.PathEscape(saved.ID)+"/publish", map[string]int{"expected_revision": saved.Revision}, nil)
	now, err = p.c.detail(saved.ID)
	if err != nil {
		return verifyDraft(saved.Draft, err), nil
	}
	if now.Kind == "SKILL" && now.Key == v.Key && now.Current == saved.Draft {
		return UploadReceipt{Status: "已发布", Detail: fmt.Sprintf("%s · 正式版本 v%d", v.Key, now.Number)}, nil
	}
	if definiteRejection(pubErr) && now.Draft == saved.Draft {
		return UploadReceipt{Status: "仅保存草稿", Detail: pubErr.Error()}, nil
	}
	return verifyDraft(saved.Draft, errors.New("正式版本尚未确认")), nil
}
func (p *uploadPlan) saveDocument() (UploadReceipt, error) {
	v := p.review
	if v.TargetID == "" {
		return UploadReceipt{}, errors.New("请先核对目标知识库")
	}
	path := "/knowledge-bases/" + url.PathEscape(v.TargetID) + "/documents"
	method := "POST"
	body := p.payload()
	body["expected_revision"] = v.Revision
	if v.Existing {
		method = "PUT"
		path += "/" + url.PathEscape(p.doc.ID)
	}
	var d document
	if err := p.c.call(method, path, body, &d); err != nil {
		return uncertain(err), nil
	}
	if d.Library != v.TargetID || d.ID == "" || d.Name != v.File.Name || d.Revision != v.Revision+1 || (v.Existing && d.ID != p.doc.ID) {
		return uncertain(errors.New("知识库文档回执与本次上传不符")), nil
	}
	switch d.Status {
	case "READY":
		if d.PublishedRevision == d.Revision {
			return UploadReceipt{Status: "可检索", Detail: fmt.Sprintf("%s · %s · 版本 %d", v.Target, d.Name, d.Revision)}, nil
		}
	case "PROCESSING":
		return UploadReceipt{Status: "处理中", Detail: "文件已接收，请在知识库查看处理结果。"}, nil
	case "FAILED":
		return UploadReceipt{Status: "处理失败", Detail: "文件已接收，转换失败。请在知识库核对：" + d.Error}, nil
	}
	return uncertain(errors.New("尚未确认可检索版本")), nil
}

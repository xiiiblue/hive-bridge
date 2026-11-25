package publisher

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const resources = "/system-resources"

type client struct {
	base, token string
	http        *http.Client
}

func newClient(base string) *client {
	return &client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type httpError struct {
	status int
	code   string
}

func (e *httpError) Error() string {
	label := map[string]string{
		"SYSTEM_RESOURCE_REVISION_CONFLICT": "面板已被其他操作更新，请先核对最新版本",
		"SYSTEM_RESOURCE_NOT_FOUND":         "目标面板缺失或已停用",
		"PLATFORM_ADMIN_REQUIRED":           "当前账号缺少平台管理员权限",
		"LOGIN_FAILED":                      "请核对这个环境的管理员账号和密码",
	}[e.code]
	return strings.TrimSpace(fmt.Sprintf("HTTP %d %s %s", e.status, e.code, label))
}
func definiteRejection(err error) bool {
	var e *httpError
	return errors.As(err, &e) && e.status >= 400 && e.status < 500 && e.status != 408
}

func (c *client) call(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+"/api/admin/v1"+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return err
		}
		req.Header.Set("x-request-id", hex.EncodeToString(id[:]))
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var v struct{ Error struct{ Code string } }
		_ = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&v)
		return &httpError{res.StatusCode, v.Error.Code}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out)
}
func (c *client) callRaw(method, path string) ([]byte, error) {
	req, err := http.NewRequest(method, c.base+"/api/admin/v1"+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var v struct{ Error struct{ Code string } }
		_ = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&v)
		return nil, &httpError{res.StatusCode, v.Error.Code}
	}
	return io.ReadAll(io.LimitReader(res.Body, 32<<20))
}
func (c *client) login(name, pass string) error {
	var v struct {
		Token    string `json:"session_token"`
		Change   bool   `json:"password_change_required"`
		Audience string
	}
	if err := c.call("POST", "/session", map[string]string{"login_name": name, "password": pass}, &v); err != nil {
		return err
	}
	c.token = v.Token
	if v.Token == "" || v.Audience != "ADMIN" {
		return errors.New("没有收到有效管理员会话")
	}
	if v.Change {
		return errors.New("请先到管理台修改初始密码")
	}
	return nil
}
func (c *client) logout() error {
	if c.token == "" {
		return nil
	}
	defer func() { c.token = "" }()
	return c.call("DELETE", "/session", nil, nil)
}

type resource struct {
	ID                        string `json:"resource_id"`
	Key                       string `json:"resource_key"`
	Display                   string `json:"display_name"`
	Kind, Status, Description string
	Revision                  int
	Draft                     string `json:"draft_version_id"`
	Current                   string `json:"current_version_id"`
	Number                    int    `json:"current_version_number"`
}
type catalog struct {
	systems map[string]string
	panels  map[string][]resource
}

func (c *client) directory() (catalog, error) {
	d := catalog{map[string]string{}, map[string][]resource{}}
	var listing struct {
		Systems []struct {
			ID     string `json:"business_system_id"`
			Key    string `json:"system_key"`
			Status string
		}
	}
	err := c.call("GET", "/business-systems", nil, &listing)
	if err != nil {
		return d, err
	}
	for _, s := range listing.Systems {
		if s.Status == "ACTIVE" {
			d.systems[s.Key] = s.ID
		}
	}
	return d, nil
}
func (c *client) resolve(d catalog, t target) (resource, error) {
	sys := d.systems[t.System]
	if sys == "" {
		return resource{}, fmt.Errorf("业务系统 %s 缺失或停用", t.System)
	}
	rows, ok := d.panels[sys]
	if !ok {
		var result struct{ Resources []resource }
		if err := c.call("GET", resources+"?kind=PANEL&system="+url.QueryEscape(sys), nil, &result); err != nil {
			return resource{}, err
		}
		rows = result.Resources
		d.panels[sys] = rows
	}
	for _, r := range rows {
		if r.Key != t.Key || r.Kind != "PANEL" {
			continue
		}
		if r.ID == "" || r.Revision <= 0 || r.Status != "ACTIVE" {
			return resource{}, errors.New("面板已停用或返回信息不完整")
		}
		if r.Draft != "" && r.Draft != r.Current {
			return resource{}, errors.New("有待发布草稿，请先在管理台处理")
		}
		return r, nil
	}
	return resource{}, fmt.Errorf("系统 %s 没有配置指定的面板 %s", t.System, t.Key)
}
func (c *client) detail(id string) (resource, error) {
	var r resource
	err := c.call("GET", resources+"/"+url.PathEscape(id), nil, &r)
	if err == nil && r.ID != id {
		err = errors.New("读回的资源编号不符")
	}
	return r, err
}

type result struct{ status, detail string }

const published = "已发布"

func (c *client) publish(t target, r resource, pack []byte) result {
	path := resources + "/" + url.PathEscape(r.ID)
	var saved resource
	file := map[string]any{"filename": t.Key + ".zip", "content_base64": pack, "name": r.Key, "display_name": r.Display, "description": r.Description}
	// Core may fall back to PANEL.md when an empty description is supplied.
	if r.Description == "" {
		var inspected struct {
			Description *string
			Key         string `json:"resource_key"`
		}
		err := c.call("POST", resources+"/inspect", map[string]any{"kind": "PANEL", "file": file}, &inspected)
		if err != nil {
			return result{"未执行", "核对包的面板信息失败：" + err.Error()}
		}
		if inspected.Key != r.Key || inspected.Description == nil || *inspected.Description != "" {
			return result{"未执行", "包内面板信息会改变现有资源的空描述或标识，请核对PANEL.md开头的说明字段"}
		}
	}
	err := c.call("POST", path+"/versions", map[string]any{"kind": "PANEL", "expected_revision": r.Revision, "file": file}, &saved)
	if err == nil && (saved.ID != r.ID || saved.Draft == "" || saved.Revision <= r.Revision) {
		err = errors.New("草稿回执无效")
	}
	if err != nil {
		if definiteRejection(err) {
			return result{"上传失败", err.Error()}
		}
		now, readErr := c.detail(r.ID)
		if readErr != nil {
			return result{"待核对", fmt.Sprintf("上传未取得可靠回执（%v），读回失败（%v）", err, readErr)}
		}
		return result{"待核对", fmt.Sprintf("上传未取得可靠回执（%v）。读回正式版v%d，修订%d，草稿编号 %q；请在管理台核对", err, now.Number, now.Revision, now.Draft)}
	}
	var receipt resource
	publishErr := c.call("POST", path+"/publish", map[string]int{"expected_revision": saved.Revision}, &receipt)
	if publishErr == nil && (receipt.ID != saved.ID || receipt.Current != saved.Draft) {
		publishErr = errors.New("发布回执与本次草稿不符")
	}
	now, readErr := c.detail(r.ID)
	if readErr != nil {
		check := "版本核对失败"
		var networkErr net.Error
		if errors.As(readErr, &networkErr) && networkErr.Timeout() {
			check = "版本核对超时"
		}
		switch {
		case publishErr == nil:
			return result{published, fmt.Sprintf("服务器已确认发布。%s（%v）。", check, readErr)}
		case definiteRejection(publishErr):
			return result{"发布失败", fmt.Sprintf("草稿 %s 已保存。服务器拒绝本次发布（%v）。%s（%v）。", saved.Draft, publishErr, check, readErr)}
		default:
			return result{"发布待确认", fmt.Sprintf("草稿 %s 已保存，发布结果暂未确认。请先在管理台核对再重试。发布回执：%v。%s（%v）。", saved.Draft, publishErr, check, readErr)}
		}
	}
	if now.Current == saved.Draft {
		return result{published, fmt.Sprintf("v%d  %s/panels/%s/%s", now.Number, c.base, t.System, t.Key)}
	}
	if now.Draft == saved.Draft && definiteRejection(publishErr) {
		return result{"仅保存草稿", fmt.Sprintf("%s，发布失败（%v），请在管理台处理", saved.Draft, publishErr)}
	}
	return result{"待核对", fmt.Sprintf("本次草稿 %s，读回正式版v%d，尚未确认生效。请在管理台核对", saved.Draft, now.Number)}
}

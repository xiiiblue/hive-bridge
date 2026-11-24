package publisher

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type PanelRow struct {
	Alias, Name, Source string
	Bindings            map[string]string
}
type PanelInput struct {
	Alias, Name, Source, Binding string
	ApplyAllEnvs                 bool
}
type RemotePanelOption struct {
	SystemKey   string
	SystemName  string
	ResourceKey string
	DisplayName string
	Binding     string
	Version     int
}
type EnvironmentRow struct {
	Name, URL, Tag string
	Order          int
	LoggedIn       bool
	Account        string
}
type EnvInput struct {
	Name, URL, Tag, Account, Password string
	Order                             int
	Login                             bool
}
type View struct {
	ConfigPath, Root, Stamp string
	Panels                  []PanelRow
	Environments            []EnvironmentRow
}

type Workspace struct {
	mu                   sync.Mutex
	path, root, settings string
	creds                *credentialStore
	checked              *checkedBatch
	file                 uploadFile
	upload               *uploadPlan
}

func NewWorkspace(configPath, rootSettings string) *Workspace {
	w := &Workspace{path: configPath, settings: rootSettings, creds: newCredentialStore(rootSettings)}
	if b, err := os.ReadFile(rootSettings); err == nil {
		w.root, _ = validRoot(string(b))
	}
	return w
}
func (w *Workspace) read() (config, string, error) {
	data, err := os.ReadFile(w.path)
	if err != nil {
		return config{}, "", err
	}
	c, err := decodeConfig(bytes.NewReader(data))
	return c, fmt.Sprintf("%x", sha256.Sum256(append([]byte(w.path+"\x00"+w.root+"\x00"), data...))), err
}
func (w *Workspace) snapshot() (View, error) {
	v := View{ConfigPath: w.path, Root: w.root}
	c, stamp, err := w.read()
	if err != nil {
		return v, err
	}
	v.Stamp = stamp
	v.Panels = make([]PanelRow, 0, len(c.Panels))
	v.Environments = make([]EnvironmentRow, 0, len(c.Environments))
	for _, env := range sortedEnvNames(c.Environments) {
		e := c.Environments[env]
		row := EnvironmentRow{Name: env, URL: e.URL, Tag: e.Tag, Order: e.Order}
		if saved, ok := w.creds.get(e.URL); ok {
			row.LoggedIn = true
			row.Account = saved.Name
		}
		v.Environments = append(v.Environments, row)
	}
	for alias, p := range c.Panels {
		b := map[string]string{}
		for env, e := range c.Environments {
			if key, ok := e.Bindings[alias]; ok {
				b[env] = key
			}
		}
		v.Panels = append(v.Panels, PanelRow{alias, p.Name, p.Source, b})
	}
	sort.Slice(v.Panels, func(i, j int) bool {
		a, b := v.Panels[i], v.Panels[j]
		if (a.Source == "") != (b.Source == "") {
			return a.Source != ""
		}
		return a.Alias < b.Alias
	})
	return v, nil
}

// EnsureManagedConfig 将配置托管在用户目录中；首次启动自动从随包配置初始化（无随包配置时生成空配置），不再要求程序目录可写。
func EnsureManagedConfig(managedPath, seedPath string) string {
	if managedPath == "" {
		return seedPath
	}
	if _, err := os.Stat(managedPath); err == nil {
		return managedPath
	}
	var initData []byte
	if seedPath != "" {
		if raw, err := os.ReadFile(seedPath); err == nil {
			if _, err := decodeConfig(bytes.NewReader(raw)); err == nil {
				initData = raw
			}
		}
	}
	if len(initData) == 0 {
		initData = []byte("{\n  \"panels\": {},\n  \"environments\": {}\n}\n")
	}
	if err := writeConfigFile(managedPath, initData); err != nil {
		return seedPath
	}
	return managedPath
}

func (w *Workspace) ImportConfig(srcPath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, err := readConfig(srcPath)
	if err != nil {
		return err
	}
	if err := w.discard(); err != nil {
		return err
	}
	if c.Panels == nil {
		c.Panels = map[string]panel{}
	}
	if c.Environments == nil {
		c.Environments = map[string]environment{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(w.path, append(data, '\n'))
}

func (w *Workspace) ExportConfig(destPath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, _, err := w.read()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(destPath, append(data, '\n'))
}

func (w *Workspace) SaveEnvironment(stamp, previousName string, in EnvInput) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	if c.Panels == nil {
		c.Panels = map[string]panel{}
	}
	if c.Environments == nil {
		c.Environments = map[string]environment{}
	}
	name := strings.TrimSpace(in.Name)
	rawURL := strings.TrimRight(strings.TrimSpace(in.URL), "/")
	tag := strings.TrimSpace(in.Tag)
	if !validEnvName(name) {
		return errors.New("请输入有效的环境名称（1-48字，不含斜杠）")
	}
	if !validEnvTag(tag) {
		return errors.New("环境类型仅支持开发、测试或正式")
	}
	var bindings map[string]string
	var oldURL string
	order := in.Order
	if previousName != "" {
		old, ok := c.Environments[previousName]
		if !ok {
			return fmt.Errorf("环境 %q 不存在", previousName)
		}
		bindings = old.Bindings
		oldURL = old.URL
		if order <= 0 {
			order = old.Order
		}
		if previousName != name {
			if _, exists := c.Environments[name]; exists {
				return fmt.Errorf("已存在名为 %q 的环境", name)
			}
			delete(c.Environments, previousName)
		}
	} else {
		if _, exists := c.Environments[name]; exists {
			return fmt.Errorf("已存在名为 %q 的环境", name)
		}
		if order <= 0 {
			order = nextEnvOrder(c.Environments)
		}
	}
	c.Environments[name] = environment{URL: rawURL, Bindings: bindings, Tag: tag, Order: order}
	if err := c.validate(); err != nil {
		return err
	}
	if in.Login || (strings.TrimSpace(in.Account) != "" && in.Password != "") {
		acc := strings.TrimSpace(in.Account)
		if acc == "" || in.Password == "" {
			return errors.New("请填写管理员账号和密码")
		}
		cli := newClient(rawURL)
		if err := cli.login(acc, in.Password); err != nil {
			return err
		}
		_ = cli.logout()
		if err := w.creds.set(rawURL, Credentials{Name: acc, Password: in.Password}); err != nil {
			return fmt.Errorf("保存本地凭据失败：%w", err)
		}
	}
	if oldURL != "" && normalizeOrigin(oldURL) != normalizeOrigin(rawURL) {
		_ = w.creds.delete(oldURL)
	}
	if err := w.discard(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(w.path, append(data, '\n'))
}

func (w *Workspace) DeleteEnvironment(stamp, name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	target, ok := c.Environments[name]
	if !ok {
		return fmt.Errorf("环境 %q 不存在", name)
	}
	delete(c.Environments, name)
	if err := w.discard(); err != nil {
		return err
	}
	_ = w.creds.delete(target.URL)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(w.path, append(data, '\n'))
}

// ReorderEnvironments 按给定完整顺序更新所有环境的排序值（从 1 开始连续编号）。
func (w *Workspace) ReorderEnvironments(stamp string, names []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	if len(names) != len(c.Environments) {
		return errors.New("环境排序列表数量与当前配置不一致")
	}
	seen := make(map[string]bool, len(names))
	for i, name := range names {
		e, ok := c.Environments[name]
		if !ok || seen[name] {
			return fmt.Errorf("环境排序列表包含无效或重复项 %q", name)
		}
		seen[name] = true
		e.Order = i + 1
		c.Environments[name] = e
	}
	if err := w.discard(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(w.path, append(data, '\n'))
}

// ListRemotePanels 从已登录环境实时拉取启用的业务系统及业务面板清单，供界面选择目标资源键或一键同步。
func (w *Workspace) ListRemotePanels(env string) ([]RemotePanelOption, error) {
	out := []RemotePanelOption{}
	err := w.withEnvClient(env, func(c *client, _ environment) error {
		var listing struct {
			Systems []struct {
				ID     string `json:"business_system_id"`
				Key    string `json:"system_key"`
				Name   string `json:"display_name"`
				Status string `json:"status"`
			} `json:"systems"`
		}
		if err := c.call("GET", "/business-systems", nil, &listing); err != nil {
			return err
		}
		for _, s := range listing.Systems {
			if s.Status != "ACTIVE" || s.ID == "" || !systemPattern.MatchString(s.Key) {
				continue
			}
			var res struct {
				Resources []resource `json:"resources"`
			}
			if err := c.call("GET", resources+"?kind=PANEL&system="+url.QueryEscape(s.ID), nil, &res); err != nil {
				return err
			}
			for _, r := range res.Resources {
				if r.Kind != "PANEL" || r.Status != "ACTIVE" || !aliasPattern.MatchString(r.Key) {
					continue
				}
				display := strings.TrimSpace(r.Display)
				if display == "" {
					display = r.Key
				}
				out = append(out, RemotePanelOption{
					SystemKey:   s.Key,
					SystemName:  s.Name,
					ResourceKey: r.Key,
					DisplayName: display,
					Binding:     s.Key + "/" + r.Key,
					Version:     r.Number,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SystemKey != out[j].SystemKey {
			return out[i].SystemKey < out[j].SystemKey
		}
		return out[i].ResourceKey < out[j].ResourceKey
	})
	return out, nil
}

// SyncRemotePanels 拉取当前环境的远端面板，并将尚未映射的面板及目标资源键补充到本地配置。
func (w *Workspace) SyncRemotePanels(stamp, env string) (int, error) {
	remotes, err := w.ListRemotePanels(env)
	if err != nil {
		return 0, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return 0, err
	}
	if current != stamp {
		return 0, errChanged
	}
	e, ok := c.Environments[env]
	if !ok {
		return 0, fmt.Errorf("未知环境 %q", env)
	}
	if c.Panels == nil {
		c.Panels = map[string]panel{}
	}
	if e.Bindings == nil {
		e.Bindings = map[string]string{}
	}
	boundTargets := map[string]bool{}
	for _, b := range e.Bindings {
		boundTargets[b] = true
	}
	added := 0
	for _, rp := range remotes {
		if boundTargets[rp.Binding] {
			continue
		}
		alias := rp.ResourceKey
		if _, exists := c.Panels[alias]; exists {
			if _, bound := e.Bindings[alias]; !bound {
				e.Bindings[alias] = rp.Binding
				boundTargets[rp.Binding] = true
				added++
				continue
			}
			alt := rp.SystemKey + "-" + rp.ResourceKey
			if _, existsAlt := c.Panels[alt]; !existsAlt && aliasPattern.MatchString(alt) {
				alias = alt
			} else {
				continue
			}
		}
		c.Panels[alias] = panel{Name: rp.DisplayName, Source: ""}
		e.Bindings[alias] = rp.Binding
		boundTargets[rp.Binding] = true
		added++
	}
	if added == 0 {
		return 0, nil
	}
	c.Environments[env] = e
	if err := c.validate(); err != nil {
		return 0, err
	}
	if err := w.discard(); err != nil {
		return 0, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := writeConfigFile(w.path, append(data, '\n')); err != nil {
		return 0, err
	}
	return added, nil
}

// SavePanel 新增或编辑面板信息，以及该面板在指定环境下的目标资源键绑定。
func (w *Workspace) SavePanel(stamp, previousAlias, env string, in PanelInput) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	if c.Panels == nil {
		c.Panels = map[string]panel{}
	}
	if c.Environments == nil {
		c.Environments = map[string]environment{}
	}
	name := strings.TrimSpace(in.Name)
	source := strings.TrimSpace(in.Source)
	if source != "" && !isAbsPanelPath(source) {
		source = filepath.ToSlash(source)
	}
	binding := strings.TrimSpace(in.Binding)
	if name == "" {
		return errors.New("请填写面板名称")
	}
	alias := strings.TrimSpace(in.Alias)
	if previousAlias != "" {
		if _, exists := c.Panels[previousAlias]; !exists {
			return fmt.Errorf("面板 %q 不存在", previousAlias)
		}
		alias = previousAlias
	} else {
		if alias == "" && binding != "" {
			if _, key, ok := strings.Cut(binding, "/"); ok {
				alias = key
			}
		}
		if !aliasPattern.MatchString(alias) {
			return errors.New("请填写有效的目标资源键（如 system/panel-key）或面板标识")
		}
		if _, exists := c.Panels[alias]; exists {
			return fmt.Errorf("已存在标识为 %q 的面板", alias)
		}
	}
	c.Panels[alias] = panel{Name: name, Source: source}
	if in.ApplyAllEnvs && binding != "" {
		for envName, e := range c.Environments {
			if e.Bindings == nil {
				e.Bindings = map[string]string{}
			}
			e.Bindings[alias] = binding
			c.Environments[envName] = e
		}
	} else if env != "" {
		e, ok := c.Environments[env]
		if !ok {
			return fmt.Errorf("未知环境 %q", env)
		}
		if e.Bindings == nil {
			e.Bindings = map[string]string{}
		}
		if binding == "" {
			delete(e.Bindings, alias)
		} else {
			e.Bindings[alias] = binding
		}
		c.Environments[env] = e
	}
	if err := c.validate(); err != nil {
		return err
	}
	if err := w.discard(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(w.path, append(data, '\n'))
}

// DeletePanel 删除面板并同步清理所有环境中指向该面板的绑定。
func (w *Workspace) DeletePanel(stamp, alias string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	if _, exists := c.Panels[alias]; !exists {
		return fmt.Errorf("面板 %q 不存在", alias)
	}
	delete(c.Panels, alias)
	for envName, e := range c.Environments {
		if len(e.Bindings) > 0 {
			delete(e.Bindings, alias)
			c.Environments[envName] = e
		}
	}
	if err := w.discard(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(w.path, append(data, '\n'))
}

func (w *Workspace) withEnvClient(env string, fn func(*client, environment) error) error {
	w.mu.Lock()
	c, _, err := w.read()
	if err != nil {
		w.mu.Unlock()
		return err
	}
	e, ok := c.Environments[env]
	if !ok {
		w.mu.Unlock()
		return fmt.Errorf("未知环境 %q", env)
	}
	cred, found := w.creds.get(e.URL)
	w.mu.Unlock()
	if !found {
		return fmt.Errorf("请先登录环境 %s", env)
	}
	cli := newClient(e.URL)
	if err := cli.login(cred.Name, cred.Password); err != nil {
		return err
	}
	defer func() { _ = cli.logout() }()
	return fn(cli, e)
}

func writeConfigFile(path string, data []byte) error {
	if _, err := os.Lstat(path); err == nil {
		return replaceConfig(path, data)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hive-panel-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0644); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (w *Workspace) View() (View, error) { w.mu.Lock(); defer w.mu.Unlock(); return w.snapshot() }
func (w *Workspace) Login(stamp, env string, cred Credentials) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	e, ok := c.Environments[env]
	if !ok {
		return fmt.Errorf("未知环境 %q", env)
	}
	name := strings.TrimSpace(cred.Name)
	if name == "" || cred.Password == "" {
		return errors.New("请填写管理员账号和密码")
	}
	cli := newClient(e.URL)
	if err := cli.login(name, cred.Password); err != nil {
		return err
	}
	_ = cli.logout()
	if err := w.creds.set(e.URL, Credentials{Name: name, Password: cred.Password}); err != nil {
		return fmt.Errorf("保存本地凭据失败：%w", err)
	}
	return nil
}
func (w *Workspace) Logout(stamp, env string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	e, ok := c.Environments[env]
	if !ok {
		return fmt.Errorf("未知环境 %q", env)
	}
	if err := w.discard(); err != nil {
		return err
	}
	return w.creds.delete(e.URL)
}
func (w *Workspace) SetConfig(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := readConfig(path); err != nil {
		return err
	}
	if err := w.discard(); err != nil {
		return err
	}
	w.path = path
	return nil
}
func (w *Workspace) SetRoot(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	root, err := validRoot(dir)
	if err != nil {
		return err
	}
	if err = w.discard(); err != nil {
		return err
	}
	if err = rememberRoot(w.settings, root); err == nil {
		w.root = root
	}
	return err
}

var errChanged = errors.New("配置或源码根目录已变化，请刷新后重选")

func (w *Workspace) Bind(stamp, alias, dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, current, err := w.read()
	if err != nil {
		return err
	}
	if current != stamp {
		return errChanged
	}
	absDir, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return err
	}
	view, err := w.snapshot()
	if err != nil {
		return err
	}
	for _, row := range view.Panels {
		if row.Alias != alias {
			continue
		}
		source := absDir
		if w.root != "" {
			if rel, relErr := filepath.Rel(w.root, absDir); relErr == nil && rel != "." && filepath.IsLocal(rel) {
				source = filepath.ToSlash(rel)
			}
		}
		p := panel{row.Name, source}
		if _, err := makePackage(w.root, p); err != nil {
			return fmt.Errorf("目录未通过打包检查：%w", err)
		}
		c.Panels[alias] = p
		for env, binding := range row.Bindings {
			e := c.Environments[env]
			if e.Bindings == nil {
				e.Bindings = map[string]string{}
			}
			e.Bindings[alias] = binding
			c.Environments[env] = e
		}
		if err := c.validate(); err != nil {
			return err
		}
		if err := w.discard(); err != nil {
			return err
		}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return err
		}
		_, latest, err := w.read()
		if err != nil {
			return err
		}
		if stamp != latest {
			return errChanged
		}
		return replaceConfig(w.path, append(data, '\n'))
	}
	return errors.New("面板不在当前配置或已核对清单中")
}
func replaceConfig(path string, data []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("配置应为普通文件")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hive-panel-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

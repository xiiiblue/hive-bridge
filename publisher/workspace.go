package publisher

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	if w.root == "" {
		return errors.New("请先选择源码根目录")
	}
	view, err := w.snapshot()
	if err != nil {
		return err
	}
	for _, row := range view.Panels {
		if row.Alias != alias {
			continue
		}
		rel, err := filepath.Rel(w.root, dir)
		if err != nil {
			return err
		}
		if rel == "." || !filepath.IsLocal(rel) {
			return errors.New("请选择源码根目录内的面板子目录")
		}
		p := panel{row.Name, filepath.ToSlash(rel)}
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

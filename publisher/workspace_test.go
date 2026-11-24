package publisher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func desktopFixture(t *testing.T, c config) (*Workspace, View) {
	t.Helper()
	root := fixture(t)
	location := filepath.Join(t.TempDir(), "hive-panel.json")
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	put(t, location, string(data))
	w := NewWorkspace(location, filepath.Join(t.TempDir(), "hive-panel", "source-root"))
	if err := w.SetRoot(root); err != nil {
		t.Fatal(err)
	}
	v, err := w.View()
	if err != nil {
		t.Fatal(err)
	}
	return w, v
}

// TRACE-REQ: DOM-140
func TestDesktopIndividualTargetsAndConfirmation(t *testing.T) {
	a, b := mock(t, active("alpha"), active("beta")), mock(t, active("only-b"))
	c := config{map[string]panel{"one": {"面板一", "工具 测试"}, "two": {"面板二", "工具 测试"}}, map[string]environment{
		"first":  {URL: a.server.URL + "/prefix", Bindings: map[string]string{"one": "lighting/alpha", "two": "lighting/beta"}},
		"second": {URL: b.server.URL + "/prefix", Bindings: map[string]string{"two": "lighting/only-b"}},
	}}
	w, v := desktopFixture(t, c)
	chosen := []Selection{{"first", "one"}, {"second", "two"}}
	noop := func(string) {}
	preview, err := w.Prepare(v.Stamp, chosen, nil, false, noop)
	if err != nil || preview.ID != "" || len(preview.Results) != 2 {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	creds := map[string]Credentials{"first": {"a", "p"}, "second": {"b", "q"}}
	checked, err := w.Prepare(v.Stamp, chosen, creds, true, noop)
	if err != nil || checked.ID == "" || len(a.writes)+len(b.writes) != 0 {
		t.Fatalf("check: %+v, %v", checked, err)
	}
	if _, err = w.Publish("unconfirmed", noop); err == nil {
		t.Fatal("unexpected confirmation")
	}
	b.mode = "conflict"
	done, err := w.Publish(checked.ID, noop)
	if err != nil {
		t.Fatal(err)
	}
	if len(done.Results) != 2 || done.Results[0].Status != published || done.Results[1].Status != "仅保存草稿" || a.writes[0] != "alpha/versions" || b.writes[0] != "only-b/versions" {
		t.Fatal(done)
	}
	if _, err := w.Publish(checked.ID, noop); err == nil || len(a.writes) != 2 || len(b.writes) != 2 {
		t.Fatal("duplicate writes")
	}
	if a.logouts != 1 || b.logouts != 1 {
		t.Fatal("sessions leaked")
	}
	checked, err = w.Prepare(v.Stamp, chosen[:1], creds, true, noop)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(v.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("\n")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Publish(checked.ID, noop); err == nil || len(a.writes) != 2 || a.logouts != 2 {
		t.Fatal("config change did not invalidate check")
	}
}
func TestDesktopBindsPendingPanel(t *testing.T) {
	c := config{
		Panels: map[string]panel{
			"one":     {"原面板", "somewhere"},
			"pending": {"待配目录面板", ""},
		},
		Environments: map[string]environment{
			"dev":     {URL: "https://dev.example.com", Tag: "dev", Order: 1, Bindings: map[string]string{"one": "ops/original", "pending": "ops/pending"}},
			"staging": {URL: "https://staging.example.com", Tag: "test", Order: 2, Bindings: map[string]string{"one": "ops/different"}},
		},
	}
	w, v := desktopFixture(t, c)
	if len(v.Panels) != 2 {
		t.Fatal(v.Panels)
	}
	if err := w.Bind(v.Stamp, "pending", t.TempDir()); err == nil {
		t.Fatal("outside root accepted")
	}
	if err := w.Bind(v.Stamp, "pending", filepath.Join(v.Root, "missing")); err == nil {
		t.Fatal("missing package accepted")
	}
	if err := w.Bind(v.Stamp, "pending", filepath.Join(v.Root, "工具 测试")); err != nil {
		t.Fatal(err)
	}
	got, err := readConfig(v.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Panels["pending"].Source != "工具 测试" || got.Environments["dev"].Bindings["pending"] != "ops/pending" || got.Environments["staging"].Bindings["pending"] != "" {
		t.Fatal(got)
	}
	if err := w.Bind(v.Stamp, "one", filepath.Join(v.Root, "工具 测试")); err == nil || !strings.Contains(err.Error(), "刷新") {
		t.Fatal(err)
	}
	reopened := NewWorkspace(v.ConfigPath, w.settings)
	next, err := reopened.View()
	if err != nil || next.Root != v.Root || len(next.Panels) != 2 {
		t.Fatal(next, err)
	}
}

func TestUpfrontLoginPersistsEncryptedCredentials(t *testing.T) {
	srv := mock(t, active("alpha"))
	c := config{
		map[string]panel{"one": {"面板一", "工具 测试"}},
		map[string]environment{"dev": {URL: srv.server.URL + "/prefix", Bindings: map[string]string{"one": "lighting/alpha"}}},
	}
	w, v := desktopFixture(t, c)
	if v.Environments[0].LoggedIn || v.Environments[0].Account != "" {
		t.Fatalf("expected initially logged out: %+v", v.Environments[0])
	}
	if err := w.Login(v.Stamp, "dev", Credentials{Name: "admin", Password: "secret-pass-value"}); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	// 登录校验完成后应及时登出验证会话，不留悬挂会话。
	if srv.logouts != 1 {
		t.Fatalf("expected validation session logout, got %d", srv.logouts)
	}
	// 验证磁盘文件不含明文密码。
	encPath := filepath.Join(filepath.Dir(w.settings), "credentials.enc")
	raw, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("encrypted credentials file missing: %v", err)
	}
	if strings.Contains(string(raw), "secret-pass-value") {
		t.Fatal("plaintext password leaked into credentials.enc")
	}
	// 模拟重启应用：使用同一设置目录重新创建 Workspace，应保持已登录状态。
	reopened := NewWorkspace(v.ConfigPath, w.settings)
	v2, err := reopened.View()
	if err != nil {
		t.Fatal(err)
	}
	if !v2.Environments[0].LoggedIn || v2.Environments[0].Account != "admin" {
		t.Fatalf("expected persisted login after reopen: %+v", v2.Environments[0])
	}
	// 无需再次传入 creds，直接检查并发布成功。
	noop := func(string) {}
	checked, err := reopened.Prepare(v2.Stamp, []Selection{{"dev", "one"}}, nil, true, noop)
	if err != nil || checked.ID == "" || len(checked.Results) != 1 || checked.Results[0].Status != "就绪" {
		t.Fatalf("prepare with saved credentials failed: %+v, %v", checked, err)
	}
	done, err := reopened.Publish(checked.ID, noop)
	if err != nil || done.Results[0].Status != published {
		t.Fatalf("publish with saved credentials failed: %+v, %v", done, err)
	}
	// 退出登录后不再记住凭据。
	if err := reopened.Logout(v2.Stamp, "dev"); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	afterLogout := NewWorkspace(v.ConfigPath, w.settings)
	v3, err := afterLogout.View()
	if err != nil {
		t.Fatal(err)
	}
	if v3.Environments[0].LoggedIn || v3.Environments[0].Account != "" {
		t.Fatalf("expected logged out after Logout: %+v", v3.Environments[0])
	}
	res, err := afterLogout.Prepare(v3.Stamp, []Selection{{"dev", "one"}}, nil, true, noop)
	if err != nil || res.ID != "" || res.Results[0].Status != "未执行" {
		t.Fatalf("expected unauthenticated check to fail cleanly: %+v, %v", res, err)
	}
}

func TestExampleConfigOrderAndReorder(t *testing.T) {
	// 1. 验证仓库示例配置格式有效且按 order 正确排序
	examplePath := filepath.Join("..", "hivebridge.example.json")
	example, err := readConfig(examplePath)
	if err != nil {
		t.Fatalf("read hivebridge.example.json: %v", err)
	}
	gotOrder := sortedEnvNames(example.Environments)
	wantOrder := []string{"开发环境", "测试环境", "生产环境"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("example env order = %v, want %v", gotOrder, wantOrder)
	}

	// 2. 验证 EnsureManagedConfig 从示例配置初始化并支持界面重排顺序
	dir := t.TempDir()
	managed := EnsureManagedConfig(filepath.Join(dir, "hivebridge.json"), examplePath)
	w := NewWorkspace(managed, filepath.Join(dir, "source-root"))
	v, err := w.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	reordered := []string{"开发环境", "生产环境", "测试环境"}
	if err := w.ReorderEnvironments(v.Stamp, reordered); err != nil {
		t.Fatalf("ReorderEnvironments: %v", err)
	}
	v2, err := w.View()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range v2.Environments {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != strings.Join(reordered, ",") {
		t.Fatalf("reordered env order = %v, want %v", names, reordered)
	}
}

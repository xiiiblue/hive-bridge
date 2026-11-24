package publisher

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type terminal struct {
	in           *bufio.Reader
	out          io.Writer
	readPassword func(terminal) (string, error)
}

func (u terminal) line(prompt string) (string, error) {
	fmt.Fprint(u.out, prompt)
	s, err := u.in.ReadString('\n')
	if err != nil && len(s) == 0 {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

type values []string

func (v *values) String() string { return strings.Join(*v, ",") }
func (v *values) Set(s string) error {
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			return errors.New("选项不能为空")
		}
		if !slices.Contains(*v, k) {
			*v = append(*v, k)
		}
	}
	return nil
}
func (u terminal) choose(title string, items []string, label func(string) string) ([]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("%s：没有可选项", title)
	}
	fmt.Fprintln(u.out, title)
	for i, k := range items {
		fmt.Fprintf(u.out, "  %d) %s\n", i+1, label(k))
	}
	for {
		s, err := u.line("输入编号，多选用逗号分隔，* 全选，q 退出：")
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "q" {
			return nil, errCancel
		}
		if s == "*" {
			return items, nil
		}
		var out []string
		for _, val := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '，' }) {
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > len(items) {
				out = nil
				break
			}
			if !slices.Contains(out, items[n-1]) {
				out = append(out, items[n-1])
			}
		}
		if len(out) > 0 {
			return out, nil
		}
		fmt.Fprintln(u.out, "编号无效，请按列表重新选择。")
	}
}

var errCancel = errors.New("已取消")

func validRoot(p string) (string, error) {
	if p == "" {
		return "", errors.New("请选择源码根目录")
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	i, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !i.IsDir() {
		return "", errors.New("请选择目录")
	}
	return p, nil
}
func getRoot(u terminal, override string) (string, error) {
	if override != "" {
		return validRoot(override)
	}
	home, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	state := filepath.Join(home, "hive-panel", "source-root")
	b, err := os.ReadFile(state)
	if err == nil {
		if p, e := validRoot(string(b)); e == nil {
			return p, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	s, err := u.line("业务面板仓库的本机路径（可拖入目录）：")
	if err != nil {
		return "", err
	}
	s = strings.Trim(strings.TrimSpace(s), "\"'")
	if s == "" {
		return "", errors.New("仓库路径不能为空")
	}
	root, err := validRoot(s)
	if err != nil {
		return "", err
	}
	return root, rememberRoot(state, root)
}
func rememberRoot(state, root string) error {
	if err := os.MkdirAll(filepath.Dir(state), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(state), "source-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(root)
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), state)
	}
	return err
}
func usage(out io.Writer) {
	fmt.Fprint(out, `hive-panel — 蜂巢业务面板批量发布

hive-panel                         中文菜单
hive-panel plan    [选项]           本地打包演练，无网络请求
hive-panel check   [选项]           登录并核对目标与草稿，仅读面板
hive-panel publish [选项]           确认后上传、发布并核对版本

--env 环境       多选可重复，也可用逗号（例如 开发环境,测试环境）
--panel 面板     面板别名，多选方式同上
--root 目录      本次使用的业务仓库根目录
--config 文件    默认读取可执行文件旁的hive-panel.json
--yes            明确给出环境和面板时跳过最后确认，仍需本人登录
`)
}

type item struct {
	t   target
	r   resource
	res result
}
type session struct {
	c   *client
	dir catalog
	err error
}

func connect(u terminal, env string, e environment) (s session) {
	s.c = newClient(e.URL)
	fmt.Fprintf(u.out, "\n登录 %s（%s）\n", env, e.URL)
	name, err := u.line("管理员账号：")
	if err != nil {
		s.err = err
		return
	}
	fmt.Fprint(u.out, "管理员密码（隐藏输入）：")
	pass, err := u.readPassword(u)
	fmt.Fprintln(u.out)
	if err == nil {
		err = s.c.login(strings.TrimSpace(name), pass)
	}
	if err == nil {
		s.dir, err = s.c.directory()
	}
	s.err = err
	return
}
func run(args []string, u terminal) error {
	cmd := "publish"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	case "-h", "--help", "help":
		usage(u.out)
		return nil
	case "plan", "check", "publish":
	default:
		return fmt.Errorf("未知命令 %q，请使用 --help", cmd)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(u.out)
	flags.Usage = func() { usage(u.out) }
	cfg := flags.String("config", filepath.Join(filepath.Dir(exe), "hive-panel.json"), "配置路径")
	rootOpt := flags.String("root", "", "仓库目录")
	yes := flags.Bool("yes", false, "跳过最终确认")
	var envs, aliases values
	flags.Var(&envs, "env", "环境")
	flags.Var(&aliases, "panel", "面板")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("多余的参数 %q", flags.Arg(0))
	}
	if *yes && (cmd != "publish" || len(envs) == 0 || len(aliases) == 0) {
		return errors.New("--yes需要publish及明确的--env和--panel")
	}
	c, err := readConfig(*cfg)
	if err != nil {
		return fmt.Errorf("读取 %s：%w", *cfg, err)
	}
	if len(envs) == 0 {
		envs, err = u.choose("选择蜂巢环境", sortedEnvNames(c.Environments), func(k string) string { return k + "  " + c.Environments[k].URL })
		if err != nil {
			return err
		}
	}
	avail := map[string][]string{}
	for _, k := range envs {
		e, ok := c.Environments[k]
		if !ok {
			return fmt.Errorf("未知环境 %s", k)
		}
		for a := range e.Bindings {
			avail[a] = append(avail[a], k)
		}
	}
	if len(aliases) == 0 {
		aliases, err = u.choose("选择要发布的面板", keys(avail), func(a string) string {
			return fmt.Sprintf("%s (%s) — %s", c.Panels[a].Name, a, strings.Join(avail[a], "、"))
		})
		if err != nil {
			return err
		}
	}
	targets, skips, err := c.plan(envs, aliases)
	for _, s := range skips {
		fmt.Fprintln(u.out, "跳过：", s)
	}
	if err != nil {
		return err
	}
	root, err := getRoot(u, *rootOpt)
	if err != nil {
		return err
	}
	fmt.Fprintln(u.out, "\n本地目录：", root)
	packs := map[string][]byte{}
	packErr := map[string]error{}
	sessions := map[string]session{}
	defer func() {
		for _, k := range keys(sessions) {
			if err := sessions[k].c.logout(); err != nil {
				fmt.Fprintf(u.out, "提示：%s 登出回执异常，请在管理台核对会话。\n", k)
			}
		}
	}()
	var items []item
	ready := 0
	for _, t := range targets {
		p := c.Panels[t.Alias]
		if _, seen := packErr[t.Alias]; !seen {
			packs[t.Alias], packErr[t.Alias] = makePackage(root, p)
		}
		it := item{t: t}
		err := packErr[t.Alias]
		if err == nil && cmd != "plan" {
			s, ok := sessions[t.Env]
			if !ok {
				s = connect(u, t.Env, c.Environments[t.Env])
				sessions[t.Env] = s
			}
			err = s.err
			if err == nil {
				it.r, err = s.c.resolve(s.dir, t)
			}
		}
		if err != nil {
			it.res = result{"未执行", err.Error()}
		} else {
			ready++
		}
		items = append(items, it)
	}
	fmt.Fprintln(u.out, "\n本批目标")
	for _, it := range items {
		p := c.Panels[it.t.Alias]
		info := it.res.detail
		if it.res.status == "" {
			info = fmt.Sprintf("ZIP %d 字节", len(packs[it.t.Alias]))
			if cmd != "plan" {
				info += fmt.Sprintf("，当前v%d", it.r.Number)
			}
		}
		fmt.Fprintf(u.out, "  %s  %s  %s → %s/%s  %s %s\n", it.t.Env, p.Name, p.Source, it.t.System, it.t.Key, it.res.status, info)
	}
	if cmd != "publish" {
		if ready != len(items) {
			return errors.New("部分目标检查失败")
		}
		fmt.Fprintln(u.out, "检查通过。")
		return nil
	}
	if ready == 0 {
		return errors.New("没有可发布的目标")
	}
	if !*yes {
		s, err := u.line(fmt.Sprintf("发布以上%d个就绪目标？输入 y 确认：", ready))
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(s), "y") {
			return errCancel
		}
	}
	failed := false
	for i, it := range items {
		if it.res.status == "" {
			fmt.Fprintf(u.out, "正在发布 %s / %s…\n", it.t.Env, c.Panels[it.t.Alias].Name)
			items[i].res = sessions[it.t.Env].c.publish(it.t, it.r, packs[it.t.Alias])
		}
	}
	fmt.Fprintln(u.out, "\n发布结果")
	for _, it := range items {
		fmt.Fprintf(u.out, "  %s / %s  %s  %s\n", it.t.Env, c.Panels[it.t.Alias].Name, it.res.status, it.res.detail)
		if it.res.status != published {
			failed = true
		}
	}
	if failed {
		return errors.New("部分目标未确认发布，请按上表逐项处理")
	}
	return nil
}
func RunCLI() {
	u := terminal{bufio.NewReader(os.Stdin), os.Stdout, password}
	err := run(os.Args[1:], u)
	code := 0
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if !errors.Is(err, errCancel) {
			code = 1
		}
	}
	if runtime.GOOS == "windows" && len(os.Args) == 1 {
		_, _ = u.line("按回车关闭窗口。")
	}
	os.Exit(code)
}

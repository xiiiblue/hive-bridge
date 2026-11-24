package publisher

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type panel struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}
type environment struct {
	URL      string            `json:"url"`
	Bindings map[string]string `json:"bindings,omitempty"`
	Tag      string            `json:"tag,omitempty"`
	Order    int               `json:"order,omitempty"`
}
type config struct {
	Panels       map[string]panel       `json:"panels"`
	Environments map[string]environment `json:"environments"`
}
type target struct{ Env, Alias, System, Key string }

var aliasPattern = regexp.MustCompile("^[a-z0-9][a-z0-9._-]{0,127}$")
var systemPattern = regexp.MustCompile("^[a-z]{1,32}$")

func validEnvName(name string) bool {
	s := strings.TrimSpace(name)
	if s == "" || s != name || len([]rune(s)) > 48 {
		return false
	}
	return !strings.ContainsAny(s, "/\\\r\n\t")
}

func validEnvTag(tag string) bool {
	return tag == "" || tag == "dev" || tag == "test" || tag == "prod"
}

func readConfig(path string) (config, error) {
	var c config
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	return decodeConfig(f)
}
func decodeConfig(r io.Reader) (config, error) {
	var c config
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("配置格式错误：%w", err)
	}
	if d.Decode(new(any)) != io.EOF {
		return c, fmt.Errorf("配置末尾有额外内容")
	}
	return c, c.validate()
}
func keys[T any](m map[string]T) []string {
	a := make([]string, 0, len(m))
	for k := range m {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}

// sortedEnvNames 按配置的 order 升序返回环境名；未指定 order（<=0）的排在后面并按名称排序。
func sortedEnvNames(envs map[string]environment) []string {
	a := make([]string, 0, len(envs))
	for k := range envs {
		a = append(a, k)
	}
	sort.Slice(a, func(i, j int) bool {
		oi, oj := envs[a[i]].Order, envs[a[j]].Order
		if (oi > 0) != (oj > 0) {
			return oi > 0
		}
		if oi != oj {
			return oi < oj
		}
		return a[i] < a[j]
	})
	return a
}

func nextEnvOrder(envs map[string]environment) int {
	maxOrder := 0
	for _, e := range envs {
		if e.Order > maxOrder {
			maxOrder = e.Order
		}
	}
	if maxOrder == 0 {
		return len(envs) + 1
	}
	return maxOrder + 1
}

func (c config) validate() error {
	for k, p := range c.Panels {
		if !aliasPattern.MatchString(k) || strings.TrimSpace(p.Name) == "" || (p.Source != "" && (p.Source == "." || !fs.ValidPath(p.Source) || strings.ContainsAny(p.Source, "\\:"))) {
			return fmt.Errorf("面板 %q 的名称或相对目录无效", k)
		}
	}
	origins := map[string]bool{}
	for k, e := range c.Environments {
		u, err := url.Parse(e.URL)
		if !validEnvName(k) || !validEnvTag(e.Tag) || e.Order < 0 || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return fmt.Errorf("环境 %q 的名称、标签、排序或地址无效", k)
		}
		origin := strings.ToLower(u.Scheme+"://"+u.Host) + strings.TrimRight(u.EscapedPath(), "/")
		if origins[origin] {
			return fmt.Errorf("多个环境重复使用地址 %s", origin)
		}
		origins[origin] = true
		used := map[string]bool{}
		for alias, b := range e.Bindings {
			sys, key, ok := strings.Cut(b, "/")
			if _, known := c.Panels[alias]; !known || !ok || !systemPattern.MatchString(sys) || !aliasPattern.MatchString(key) || used[key] {
				return fmt.Errorf("环境 %s 的绑定 %s = %q 无效或重复", k, alias, b)
			}
			used[key] = true
		}
	}
	return nil
}
func (c config) plan(envs, aliases []string) (out []target, skipped []string, err error) {
	for _, env := range envs {
		e, ok := c.Environments[env]
		if !ok {
			return nil, nil, fmt.Errorf("未知环境 %q", env)
		}
		for _, alias := range aliases {
			if _, ok := c.Panels[alias]; !ok {
				return nil, nil, fmt.Errorf("未知面板 %q", alias)
			}
			b, ok := e.Bindings[alias]
			if !ok {
				skipped = append(skipped, env+" / "+alias+" 未配置")
				continue
			}
			sys, key, _ := strings.Cut(b, "/")
			t := target{env, alias, sys, key}
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil, skipped, fmt.Errorf("所选环境均未配置这些面板")
	}
	return out, skipped, nil
}

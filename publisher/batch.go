package publisher

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type Selection struct{ Env, Alias string }
type Credentials struct{ Name, Password string }
type TargetResult struct {
	Env, Alias, Name, Source, Binding, Status, Detail, URL string
	Version, Bytes                                         int
}
type Report struct {
	ID      string
	Results []TargetResult
	Warning string
}
type checkedBatch struct {
	id, stamp string
	targets   []target
	resources []resource
	report    Report
	packages  map[string][]byte
	sessions  map[string]session
}

func (b *checkedBatch) logout() error {
	var issues []error
	for _, env := range keys(b.sessions) {
		if err := b.sessions[env].c.logout(); err != nil {
			issues = append(issues, fmt.Errorf("%s登出回执异常", env))
		}
	}
	return errors.Join(issues...)
}
func (w *Workspace) discard() error {
	b := w.checked
	w.checked = nil
	var err error
	if w.upload != nil {
		err = w.upload.c.logout()
		w.upload = nil
	}
	if b != nil {
		err = errors.Join(err, b.logout())
	}
	return err
}
func (w *Workspace) Cancel() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.discard()
}

func resolveSelection(c config, chosen []Selection) ([]target, error) {
	if len(chosen) == 0 {
		return nil, errors.New("请至少勾选一个目标")
	}
	seen := map[Selection]bool{}
	var out []target
	for _, s := range chosen {
		if seen[s] {
			return nil, errors.New("发布目标重复")
		}
		seen[s] = true
		pair, _, err := c.plan([]string{s.Env}, []string{s.Alias})
		if err != nil {
			return nil, err
		}
		out = append(out, pair[0])
	}
	return out, nil
}
func adminSession(e environment, cred Credentials) session {
	c := newClient(e.URL)
	s := session{c: c}
	if strings.TrimSpace(cred.Name) == "" || cred.Password == "" {
		s.err = errors.New("请填写此环境的管理员账号和密码")
		return s
	}
	s.err = c.login(strings.TrimSpace(cred.Name), cred.Password)
	if s.err == nil {
		s.dir, s.err = c.directory()
	}
	return s
}

// Prepare freezes exact targets and bytes. An offline preview never creates a publishable batch.
func (w *Workspace) Prepare(stamp string, chosen []Selection, creds map[string]Credentials, online bool, progress func(string)) (Report, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.discard(); err != nil {
		return Report{}, err
	}
	c, current, err := w.read()
	if err != nil {
		return Report{}, err
	}
	if current != stamp {
		return Report{}, errChanged
	}
	if w.root == "" {
		return Report{}, errors.New("请先选择源码根目录")
	}
	targets, err := resolveSelection(c, chosen)
	if err != nil {
		return Report{}, err
	}
	b := &checkedBatch{stamp: stamp, targets: targets, packages: map[string][]byte{}, sessions: map[string]session{}}
	packErrors := map[string]error{}
	ready := 0
	for _, t := range targets {
		p := c.Panels[t.Alias]
		r := TargetResult{Env: t.Env, Alias: t.Alias, Name: p.Name, Source: p.Source, Binding: t.System + "/" + t.Key, Status: "打包通过"}
		progress("正在检查 " + t.Env + " · " + p.Name)
		if _, ok := packErrors[t.Alias]; !ok {
			b.packages[t.Alias], packErrors[t.Alias] = makePackage(w.root, p)
		}
		itemErr := packErrors[t.Alias]
		r.Bytes = len(b.packages[t.Alias])
		var resolved resource
		if online && itemErr == nil {
			s, ok := b.sessions[t.Env]
			if !ok {
				e := c.Environments[t.Env]
				cred := creds[t.Env]
				if strings.TrimSpace(cred.Name) == "" || cred.Password == "" {
					if saved, found := w.creds.get(e.URL); found {
						cred = saved
					}
				}
				s = adminSession(e, cred)
				b.sessions[t.Env] = s
			}
			itemErr = s.err
			if itemErr == nil {
				resolved, itemErr = s.c.resolve(s.dir, t)
			}
			if itemErr == nil {
				r.Version = resolved.Number
				r.Status = "就绪"
				ready++
			}
		}
		if itemErr != nil {
			r.Status = "未执行"
			r.Detail = itemErr.Error()
		}
		b.resources = append(b.resources, resolved)
		b.report.Results = append(b.report.Results, r)
	}
	if ready > 0 {
		var nonce [16]byte
		rand.Read(nonce[:])
		b.id = hex.EncodeToString(nonce[:])
		b.report.ID = b.id
		w.checked = b
	} else if err := b.logout(); err != nil {
		b.report.Warning = err.Error()
	}
	return b.report, nil
}
func (w *Workspace) Publish(id string, progress func(string)) (Report, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.checked
	if b == nil || id == "" || id != b.id {
		return Report{}, errors.New("检查已失效，请重新检查")
	}
	w.checked = nil
	_, current, err := w.read()
	if err != nil || current != b.stamp {
		return Report{}, errors.Join(errChanged, err, b.logout())
	}
	report := b.report
	report.ID = ""
	for i, t := range b.targets {
		r := &report.Results[i]
		if r.Status != "就绪" {
			continue
		}
		progress("正在发布 " + t.Env + " · " + r.Name)
		c := b.sessions[t.Env].c
		outcome := c.publish(t, b.resources[i], b.packages[t.Alias])
		r.Status = outcome.status
		r.Detail = outcome.detail
		if outcome.status == published {
			r.URL = c.base + "/panels/" + r.Binding
		}
	}
	if err := b.logout(); err != nil {
		report.Warning = err.Error()
	}
	return report, nil
}

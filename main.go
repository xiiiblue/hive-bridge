package main

import (
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"hive.local/hive-bridge/publisher"
)

//go:embed all:frontend/dist
var assets embed.FS

type App struct {
	ctx     context.Context
	work    *publisher.Workspace
	writing atomic.Bool
}

func (a *App) Load() (publisher.View, error) { return a.work.View() }
func (a *App) Login(stamp, env string, cred publisher.Credentials) error {
	return a.work.Login(stamp, env, cred)
}
func (a *App) Logout(stamp, env string) error { return a.work.Logout(stamp, env) }
func (a *App) SaveEnvironment(stamp, previousName string, input publisher.EnvInput) error {
	return a.work.SaveEnvironment(stamp, previousName, input)
}
func (a *App) DeleteEnvironment(stamp, name string) error {
	return a.work.DeleteEnvironment(stamp, name)
}
func (a *App) ReorderEnvironments(stamp string, names []string) error {
	return a.work.ReorderEnvironments(stamp, names)
}
func (a *App) ListRemotePanels(env string) ([]publisher.RemotePanelOption, error) {
	return a.work.ListRemotePanels(env)
}
func (a *App) SyncRemotePanels(stamp, env string) (int, error) {
	return a.work.SyncRemotePanels(stamp, env)
}
func (a *App) SavePanel(stamp, previousAlias, env string, input publisher.PanelInput) error {
	return a.work.SavePanel(stamp, previousAlias, env, input)
}
func (a *App) DeletePanel(stamp, alias string) error {
	return a.work.DeletePanel(stamp, alias)
}
func (a *App) ChooseConfig() error {
	p, err := wr.OpenFileDialog(a.ctx, wr.OpenDialogOptions{Title: "导入HiveBridge配置文件", Filters: []wr.FileFilter{{DisplayName: "JSON配置", Pattern: "*.json"}}})
	if err != nil || p == "" {
		return err
	}
	return a.work.ImportConfig(p)
}
func (a *App) ExportConfig() (string, error) {
	p, err := wr.SaveFileDialog(a.ctx, wr.SaveDialogOptions{
		Title:           "导出HiveBridge配置文件",
		DefaultFilename: "hivebridge.json",
		Filters:         []wr.FileFilter{{DisplayName: "JSON配置", Pattern: "*.json"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	if err := a.work.ExportConfig(p); err != nil {
		return "", err
	}
	return p, nil
}
func (a *App) ChooseRoot() error {
	p, err := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "选择业务面板仓库根目录"})
	if err != nil || p == "" {
		return err
	}
	return a.work.SetRoot(p)
}
func (a *App) ChooseSource(stamp, alias string) error {
	view, err := a.work.View()
	if err != nil {
		return err
	}
	opts := wr.OpenDialogOptions{Title: "选择面板上传目录"}
	if view.Root != "" {
		opts.DefaultDirectory = view.Root
	}
	p, err := wr.OpenDirectoryDialog(a.ctx, opts)
	if err != nil || p == "" {
		return err
	}
	return a.work.Bind(stamp, alias, p)
}
func (a *App) progress(msg string) { wr.EventsEmit(a.ctx, "panel-progress", msg) }
func (a *App) Preview(stamp string, chosen []publisher.Selection) (publisher.Report, error) {
	return a.work.Prepare(stamp, chosen, nil, false, a.progress)
}
func (a *App) Check(stamp string, chosen []publisher.Selection, creds map[string]publisher.Credentials) (publisher.Report, error) {
	return a.work.Prepare(stamp, chosen, creds, true, a.progress)
}
func (a *App) Publish(id string) (publisher.Report, error) {
	if !a.writing.CompareAndSwap(false, true) {
		return publisher.Report{}, errors.New("正在发布")
	}
	defer a.writing.Store(false)
	return a.work.Publish(id, a.progress)
}
func (a *App) ChooseUpload(kind string) (publisher.UploadFile, error) {
	var filters []wr.FileFilter
	switch kind {
	case "skill":
		filters = []wr.FileFilter{{DisplayName: "Skill文件", Pattern: "*.md;*.zip"}}
	case "knowledge":
		filters = []wr.FileFilter{{DisplayName: "知识库文档", Pattern: "*.md;*.docx;*.pdf"}}
	default:
		return publisher.UploadFile{}, errors.New("未知上传类型")
	}
	p, err := wr.OpenFileDialog(a.ctx, wr.OpenDialogOptions{Title: "选择上传文件", Filters: filters})
	if err != nil || p == "" {
		return publisher.UploadFile{}, err
	}
	return a.work.SelectFile(kind, p)
}
func (a *App) ChooseSkillDir() (publisher.UploadFile, error) {
	p, err := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "选择 Skill 目录（需包含 SKILL.md）"})
	if err != nil || p == "" {
		return publisher.UploadFile{}, err
	}
	return a.work.SelectSkillDir(p)
}
func (a *App) InspectUpload(stamp, kind, env string, cred publisher.Credentials) (publisher.UploadReview, error) {
	return a.work.InspectUpload(stamp, kind, env, cred)
}
func (a *App) InspectSkillUpload(stamp, env, filename, contentBase64 string) (publisher.UploadReview, error) {
	return a.work.InspectSkillUpload(stamp, env, filename, contentBase64)
}
func (a *App) InspectSkillDirUpload(stamp, env, dirName string, entries []publisher.SkillDirEntry) (publisher.UploadReview, error) {
	return a.work.InspectSkillDirUpload(stamp, env, dirName, entries)
}
func (a *App) Library(id, lib string) (publisher.UploadReview, error) { return a.work.Library(id, lib) }
func (a *App) CommitUpload(id string, opts publisher.UploadOptions) (publisher.UploadReceipt, error) {
	if !a.writing.CompareAndSwap(false, true) {
		return publisher.UploadReceipt{}, errors.New("正在保存资源")
	}
	defer a.writing.Store(false)
	return a.work.CommitUpload(id, opts)
}

// 企业 Skill 双向管理
func (a *App) ListSkills(env string) (publisher.SkillCatalog, error) {
	return a.work.ListSkills(env)
}
func (a *App) GetSkillDetail(env, resourceID string) (publisher.SkillDetail, error) {
	return a.work.GetSkillDetail(env, resourceID)
}
func (a *App) DownloadSkillVersion(env, resourceID, versionID, defaultName string) (string, error) {
	if defaultName == "" {
		defaultName = "skill.zip"
	}
	p, err := wr.SaveFileDialog(a.ctx, wr.SaveDialogOptions{
		Title:           "保存 Skill 包",
		DefaultFilename: defaultName,
		Filters:         []wr.FileFilter{{DisplayName: "Skill 包或 Markdown", Pattern: "*.zip;*.md"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	return a.work.DownloadSkillVersion(env, resourceID, versionID, p)
}
func (a *App) PublishSkillDraft(env, resourceID string, expectedRevision int) error {
	return a.work.PublishSkillDraft(env, resourceID, expectedRevision)
}
func (a *App) SetSkillStatus(env, resourceID string, expectedRevision int, enable bool) error {
	return a.work.SetSkillStatus(env, resourceID, expectedRevision, enable)
}
func (a *App) UpdateSkillMetadata(env, resourceID string, input publisher.SkillMetadataInput) error {
	return a.work.UpdateSkillMetadata(env, resourceID, input)
}

// 知识库与文档双向管理
func (a *App) ListKnowledgeBases(env string) ([]publisher.KnowledgeBaseItem, error) {
	return a.work.ListKnowledgeBases(env)
}
func (a *App) SaveKnowledgeBase(env, id, name, description string, expectedRevision int) error {
	return a.work.SaveKnowledgeBase(env, id, name, description, expectedRevision)
}
func (a *App) DeleteKnowledgeBase(env, id string, expectedRevision int) error {
	return a.work.DeleteKnowledgeBase(env, id, expectedRevision)
}
func (a *App) ListKnowledgeDocuments(env, baseID string) ([]publisher.KnowledgeDocItem, error) {
	return a.work.ListKnowledgeDocuments(env, baseID)
}
func (a *App) DownloadKnowledgeDocument(env, baseID, docID, filename string) (string, error) {
	if filename == "" {
		filename = "document"
	}
	p, err := wr.SaveFileDialog(a.ctx, wr.SaveDialogOptions{
		Title:           "保存知识库文档原件",
		DefaultFilename: filename,
	})
	if err != nil || p == "" {
		return "", err
	}
	return a.work.DownloadKnowledgeDocument(env, baseID, docID, p)
}
func (a *App) RetryKnowledgeDocument(env, baseID, docID string, expectedRevision int) (publisher.KnowledgeDocItem, error) {
	return a.work.RetryKnowledgeDocument(env, baseID, docID, expectedRevision)
}
func (a *App) DeleteKnowledgeDocument(env, baseID, docID string, expectedRevision int) error {
	return a.work.DeleteKnowledgeDocument(env, baseID, docID, expectedRevision)
}
func (a *App) ChooseKnowledgeFiles() ([]publisher.BatchUploadItem, error) {
	paths, err := wr.OpenMultipleFilesDialog(a.ctx, wr.OpenDialogOptions{
		Title:   "选择要上传的知识库文档（可多选）",
		Filters: []wr.FileFilter{{DisplayName: "知识库文档", Pattern: "*.md;*.docx;*.pdf"}},
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	items := make([]publisher.BatchUploadItem, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败：%w", filepath.Base(p), err)
		}
		items = append(items, publisher.BatchUploadItem{
			Filename:      filepath.Base(p),
			ContentBase64: base64.StdEncoding.EncodeToString(raw),
		})
	}
	return items, nil
}
func (a *App) UploadKnowledgeBatch(env, baseID string, files []publisher.BatchUploadItem) ([]publisher.BatchUploadResult, error) {
	if !a.writing.CompareAndSwap(false, true) {
		return nil, errors.New("正在上传文档")
	}
	defer a.writing.Store(false)
	return a.work.UploadKnowledgeBatch(env, baseID, files)
}
func (a *App) Cancel() error { return a.work.Cancel() }

func main() {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		dir = filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	}
	home, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	managedCfg := publisher.EnsureManagedConfig(filepath.Join(home, "hive-panel", "hivebridge.json"), defaultConfig(dir))
	a := &App{work: publisher.NewWorkspace(managedCfg, filepath.Join(home, "hive-panel", "source-root"))}
	content, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		panic(err)
	}
	err = wails.Run(&options.App{Title: "HiveBridge", Width: 1180, Height: 800, MinWidth: 800, MinHeight: 600,
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 255}, AssetServer: &assetserver.Options{Assets: content},
		Bind: []interface{}{a}, OnStartup: func(ctx context.Context) { a.ctx = ctx },
		OnBeforeClose: func(ctx context.Context) bool {
			if a.writing.Load() {
				a.progress("正在等待本批发布回执，完成后即可关闭")
				return true
			}
			return false
		},
		OnShutdown: func(context.Context) {
			if err := a.Cancel(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "com.hiveos.hivebridge"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

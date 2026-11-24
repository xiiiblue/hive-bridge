package publisher

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxFile = 10 << 20
const maxTotal = 50 << 20

var debugBoundary = regexp.MustCompile(`<!--\s*本地测试注入(开始|结束)[^>]*-->`)
var debugScript = regexp.MustCompile(`(?is)^\s*<script(?:\s[^>]*)?>.*</script\s*>\s*$`)
var scriptTag = regexp.MustCompile(`(?i)</?script\b`)
var localIdentity = regexp.MustCompile(`(?i)(?:window\.hivePanel\s*=[^=]|\baccess_token\s*:\s*["'][^"'\s])`)

func cleanHTML(b []byte) ([]byte, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("HTML应使用UTF-8")
	}
	markers := debugBoundary.FindAllSubmatchIndex(b, -1)
	if len(markers) == 0 && !bytes.Contains(b, []byte("本地测试注入开始")) && !bytes.Contains(b, []byte("本地测试注入结束")) {
		return b, nil
	}
	if len(markers) != 2 || string(b[markers[0][2]:markers[0][3]]) != "开始" || string(b[markers[1][2]:markers[1][3]]) != "结束" {
		return nil, errors.New("本地测试注入标记不成对")
	}
	body := b[markers[0][1]:markers[1][0]]
	if !debugScript.Match(body) || len(scriptTag.FindAll(body, -1)) != 2 {
		return nil, errors.New("本地测试注入标记之间应有且仅有一个完整script")
	}
	cleaned := append(bytes.Clone(b[:markers[0][0]]), b[markers[1][1]:]...)
	if bytes.Contains(cleaned, []byte("本地测试注入开始")) || bytes.Contains(cleaned, []byte("本地测试注入结束")) {
		return nil, errors.New("HTML中还存在调试标记")
	}
	return cleaned, nil
}

// 部分照明弹窗把调试身份直接写在JS开头，用带说明的守卫包住。
// 只识别完整的本地上下文声明和单方法守卫，后面的业务逻辑原样保留。
var popupStart = regexp.MustCompile(`(?s)/\* 本地测试注入[^*]*\*/\s*if\s*\(!window\.hivePanel\)\s*\{`)
var popupBody = regexp.MustCompile(`(?s)^\s*const\s+testContext\s*=\s*\{[^;]*?\};\s*window\.hivePanel\s*=\s*Object\.freeze\(\{\s*async\s+getSystemContext\(systemKey\)\s*\{[^{}]*?return\s+testContext;\s*\}\s*\}\);\s*\}`)

func cleanPopup(b []byte) ([]byte, error) {
	start := popupStart.FindIndex(b)
	if start == nil {
		return b, nil
	}
	body := b[start[1]:]
	end := popupBody.FindIndex(body[:min(len(body), 4096)])
	if end == nil {
		return nil, errors.New("弹窗本地测试注入边界已变化")
	}
	return append(bytes.Clone(b[:start[0]]), body[end[1]:]...), nil
}

func openSource(root, source string) (*os.Root, error) {
	if strings.TrimSpace(source) == "" {
		return nil, errors.New("该面板尚未配置本地上传目录")
	}
	if filepath.IsAbs(source) {
		info, err := os.Lstat(source)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("源目录无效或为链接：%s", source)
		}
		return os.OpenRoot(source)
	}
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("该面板使用相对目录，请先选择默认源码根目录，或重新为其选择任意本机目录")
	}
	checkout, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer checkout.Close()
	dir := "."
	for _, part := range strings.Split(source, "/") {
		dir = filepath.Join(dir, part)
		info, err := checkout.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("源目录无效或为链接：%s", dir)
		}
	}
	return checkout.OpenRoot(dir)
}
func makePackage(root string, p panel) ([]byte, error) {
	dir, err := openSource(root, p.Source)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	total, count, htmlCount := 0, 0, 0
	names := map[string]bool{}
	err = fs.WalkDir(dir.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		key := strings.ToLower(rel)
		if d.Name() == ".DS_Store" || strings.EqualFold(d.Name(), "Thumbs.db") {
			if !d.IsDir() {
				return nil
			}
		}
		if d.Type()&os.ModeSymlink != 0 || strings.HasPrefix(d.Name(), ".") || strings.ContainsAny(rel, "\\:") || strings.IndexFunc(rel, unicode.IsControl) >= 0 || !utf8.ValidString(rel) || len([]rune(rel)) > 512 {
			return fmt.Errorf("包中含链接、隐藏项或无效路径：%s", rel)
		}
		if _, seen := names[key]; seen {
			return fmt.Errorf("大小写重复：%s", rel)
		}
		names[key] = !d.IsDir()
		if d.IsDir() {
			return nil
		}
		info, err := dir.Lstat(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxFile {
			return fmt.Errorf("非常规文件或超过10MiB：%s", rel)
		}
		count++
		if count > 512 {
			return errors.New("文件超过512个")
		}
		f, err := dir.Open(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
		f.Close()
		if err != nil {
			return err
		}
		if len(b) > maxFile {
			return fmt.Errorf("超过10MiB：%s", rel)
		}
		if strings.HasSuffix(key, ".html") {
			if filepath.Dir(rel) != "." || !strings.HasSuffix(rel, ".html") {
				return fmt.Errorf("HTML入口应在包的顶层，扩展名为.html：%s", rel)
			}
			htmlCount++
			b, err = cleanHTML(b)
			if err != nil {
				return fmt.Errorf("%s：%w", rel, err)
			}
		}
		if strings.HasSuffix(key, ".js") {
			b, err = cleanPopup(b)
			if err != nil {
				return fmt.Errorf("%s：%w", rel, err)
			}
		}
		if (strings.HasSuffix(key, ".html") || strings.HasSuffix(key, ".js")) && localIdentity.Match(b) {
			return fmt.Errorf("文件含本地宿主身份注入或非空固定票据：%s", rel)
		}
		if key == "panel.md" && (rel != "PANEL.md" || len(bytes.TrimSpace(b)) == 0) {
			return errors.New("请提供非空的PANEL.md")
		}
		if key == "api.json" {
			var data map[string]json.RawMessage
			if rel != "api.json" || json.Unmarshal(b, &data) != nil || data == nil {
				return errors.New("api.json应为JSON对象")
			}
		}
		total += len(b)
		if total > maxTotal {
			return errors.New("解压总量超过50MiB")
		}
		h := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		h.SetMode(0644)
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err != nil {
		return nil, err
	}
	if htmlCount != 1 || !names["panel.md"] || !names["api.json"] {
		return nil, errors.New("需要唯一HTML入口、PANEL.md和api.json")
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > 51<<20 {
		return nil, errors.New("ZIP超过51MiB")
	}
	return buf.Bytes(), nil
}

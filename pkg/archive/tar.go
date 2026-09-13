package archive

import (
	"archive/tar"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/pgzip"
)

// DefaultIgnoredDirs 默认跳过的目录列表
var DefaultIgnoredDirs = []string{
	".git",
	".svn",
	".idea",
	".vscode",
}

// ShouldIgnoreDir 检查目录名是否在跳过列表中
func ShouldIgnoreDir(name string) bool {
	for _, ignored := range DefaultIgnoredDirs {
		if name == ignored {
			return true
		}
	}
	return false
}

// PathFilter 抽象路径过滤匹配器接口 (解耦 rules / gitignore)
type PathFilter interface {
	ShouldIgnore(fullPath, cargoRoot string, isDir bool) bool
}

// WalkAndWriteFilesOnlyTar 仅将 srcDir 下的直接同级文件写入 tar 包 (不递归任何子目录)
func WalkAndWriteFilesOnlyTar(srcDir string, tw *tar.Writer) error {
	return WalkAndWriteFilesOnlyTarWithFilter(srcDir, tw, nil)
}

// WalkAndWriteFilesOnlyTarWithFilter 仅将 srcDir 下的直接同级文件写入 tar 包，支持 PathFilter 排除过滤
func WalkAndWriteFilesOnlyTarWithFilter(srcDir string, tw *tar.Writer, filter PathFilter) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	copyBuf := make([]byte, 1024*1024)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "ark" || name == "ark.exe" || name == "tmp" || name == "cache" || strings.HasPrefix(name, ".git") {
			continue
		}

		filePath := filepath.Join(srcDir, name)
		if filter != nil && filter.ShouldIgnore(filePath, srcDir, false) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		fillOSMetadata(info, hdr)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		copyErr := copyFileToTar(tw, f, hdr.Size, copyBuf)
		f.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// copyFileToTar 安全地将文件内容流式写入 tar.Writer
// 针对正在被动态追加写入的文件（如数据库 redo/undo log、binlog、运行中服务日志等）：
// 1. 使用 io.LimitReader 严格限制最大写入字节数为 targetSize，彻底杜绝 "archive/tar: write too long" 报错；
// 2. 若文件在此期间被截断导致实际读取字节数不足 targetSize，自动以 0 字节填补差额，确保 Tar entry 长度与分块严格对齐。
func copyFileToTar(tw *tar.Writer, f io.Reader, targetSize int64, copyBuf []byte) error {
	if targetSize <= 0 {
		return nil
	}
	limitedReader := io.LimitReader(f, targetSize)
	n, err := io.CopyBuffer(tw, limitedReader, copyBuf)
	if err != nil {
		return err
	}
	if n < targetSize {
		remaining := targetSize - n
		var zeroBuf [32 * 1024]byte
		for remaining > 0 {
			toWrite := int64(len(zeroBuf))
			if toWrite > remaining {
				toWrite = remaining
			}
			written, err := tw.Write(zeroBuf[:toWrite])
			if err != nil {
				return err
			}
			remaining -= int64(written)
		}
	}
	return nil
}

// WalkAndWriteTar 遍历 srcDir 并将所有文件与目录写入 tar.Writer (兼容老接口)
func WalkAndWriteTar(srcDir string, tw *tar.Writer) error {
	return WalkAndWriteTarWithFilter(srcDir, tw, nil)
}

// WalkAndWriteTarWithFilter 遍历 srcDir 并将所有文件与目录写入 tar.Writer，支持 PathFilter 排除过滤
func WalkAndWriteTarWithFilter(srcDir string, tw *tar.Writer, filter PathFilter) error {
	stat, err := os.Stat(srcDir)
	if err != nil {
		return err
	}

	copyBuf := make([]byte, 1024*1024)

	// 若目标为单个普通文件，直接封装单文件
	if !stat.IsDir() {
		if filter != nil && filter.ShouldIgnore(srcDir, srcDir, false) {
			return nil
		}
		hdr, err := tar.FileInfoHeader(stat, "")
		if err != nil {
			return err
		}
		hdr.Name = stat.Name()
		fillOSMetadata(stat, hdr)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(srcDir)
		if err != nil {
			return err
		}
		err = copyFileToTar(tw, f, hdr.Size, copyBuf)
		f.Close()
		return err
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if path == srcDir {
			return nil
		}

		if info.IsDir() {
			if ShouldIgnoreDir(info.Name()) && path != srcDir {
				return filepath.SkipDir
			}
			if filter != nil && filter.ShouldIgnore(path, srcDir, true) {
				return filepath.SkipDir
			}
		} else {
			if filter != nil && filter.ShouldIgnore(path, srcDir, false) {
				return nil
			}
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		hdr.Name = rel
		// 填充并保留数字 UID 与 GID
		fillOSMetadata(info, hdr)

		if info.IsDir() {
			hdr.Name += "/"
			return tw.WriteHeader(hdr)
		}

		if !info.Mode().IsRegular() {
			// 如果是软链接
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(path)
				if err == nil {
					hdr.Linkname = linkTarget
					return tw.WriteHeader(hdr)
				}
			}
			return nil
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		copyErr := copyFileToTar(tw, f, hdr.Size, copyBuf)
		f.Close()
		return copyErr
	})
}

// PackTar 将 srcDir 打包为未压缩的 tar 文件 (保留兼容)
func PackTar(srcDir, destTarPath string) error {
	out, err := os.Create(destTarPath)
	if err != nil {
		return err
	}
	defer out.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	return WalkAndWriteTar(srcDir, tw)
}

type dirMetadata struct {
	path    string
	mode    os.FileMode
	uid     int
	gid     int
	modTime time.Time
}

// UnpackTarStream 从任意 io.Reader 流中解压 Tar 格式数据至 destDir，严格恢复原始文件权限、数字 UID/GID 与修改时间
func UnpackTarStream(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	dirList := make([]dirMetadata, 0, 128)
	copyBuf := make([]byte, 1024*1024)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
			continue // 防御 Zip Slip 路径穿越
		}

		target := filepath.Join(destDir, cleanName)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			dirList = append(dirList, dirMetadata{
				path:    target,
				mode:    hdr.FileInfo().Mode().Perm(),
				uid:     hdr.Uid,
				gid:     hdr.Gid,
				modTime: hdr.ModTime,
			})

		case tar.TypeReg, tar.TypeRegA:
			parent := filepath.Dir(target)
			if err := os.MkdirAll(parent, 0755); err != nil {
				return err
			}

			// 写入前清理旧文件，防止已有只读权限文件导致拒绝访问
			_ = os.Remove(target)

			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.CopyBuffer(outFile, tr, copyBuf); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()

			restoreOwnership(target, hdr.Uid, hdr.Gid)
			if !hdr.ModTime.IsZero() {
				_ = os.Chtimes(target, hdr.AccessTime, hdr.ModTime)
			}

		case tar.TypeSymlink:
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err == nil {
				restoreSymlinkOwnership(target, hdr.Uid, hdr.Gid)
			}
		}
	}

	// 最终按路径倒序恢复目录的真实权限与修改时间（确保子文件写入不影响父目录属性）
	for i := len(dirList) - 1; i >= 0; i-- {
		d := dirList[i]
		restoreOwnership(d.path, d.uid, d.gid)
		_ = os.Chmod(d.path, d.mode)
		if !d.modTime.IsZero() {
			_ = os.Chtimes(d.path, d.modTime, d.modTime)
		}
	}

	return nil
}

// UnpackTar 将 tar 或 tar.gz 文件解压到 destDir，自动识别是否含有 gzip 压缩 (严格恢复原始文件权限与数字所有者)
func UnpackTar(tarPath, destDir string) error {
	return UnpackTarWithProgress(tarPath, destDir, nil)
}

// UnpackTarWithProgress 将 tar 或 tar.gz 文件解压到 destDir，支持实时进度与速率回调
func UnpackTarWithProgress(tarPath, destDir string, onProgress ProgressCallback) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var r io.Reader = f
	if onProgress != nil {
		r = &countingReader{r: f, callback: onProgress}
	}

	br := bufio.NewReaderSize(r, 1024*1024)
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gr, err := pgzip.NewReader(br)
		if err != nil {
			return err
		}
		defer gr.Close()
		return UnpackTarStream(gr, destDir)
	}

	return UnpackTarStream(br, destDir)
}

package archive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// WalkAndWriteFilesOnlyTar 仅将 srcDir 下的直接同级文件写入 tar 包 (不递归任何子目录)
func WalkAndWriteFilesOnlyTar(srcDir string, tw *tar.Writer) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "ark" || name == "ark.exe" || name == "tmp" || name == "cache" || strings.HasPrefix(name, ".git") {
			continue
		}

		filePath := filepath.Join(srcDir, name)
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
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

// WalkAndWriteTar 遍历 srcDir 并将所有文件与目录写入 tar.Writer，严格保留文件权限与数字 UID/GID
func WalkAndWriteTar(srcDir string, tw *tar.Writer) error {
	stat, err := os.Stat(srcDir)
	if err != nil {
		return err
	}

	// 若目标为单个普通文件，直接封装单文件
	if !stat.IsDir() {
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
		defer f.Close()
		_, err = io.Copy(tw, f)
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
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
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
			if _, err := io.Copy(outFile, tr); err != nil {
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
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gr, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer gr.Close()
		return UnpackTarStream(gr, destDir)
	}

	return UnpackTarStream(br, destDir)
}

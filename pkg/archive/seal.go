package archive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"

	"github.com/klauspost/pgzip"
	"golang.org/x/crypto/pbkdf2"
)

const (
	saltLen    = 8
	keyLen     = 32
	ivLen      = 16
	iterations = 100000
)

var magicSalted = []byte("Salted__")

// SealFile 使用 AES-256-CBC + PBKDF2 对输入文件进行流式安全密闭封装 (兼容 OpenSSL 标准，常量内存)
func SealFile(srcPath, destPath string, passphrase []byte) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}

	keyAndIV := pbkdf2.Key(passphrase, salt, iterations, keyLen+ivLen, sha256.New)
	key := keyAndIV[:keyLen]
	iv := keyAndIV[keyLen : keyLen+ivLen]

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// 写入 OpenSSL 兼容头部: "Salted__" (8B) + salt (8B)
	if _, err := out.Write(magicSalted); err != nil {
		return err
	}
	if _, err := out.Write(salt); err != nil {
		return err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	blockSize := block.BlockSize()
	// 使用 64KB 缓冲区 (16 字节的倍数)
	bufSize := 64 * 1024
	buf := make([]byte, bufSize)
	cipherBuf := make([]byte, bufSize)

	for {
		n, readErr := io.ReadFull(in, buf)
		if n > 0 {
			if n == bufSize {
				mode.CryptBlocks(cipherBuf, buf)
				if _, err := out.Write(cipherBuf); err != nil {
					return err
				}
			} else {
				// 最后一块需要补齐 PKCS7 padding
				lastPadded := pkcs7Pad(buf[:n], blockSize)
				lastCipher := make([]byte, len(lastPadded))
				mode.CryptBlocks(lastCipher, lastPadded)
				if _, err := out.Write(lastCipher); err != nil {
					return err
				}
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				// 文件长度恰好是 bufSize 的倍数，需要追加完整填充块
				emptyPadded := pkcs7Pad([]byte{}, blockSize)
				emptyCipher := make([]byte, len(emptyPadded))
				mode.CryptBlocks(emptyCipher, emptyPadded)
				if _, err := out.Write(emptyCipher); err != nil {
					return err
				}
				break
			}
			if readErr == io.ErrUnexpectedEOF {
				break
			}
			return readErr
		}
	}

	return nil
}

// UnsealFile 流式解密密闭文件并写出明文 (兼容 OpenSSL 标准，常量内存)
func UnsealFile(srcPath, destPath string, passphrase []byte) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	// 读取 16 字节头部: "Salted__" + salt
	header := make([]byte, len(magicSalted)+saltLen)
	if _, err := io.ReadFull(in, header); err != nil {
		return fmt.Errorf("读取文件头部失败: %w", err)
	}

	if !bytes.Equal(header[:len(magicSalted)], magicSalted) {
		return errors.New("无效的文件魔数 (非标准 OpenSSL 格式)")
	}

	salt := header[len(magicSalted):]
	keyAndIV := pbkdf2.Key(passphrase, salt, iterations, keyLen+ivLen, sha256.New)
	key := keyAndIV[:keyLen]
	iv := keyAndIV[keyLen : keyLen+ivLen]

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	mode := cipher.NewCBCDecrypter(block, iv)
	blockSize := block.BlockSize()
	bufSize := 64 * 1024
	buf := make([]byte, bufSize)

	var prevPlain []byte

	for {
		n, readErr := io.ReadFull(in, buf)
		if n > 0 {
			if n%blockSize != 0 {
				return errors.New("密文数据长度非分组块整数倍，文件可能损坏")
			}

			// 如果之前有暂存的分块，写入输出（说明它不是最后一块）
			if len(prevPlain) > 0 {
				if _, err := out.Write(prevPlain); err != nil {
					return err
				}
				prevPlain = nil
			}

			chunkPlain := make([]byte, n)
			mode.CryptBlocks(chunkPlain, buf[:n])
			prevPlain = chunkPlain
		}

		if readErr != nil {
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			return readErr
		}
	}

	// 对最后一块去除 PKCS7 padding 并写出
	if len(prevPlain) == 0 {
		return errors.New("密文数据为空")
	}

	unpadded, err := pkcs7Unpad(prevPlain, blockSize)
	if err != nil {
		return fmt.Errorf("解密校验失败 (可能密码不正确): %w", err)
	}

	if len(unpadded) > 0 {
		if _, err := out.Write(unpadded); err != nil {
			return err
		}
	}

	return nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	length := len(data)
	if length == 0 || length%blockSize != 0 {
		return nil, errors.New("invalid unpad block size")
	}
	padding := int(data[length-1])
	if padding > blockSize || padding == 0 {
		return nil, errors.New("invalid padding size")
	}
	for i := length - padding; i < length; i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("invalid padding bytes")
		}
	}
	return data[:length-padding], nil
}

type cbcStreamWriter struct {
	w         io.Writer
	mode      cipher.BlockMode
	blockSize int
	buf       []byte
	bufLen    int
	encBuf    []byte
}

func newCBCStreamWriter(w io.Writer, block cipher.Block, iv []byte) *cbcStreamWriter {
	bs := block.BlockSize()
	capacity := 512 * 1024
	return &cbcStreamWriter{
		w:         w,
		mode:      cipher.NewCBCEncrypter(block, iv),
		blockSize: bs,
		buf:       make([]byte, capacity),
		bufLen:    0,
		encBuf:    make([]byte, capacity),
	}
}

func (sw *cbcStreamWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		avail := len(sw.buf) - sw.bufLen
		if avail > len(p) {
			avail = len(p)
		}
		copy(sw.buf[sw.bufLen:], p[:avail])
		sw.bufLen += avail
		p = p[avail:]

		// 当缓冲区足够大时，集中批量加密并输出，零堆内存分配
		if sw.bufLen >= len(sw.buf)-sw.blockSize {
			encryptable := sw.bufLen - (sw.bufLen % sw.blockSize)
			if encryptable > 0 {
				sw.mode.CryptBlocks(sw.encBuf[:encryptable], sw.buf[:encryptable])
				if _, err := sw.w.Write(sw.encBuf[:encryptable]); err != nil {
					return 0, err
				}
				remainder := sw.bufLen - encryptable
				if remainder > 0 {
					copy(sw.buf[:remainder], sw.buf[encryptable:sw.bufLen])
				}
				sw.bufLen = remainder
			}
		}
	}
	return total, nil
}

func (sw *cbcStreamWriter) Close() error {
	encryptable := sw.bufLen - (sw.bufLen % sw.blockSize)
	if encryptable > 0 {
		sw.mode.CryptBlocks(sw.encBuf[:encryptable], sw.buf[:encryptable])
		if _, err := sw.w.Write(sw.encBuf[:encryptable]); err != nil {
			return err
		}
		remainder := sw.bufLen - encryptable
		if remainder > 0 {
			copy(sw.buf[:remainder], sw.buf[encryptable:sw.bufLen])
		}
		sw.bufLen = remainder
	}

	padded := pkcs7Pad(sw.buf[:sw.bufLen], sw.blockSize)
	outBuf := make([]byte, len(padded))
	sw.mode.CryptBlocks(outBuf, padded)
	_, err := sw.w.Write(outBuf)
	sw.bufLen = 0
	return err
}

func resolveGzipLevel() int {
	level := pgzip.BestSpeed
	if envLvl := os.Getenv("ARK_GZIP_LEVEL"); envLvl != "" {
		if l, err := strconv.Atoi(envLvl); err == nil && l >= -1 && l <= 9 {
			level = l
		}
	}
	return level
}

// newParallelGzipWriter 创建针对多核优化的高吞吐分块并行 Gzip 写入器
func newParallelGzipWriter(w io.Writer) (*pgzip.Writer, error) {
	level := resolveGzipLevel()
	gw, err := pgzip.NewWriterLevel(w, level)
	if err != nil {
		return nil, err
	}
	// 针对多核机器优化并发分块 (每块 1MB，根据 CPU 核心数动态匹配并发度)
	numCPU := runtime.NumCPU()
	blocks := numCPU * 2
	if blocks < 4 {
		blocks = 4
	}
	if blocks > 32 {
		blocks = 32
	}
	_ = gw.SetConcurrency(1024*1024, blocks)
	return gw, nil
}

// ProgressCallback 进度通知函数，接收已处理的未压缩源字节数
type ProgressCallback func(processedBytes int64)

type countingWriter struct {
	w        io.Writer
	written  int64
	callback ProgressCallback
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.written += int64(n)
	if cw.callback != nil {
		cw.callback(cw.written)
	}
	return n, err
}

type countingReader struct {
	r        io.Reader
	read     int64
	callback ProgressCallback
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.read += int64(n)
		if cr.callback != nil {
			cr.callback(cr.read)
		}
	}
	return n, err
}

// PackAndSealSourceStreamWithProgress 将指定源边打包 (Tar) -> 边压缩 (Gzip) -> 边加密 (AES-256) 写入 destDatPath，支持进度回调
func PackAndSealSourceStreamWithProgress(srcDir, destDatPath string, passphrase []byte, filesOnly bool, onProgress ProgressCallback) error {
	return PackAndSealSourceStreamWithProgressAndFilter(srcDir, destDatPath, passphrase, filesOnly, onProgress, nil)
}

// PackAndSealSourceStreamWithProgressAndFilter 将指定源边打包 (Tar) -> 边压缩 (Gzip) -> 边加密 (AES-256) 写入 destDatPath，支持进度回调与 PathFilter 排除过滤
func PackAndSealSourceStreamWithProgressAndFilter(srcDir, destDatPath string, passphrase []byte, filesOnly bool, onProgress ProgressCallback, filter PathFilter) error {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}

	keyAndIV := pbkdf2.Key(passphrase, salt, iterations, keyLen+ivLen, sha256.New)
	key := keyAndIV[:keyLen]
	iv := keyAndIV[keyLen : keyLen+ivLen]

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	out, err := os.Create(destDatPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.Write(magicSalted); err != nil {
		return err
	}
	if _, err := out.Write(salt); err != nil {
		return err
	}

	bw := bufio.NewWriterSize(out, 1024*1024)
	encWriter := newCBCStreamWriter(bw, block, iv)
	gw, err := newParallelGzipWriter(encWriter)
	if err != nil {
		return err
	}

	var tw *tar.Writer
	if onProgress != nil {
		cw := &countingWriter{w: gw, callback: onProgress}
		tw = tar.NewWriter(cw)
	} else {
		tw = tar.NewWriter(gw)
	}

	var walkErr error
	if filesOnly {
		walkErr = WalkAndWriteFilesOnlyTarWithFilter(srcDir, tw, filter)
	} else {
		walkErr = WalkAndWriteTarWithFilter(srcDir, tw, filter)
	}

	if err := tw.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	if err := gw.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	if err := encWriter.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	if err := bw.Flush(); err != nil && walkErr == nil {
		walkErr = err
	}

	return walkErr
}

// PackAndSealSourceStream 将指定源边打包 (Tar) -> 边压缩 (Gzip) -> 边加密 (AES-256) 写入 destDatPath
// 全程内存流式流水线直达，磁盘零中间临时文件，支持 filesOnly 同级文件打包
func PackAndSealSourceStream(srcDir, destDatPath string, passphrase []byte, filesOnly bool) error {
	return PackAndSealSourceStreamWithProgress(srcDir, destDatPath, passphrase, filesOnly, nil)
}

// PackAndSealStream 将指定源目录边打包 (Tar) -> 边压缩 (Gzip) -> 边加密 (AES-256) 写入 destDatPath
func PackAndSealStream(srcDir, destDatPath string, passphrase []byte) error {
	return PackAndSealSourceStream(srcDir, destDatPath, passphrase, false)
}

// PackSourceTarGzWithProgress 将指定源边打包边 gzip 压缩写入 destTarGzPath，支持进度回调
func PackSourceTarGzWithProgress(srcDir, destTarGzPath string, filesOnly bool, onProgress ProgressCallback) error {
	return PackSourceTarGzWithProgressAndFilter(srcDir, destTarGzPath, filesOnly, onProgress, nil)
}

// PackSourceTarGzWithProgressAndFilter 将指定源边打包边 gzip 压缩写入 destTarGzPath，支持进度回调与 PathFilter 排除过滤
func PackSourceTarGzWithProgressAndFilter(srcDir, destTarGzPath string, filesOnly bool, onProgress ProgressCallback, filter PathFilter) error {
	out, err := os.Create(destTarGzPath)
	if err != nil {
		return err
	}
	defer out.Close()

	bw := bufio.NewWriterSize(out, 1024*1024)
	gw, err := newParallelGzipWriter(bw)
	if err != nil {
		return err
	}

	var tw *tar.Writer
	if onProgress != nil {
		cw := &countingWriter{w: gw, callback: onProgress}
		tw = tar.NewWriter(cw)
	} else {
		tw = tar.NewWriter(gw)
	}

	var walkErr error
	if filesOnly {
		walkErr = WalkAndWriteFilesOnlyTarWithFilter(srcDir, tw, filter)
	} else {
		walkErr = WalkAndWriteTarWithFilter(srcDir, tw, filter)
	}

	if err := tw.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	if err := gw.Close(); err != nil && walkErr == nil {
		walkErr = err
	}
	if err := bw.Flush(); err != nil && walkErr == nil {
		walkErr = err
	}
	return walkErr
}

// PackSourceTarGz 将指定源边打包边 gzip 压缩写入 destTarGzPath
func PackSourceTarGz(srcDir, destTarGzPath string, filesOnly bool) error {
	return PackSourceTarGzWithProgress(srcDir, destTarGzPath, filesOnly, nil)
}

// PackTarGz 将指定源目录边打包边 gzip 压缩写入 destTarGzPath
func PackTarGz(srcDir, destTarGzPath string) error {
	return PackSourceTarGz(srcDir, destTarGzPath, false)
}

// UnsealAndUnpackStream 从加密文件流式读取并解密 (AES-256) -> 解压 (Gzip) -> 展开 (Tar)
// 零临时 tar 解密文件落盘，并自动智能兼容 gzip 与旧版未压缩 raw tar！
func UnsealAndUnpackStream(srcDatPath, destDir string, passphrase []byte) error {
	return UnsealAndUnpackStreamWithProgress(srcDatPath, destDir, passphrase, nil)
}

// UnsealAndUnpackStreamWithProgress 从加密文件流式读取并解密 (AES-256) -> 解压 (Gzip) -> 展开 (Tar)，支持进度通知
func UnsealAndUnpackStreamWithProgress(srcDatPath, destDir string, passphrase []byte, onProgress ProgressCallback) error {
	in, err := os.Open(srcDatPath)
	if err != nil {
		return err
	}
	defer in.Close()

	header := make([]byte, len(magicSalted)+saltLen)
	if _, err := io.ReadFull(in, header); err != nil {
		return fmt.Errorf("读取加密头部失败: %w", err)
	}

	if !bytes.Equal(header[:len(magicSalted)], magicSalted) {
		return errors.New("无效的文件格式: 缺少 Salted__ 标记")
	}

	salt := header[len(magicSalted):]
	keyAndIV := pbkdf2.Key(passphrase, salt, iterations, keyLen+ivLen, sha256.New)
	key := keyAndIV[:keyLen]
	iv := keyAndIV[keyLen : keyLen+ivLen]

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()

	go func() {
		mode := cipher.NewCBCDecrypter(block, iv)
		blockSize := block.BlockSize()
		bufSize := 512 * 1024
		buf := make([]byte, bufSize)
		var prevPlain []byte
		var totalRead int64 = int64(len(header))

		for {
			n, readErr := io.ReadFull(in, buf)
			if n > 0 {
				if n%blockSize != 0 {
					pw.CloseWithError(errors.New("密文数据长度非分组块整数倍"))
					return
				}
				totalRead += int64(n)
				if onProgress != nil {
					onProgress(totalRead)
				}
				if len(prevPlain) > 0 {
					if _, err := pw.Write(prevPlain); err != nil {
						pw.CloseWithError(err)
						return
					}
					prevPlain = nil
				}
				chunkPlain := make([]byte, n)
				mode.CryptBlocks(chunkPlain, buf[:n])
				prevPlain = chunkPlain
			}
			if readErr != nil {
				if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
					break
				}
				pw.CloseWithError(readErr)
				return
			}
		}

		if len(prevPlain) == 0 {
			pw.CloseWithError(errors.New("密文数据为空"))
			return
		}

		unpadded, err := pkcs7Unpad(prevPlain, blockSize)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("解密校验失败 (可能密码不正确): %w", err))
			return
		}
		if len(unpadded) > 0 {
			if _, err := pw.Write(unpadded); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.Close()
	}()

	br := bufio.NewReaderSize(pr, 1024*1024)
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		// Gzip 压缩流，创建 pgzip.Reader 实时多核解压
		gr, err := pgzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("Gzip 解压初始化失败: %w", err)
		}
		defer gr.Close()
		return UnpackTarStream(gr, destDir)
	}

	// 兼容旧版未压缩 raw tar 流
	return UnpackTarStream(br, destDir)
}

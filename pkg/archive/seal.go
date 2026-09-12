package archive

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

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

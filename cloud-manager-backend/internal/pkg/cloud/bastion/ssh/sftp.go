package ssh

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/pkg/sftp"
)

// FileInfo 文件信息
type FileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
}

// UploadFile 通过 SFTP 上传文件（同名自动追加 _1, _2...）
func UploadFile(client *gossh.Client, reader io.Reader, remotePath string) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()

	origPath := remotePath
	for i := 1; ; i++ {
		if _, err := sftpClient.Stat(remotePath); err != nil {
			break
		}
		dot := len(origPath)
		for j := len(origPath) - 1; j >= 0; j-- {
			if origPath[j] == '.' {
				dot = j
				break
			}
		}
		remotePath = origPath[:dot] + fmt.Sprintf("_%d", i) + origPath[dot:]
	}

	dstFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("创建远程文件失败: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, reader); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	if remotePath != origPath {
		fmt.Printf("[SFTP] 自动重命名: %s -> %s\n", origPath, remotePath)
	}
	return nil
}

// DownloadFile 通过 SFTP 下载文件，返回可读的文件流
func DownloadFile(client *gossh.Client, remotePath string) (io.ReadCloser, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("创建SFTP客户端失败: %w", err)
	}

	srcFile, err := sftpClient.Open(remotePath)
	if err != nil {
		sftpClient.Close()
		return nil, fmt.Errorf("打开远程文件失败: %w", err)
	}

	// 返回一个包装的 ReadCloser，关闭时会同时关闭 SFTP 客户端
	return &sftpReadCloser{File: srcFile, client: sftpClient}, nil
}

// sftpReadCloser 包装 sftp.File 和 sftp.Client，确保关闭时释放连接
type sftpReadCloser struct {
	*sftp.File
	client *sftp.Client
}

func (s *sftpReadCloser) Close() error {
	err := s.File.Close()
	s.client.Close()
	return err
}

// RemovePath 通过 SFTP 删除远端文件或目录（目录递归删除，含非空目录）
func RemovePath(client *gossh.Client, path string) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()
	return removePathRecursive(sftpClient, path)
}

// RemovePathOnClient 在已建立的 SFTP 客户端上删除路径（供需先列举目录定位的场景复用，避免重复建连）
func RemovePathOnClient(c *sftp.Client, path string) error {
	return removePathRecursive(c, path)
}

// removePathRecursive 递归删除：文件直接 Remove，目录先删子项再 RemoveDirectory
func removePathRecursive(c *sftp.Client, path string) error {
	info, err := c.Stat(path)
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %w", err)
	}
	if info.IsDir() {
		entries, err := c.ReadDir(path)
		if err != nil {
			return fmt.Errorf("读取目录失败: %w", err)
		}
		for _, e := range entries {
			child := path
			if child[len(child)-1] != '/' {
				child += "/"
			}
			child += e.Name()
			if err := removePathRecursive(c, child); err != nil {
				return err
			}
		}
		if err := c.RemoveDirectory(path); err != nil {
			return fmt.Errorf("删除目录失败: %w", err)
		}
		return nil
	}
	if err := c.Remove(path); err != nil {
		return fmt.Errorf("删除文件失败: %w", err)
	}
	return nil
}

// ListFiles 通过 SFTP 列出目录文件
// 返回值：文件列表、实际列出的目录绝对路径（用于前端回显，空路径/ "." 会解析为登录后的家目录）、错误
func ListFiles(client *gossh.Client, dirPath string) ([]FileInfo, string, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, "", fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()

	// 空路径或 "." 表示用户家目录（SFTP 登录后的当前工作目录），解析成绝对路径返回给前端
	realPath := dirPath
	if realPath == "" || realPath == "." {
		if wd, e := sftpClient.Getwd(); e == nil && wd != "" {
			realPath = wd
		}
	}

	entries, err := sftpClient.ReadDir(realPath)
	if err != nil {
		return nil, "", fmt.Errorf("读取目录失败: %w", err)
	}

	// 先按与 DeleteFile 完全一致的规则排序原始 entries（先目录后文件、组内自然排序），
	// 保证前端 fileList 的展示顺序与删除时按 idx 取回的顺序严格对齐，避免删错文件。
	SortEntries(entries)

	var result []FileInfo
	for _, entry := range entries {
		result = append(result, FileInfo{
			Name:    entry.Name(),
			Size:    entry.Size(),
			Mode:    entry.Mode().String(),
			ModTime: entry.ModTime().Format(time.DateTime),
			IsDir:   entry.IsDir(),
		})
	}

	return result, realPath, nil
}

// SortEntries 对 SFTP 目录项按「先目录后文件、组内自然排序」规则原地排序。
// ListFiles 与 DeleteFile 共用此函数，确保前端 fileList 的展示顺序与按 idx 删除时
// 后端 ReadDir 取回的顺序严格一致，否则会因排序不同导致删错文件。
func SortEntries(entries []os.FileInfo) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir() // 目录(true)排在文件(false)之前
		}
		return naturalLess(entries[i].Name(), entries[j].Name())
	})
}

// naturalLess 自然排序比较：把字符串按「数字段/非数字段」切分，数字段按数值比较，
// 从而实现 file2 < file10、img1 < img2 这类符合直觉的排序。
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			// 提取连续数字段
			na, ia := scanNum(a, i)
			nb, ib := scanNum(b, j)
			if na != nb {
				return na < nb
			}
			i, j = ia, ib
			continue
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a) < len(b)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// scanNum 从 s[i] 起读取连续数字段，返回数值与结束下标
func scanNum(s string, i int) (int, int) {
	n := 0
	for i < len(s) && isDigit(s[i]) {
		n = n*10 + int(s[i]-'0')
		i++
	}
	return n, i
}

// OpenAppend 以「读写+不存在则创建」方式打开远端文件，供分片上传时按 offset 写入。
// truncate=true 时（通常用于 offset=0 的全新/重新开始上传）先清空文件，避免残留旧数据。
// pkg/sftp 的 File 实现了 WriterAt（WriteAt），可直接定位写入，无需 Seek。
func OpenAppend(client *gossh.Client, remotePath string, truncate bool) (*sftpFileWithClient, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	flag := os.O_RDWR | os.O_CREATE
	if truncate {
		flag |= os.O_TRUNC
	}
	f, err := sftpClient.OpenFile(remotePath, flag)
	if err != nil {
		sftpClient.Close()
		return nil, fmt.Errorf("打开远程文件失败: %w", err)
	}
	// 返回文件时附带 client，便于调用方关闭时释放连接
	return &sftpFileWithClient{File: f, client: sftpClient}, nil
}

// sftpFileWithClient 包装 sftp.File 与其所属 client，关闭时一并释放
type sftpFileWithClient struct {
	*sftp.File
	client *sftp.Client
}

func (s *sftpFileWithClient) Close() error {
	err := s.File.Close()
	s.client.Close()
	return err
}

// StatSize 返回远端文件大小（字节），不存在返回 -1
func StatSize(client *gossh.Client, remotePath string) (int64, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return -1, fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()
	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return -1, nil // 文件不存在视为 0 已传（调用方据此从头开始）
	}
	return info.Size(), nil
}

// ComputeMD5 计算远端文件的 MD5 十六进制串（用于上传完成后校验完整性）
func ComputeMD5(client *gossh.Client, remotePath string) (string, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()
	src, err := sftpClient.Open(remotePath)
	if err != nil {
		return "", fmt.Errorf("打开远程文件失败: %w", err)
	}
	defer src.Close()
	h := md5.New()
	if _, err := io.Copy(h, src); err != nil {
		return "", fmt.Errorf("读取远程文件失败: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// UploadChunk 上传单个分片：以读写方式打开远端文件，使用 WriteAt 直接写入到 offset 处。
// pkg/sftp 的 File 实现了 WriterAt（WriteAt），无需逐字节 Seek/重读，支持断点续传
// （offset 即已上传字节数，重复上传同一 offset 分片可幂等覆盖）。
func UploadChunk(client *gossh.Client, remotePath string, offset int64, data []byte) error {
	// offset==0 视为全新/重新开始上传，清空旧文件避免残留数据
	f, err := OpenAppend(client, remotePath, offset == 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteAt(data, offset); err != nil {
		return fmt.Errorf("写入分片失败: %w", err)
	}
	return nil
}

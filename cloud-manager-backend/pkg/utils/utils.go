package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
)

// 格式化输出JSON
// Go 结构体 → JSON
func PrintJsonData(response any) (string, error) {
	jsonData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return "", fmt.Errorf("响应格式化失败: %w", err)
	}

	jsonString := string(jsonData)
	return jsonString, nil
}


// 向指定文件写入内容（覆盖模式）
func Writefile(filename, content string) error {
	data := []byte(content)

	err := os.WriteFile(filename, data, 0644)
	if err != nil {
		return err
	}

	log.Println("文件写入成功：", filename)
	return nil
}

// 向指定文件写入内容（追加模式）
func Appendfile(filename string, content string) error {
	// 1. 适配系统换行符：Windows 用 \r\n，其他系统（Linux/macOS）用 \n
	var newline string
	switch runtime.GOOS {
	case "windows":
		newline = "\r\n"
	default:
		newline = "\n"
	}

	data := []byte(content + newline)

	// 打开文件：os.O_APPEND（追加）| os.O_CREATE（不存在则创建）| os.O_WRONLY（只写）
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close() // 确保文件最终关闭

	// 写入内容
	if _, err := file.Write(data); err != nil {
		fmt.Println(err)
		return err
	}

	return nil
}


// 判断文件是否存在
func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err) // 不存在返回 false，存在（或其他错误）返回 true
}
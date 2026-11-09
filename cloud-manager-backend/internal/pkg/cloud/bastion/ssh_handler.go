package bastion

import (
	"cloud-manager-backend/internal/config"
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/bastion/ssh"
	"cloud-manager-backend/pkg/logger"
	"cloud-manager-backend/pkg/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	bssh "golang.org/x/crypto/ssh"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// 仅允许同源请求和明确配置的域名
		origin := r.Header.Get("Origin")
		if origin == "" {
			// 非浏览器客户端（curl/CLI工具）不发送 Origin，放行
			return true
		}
		host := r.Host
		// 允许与 Host 同源（含端口），以及 localhost
		return origin == "http://"+host || origin == "https://"+host ||
			origin == "http://localhost" || origin == "https://localhost" ||
			origin == "http://127.0.0.1" || origin == "https://127.0.0.1"
	},
}

// ==================== WebSocket SSH 终端 ====================

// SSHTerminal GET /api/bastion/ws/:ecs_id
// 通过 WebSocket 建立交互式 SSH 终端
func SSHTerminal(c *gin.Context) {
	ecsID, _ := strconv.Atoi(c.Param("asset_id")) // URL兼容，参数名不变
	uidStr, _ := c.Get("user_id")
	uid, _ := strconv.Atoi(uidStr.(string))
	role, _ := c.Get("role")

	// 获取 ECS 实例
	var ecs models.EcsInstance
	if err := models.DB.First(&ecs, ecsID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "实例不存在"})
		return
	}

	// 权限检查：管理员或有用户组资产授权
	if role != "admin" {
		var groupIDs []uint
		models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
		if len(groupIDs) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该资产"})
			return
		}
		var count int64
		models.DB.Model(&models.BastionGroupAsset{}).
			Where("ecs_id = ? AND group_id IN ?", ecsID, groupIDs).Count(&count)
		if count == 0 {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该资产"})
			return
		}
	}

	// 选择网络：默认公网，支持 公网/私网/弹性，指定网络无IP时自动回退
	network := c.DefaultQuery("network", "public")
	host := ecs.PublicIpAddress
	if network == "eip" && ecs.EipAddress != "" {
		host = ecs.EipAddress
	} else if network == "private" && ecs.PrivateIpAddress != "" {
		host = ecs.PrivateIpAddress
	}
	if host == "" {
		// 按优先级回退：弹性 -> 公网 -> 私网
		switch network {
		case "eip":
			if ecs.PublicIpAddress != "" {
				host = ecs.PublicIpAddress
			} else if ecs.PrivateIpAddress != "" {
				host = ecs.PrivateIpAddress
			}
		case "private":
			if ecs.PublicIpAddress != "" {
				host = ecs.PublicIpAddress
			}
		default: // public
			if ecs.PrivateIpAddress != "" {
				host = ecs.PrivateIpAddress
			}
		}
	}
	// SSH端口优先级：资产 ssh_port > 全局 bastion.default_ssh_port > 22
	port := ecs.SSHPort
	if port <= 0 || port > 65535 {
		port = config.GetBastionConfig().DefaultSSHPort
		if port <= 0 || port > 65535 {
			port = models.DefaultSSHPort
		}
		logger.Warn("bastion", "资产SSH端口为空或非法，回退全局默认端口",
			"ecs_id", ecsID, "ssh_port", ecs.SSHPort, "default_ssh_port", port)
	}
	logger.Debug("bastion", "准备SSH连接", "asset", ecs.InstanceName, "host", host, "port", port, "network", network, "public_ip", ecs.PublicIpAddress, "eip", ecs.EipAddress, "private_ip", ecs.PrivateIpAddress)
	if host == "" {
		logger.Warn("bastion", "资产无可用IP，WebSocket连接拒绝",
			"ecs_id", ecsID, "asset", ecs.InstanceName,
			"public_ip", ecs.PublicIpAddress, "eip", ecs.EipAddress, "private_ip", ecs.PrivateIpAddress,
			"network", network)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该资产没有IP地址，请先在资产管理中设置IP"})
		return
	}

	// 获取凭证
	credIDStr := c.DefaultQuery("credential_id", "0")
	credID, _ := strconv.Atoi(credIDStr)
	if credID == 0 {
		// 从用户组的凭证授权中获取凭证
		var groupIDs []uint
		models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
		if len(groupIDs) > 0 {
			var gc models.BastionGroupCredential
			models.DB.Where("group_id IN ?", groupIDs).Order("id ASC").First(&gc)
			credID = int(gc.CredentialID)
		}
	}
	if credID == 0 {
		logger.Warn("bastion", "未找到可用凭证，WebSocket连接拒绝",
			"ecs_id", ecsID, "user_id", uid, "cred_from_query", c.Query("credential_id"))
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "未找到可用的凭证"})
		return
	}

	var cred models.ServerCredential
	if err := models.DB.First(&cred, credID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "凭证不存在"})
		return
	}

	// 解密凭证
	password := ""
	privateKey := ""
	passphrase := ""
	if cred.Password != "" {
		password, _ = utils.Decrypt(cred.Password)
	}
	if cred.PrivateKey != "" {
		privateKey, _ = utils.Decrypt(cred.PrivateKey)
	}
	if cred.Passphrase != "" {
		passphrase, _ = utils.Decrypt(cred.Passphrase)
	}

	// 构建 SSH 认证配置
	var authMethods []bssh.AuthMethod
	if cred.AuthType == "private_key" && privateKey != "" {
		var signer bssh.Signer
		var err error
		if passphrase != "" {
			signer, err = bssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
		} else {
			signer, err = bssh.ParsePrivateKey([]byte(privateKey))
		}
		if err != nil {
			logger.Warn("bastion", "私钥解析失败，WebSocket连接拒绝",
				"ecs_id", ecsID, "cred_id", credID, "err", err)
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "私钥解析失败: " + err.Error()})
			return
		}
		authMethods = append(authMethods, bssh.PublicKeys(signer))
	}
	if password != "" {
		authMethods = append(authMethods, bssh.Password(password))
	}
	logger.Debug("bastion", "SSH认证配置", "auth_methods", len(authMethods), "has_password", password != "", "auth_type", cred.AuthType, "username", cred.Username, "cred_id", credID)
	if len(authMethods) == 0 {
		logger.Warn("bastion", "凭证无有效认证方式，WebSocket连接拒绝",
			"ecs_id", ecsID, "cred_id", credID, "auth_type", cred.AuthType)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "凭证无有效的认证方式"})
		return
	}

	sshConfig := &bssh.ClientConfig{
		User:            cred.Username,
		Auth:            authMethods,
		HostKeyCallback: bssh.InsecureIgnoreHostKey(), // 生产环境需替换为已知主机验证
		Timeout:         10 * time.Second,
	}

	// 先升级 WebSocket，再建立 SSH（这样 SSH 失败也能通过 WS 把错误传给前端）
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("bastion", "WebSocket升级失败",
			"err", err, "ecs_id", ecsID, "asset", ecs.InstanceName,
			"upgrade_header", c.GetHeader("Upgrade"),
			"connection_header", c.GetHeader("Connection"),
			"origin", c.GetHeader("Origin"))
		return
	}
	defer ws.Close()

	// 最大连接数检查
	if mgr := ssh.GetManager(); mgr.Count() >= config.GetBastionConfig().MaxSessions {
		sendWSMsg(ws, "error", fmt.Sprintf("连接数已满（上限 %d），请稍后再试", config.GetBastionConfig().MaxSessions))
		return
	}

	// 解析终端尺寸（供新建/复用会话时设定 pty 大小）
	cols, _ := strconv.Atoi(c.DefaultQuery("cols", "120"))
	rows, _ := strconv.Atoi(c.DefaultQuery("rows", "40"))

	// 建立或复用会话：带 session_id 且后端仍存在 → 复用同一持久化 shell（刷新复用）；否则新建
	sessionID := c.Query("session_id")
	sessionManager := ssh.GetManager()

	var sess *ssh.SessionInfo
	if sessionID != "" {
		sess = sessionManager.GetSession(sessionID)
	}
	if sess == nil {
		sess, err = sessionManager.CreateSession(uint(ecsID), ecs.InstanceName, host, port, sshConfig, cols, rows)
		if err != nil {
			logger.Error("bastion", "SSH连接失败", "err", err, "host", host, "port", port, "asset", ecs.InstanceName)
			sendWSMsg(ws, "error", err.Error())
			return
		}
		// 新建会话：shell 在会话内持久保活，不随 WS 断开而关闭（仅由超时回收或主动断开回收）
	}

	// 附着 WebSocket：回放已有输出缓冲，刷新后恢复之前的屏幕内容与历史
	sess.Attach(ws, cols, rows)

	// 发送会话信息给前端（含 session_id 供刷新复用、port 供前端标签展示实际连接端口）
	data := map[string]string{
		"type":       "connected",
		"session_id": sess.ID,
		"port":       strconv.Itoa(port),
	}
	d, _ := json.Marshal(data)
	ws.WriteMessage(websocket.TextMessage, d)
	logger.Debug("bastion", "已发送connected", "host", host, "session_id", sess.ID, "reused", sessionID != "")

	// WebSocket → SSH stdin（仅真实输入写入远端；心跳/resize 不计入活跃，因此空闲超时生效）
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		// 处理 JSON 控制消息（窗口大小变更 / 心跳 ping）
		var ctrl struct {
			Type string `json:"type"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(msg, &ctrl) == nil {
			if ctrl.Type == "resize" {
				sess.Resize(ctrl.Cols, ctrl.Rows)
			}
			// ping 等控制消息不写入 SSH、不刷新活跃时间，仅用于保活 WS 传输层
			continue
		}
		// 普通按键数据：写入远端 shell（真实输入，计入活跃时间）
		sess.WriteInput(msg)
	}

	// WebSocket 断开：仅 detach，shell 继续在后台运行（供刷新后复用同一会话）
	sess.Detach(ws)
	logger.Debug("bastion", "WebSocket断开（会话保活）", "session_id", sess.ID)
}

func sendWSMsg(ws *websocket.Conn, msgType, msg string) {
	d, _ := json.Marshal(map[string]string{"type": msgType, "msg": msg})
	ws.WriteMessage(websocket.TextMessage, d)
}

// ==================== 会话断开 ====================

// DisconnectSession POST /api/bastion/disconnect
func DisconnectSession(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	ssh.GetManager().CloseSession(req.SessionID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已断开"})
}

// ==================== 文件上传下载 ====================

// UploadFile POST /api/bastion/upload/:ecs_id
func UploadFile(c *gin.Context) {
	_ = c.Param("asset_id")
	sessionID := c.Query("session_id")
	remotePath := c.Query("path")

	if sessionID == "" || remotePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少session_id或path参数"})
		return
	}

	sess := ssh.GetManager().GetSession(sessionID)
	if sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH会话不存在或已过期"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "读取文件失败"})
		return
	}
	defer file.Close()

	fullPath := remotePath
	if remotePath[len(remotePath)-1] != '/' {
		fullPath += "/"
	}
	fullPath += header.Filename

	if err := ssh.UploadFile(sess.Client, file, fullPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "上传失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": gin.H{"path": fullPath}})
}

// DownloadFile GET /api/bastion/download/:asset_id
func DownloadFile(c *gin.Context) {
	_ = c.Param("asset_id")
	sessionID := c.Query("session_id")
	filePath := c.Query("path")

	if sessionID == "" || filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少session_id或path参数"})
		return
	}

	sess := ssh.GetManager().GetSession(sessionID)
	if sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH会话不存在或已过期"})
		return
	}

	reader, err := ssh.DownloadFile(sess.Client, filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "下载失败: " + err.Error()})
		return
	}
	defer reader.Close()

	// 提取文件名
	filename := filePath
	if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
		filename = filePath[idx+1:]
	}

	// 设置文件大小，便于前端显示下载进度百分比；同时声明支持 Range（以备续传）
	if size, e := ssh.StatSize(sess.Client, filePath); e == nil && size >= 0 {
		c.Header("Content-Length", strconv.FormatInt(size, 10))
		c.Header("Accept-Ranges", "bytes")
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")
	io.Copy(c.Writer, reader)
}

// ListDir GET /api/bastion/ls/:asset_id
func ListDir(c *gin.Context) {
	_ = c.Param("asset_id")
	sessionID := c.Query("session_id")
	dirPath := c.DefaultQuery("path", ".")

	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少session_id参数"})
		return
	}

	sess := ssh.GetManager().GetSession(sessionID)
	if sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH会话不存在或已过期"})
		return
	}

	files, realPath, err := ssh.ListFiles(sess.Client, dirPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "列目录失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"path": realPath, "files": files}})
}

// DeleteFile DELETE /api/bastion/rm/:asset_id
// 删除远端文件或目录（目录递归删除）。
// 注意：远端文件名可能为非 UTF-8 编码（如 GBK），经 JSON 序列化后原始字节会丢失、
// 无法可靠通过 URL 回传，导致按 path 删除时 Stat 找不到文件（500）。
// 因此这里改用 parent(父目录) + idx(在目录列表中的序号) 定位：由后端重新 ReadDir
// 取回原始 entry.Name() 字节构造目标路径，彻底规避乱码文件名的编码往返失真。
func DeleteFile(c *gin.Context) {
	_ = c.Param("asset_id")
	sessionID := c.Query("session_id")
	parent := c.Query("parent")
	idxStr := c.Query("idx")

	if sessionID == "" || parent == "" || idxStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少session_id、parent或idx参数"})
		return
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "idx参数无效"})
		return
	}

	sess := ssh.GetManager().GetSession(sessionID)
	if sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH会话不存在或已过期"})
		return
	}

	sftpClient, err := sftp.NewClient(sess.Client)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建SFTP客户端失败: " + err.Error()})
		return
	}
	defer sftpClient.Close()

	// 父目录为空或 "." 时解析为登录后的家目录（与 ListFiles 行为一致）
	realParent := parent
	if realParent == "" || realParent == "." {
		if wd, e := sftpClient.Getwd(); e == nil && wd != "" {
			realParent = wd
		}
	}

	entries, err := sftpClient.ReadDir(realParent)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取父目录失败: " + err.Error()})
		return
	}
	// 必须与 ListFiles 使用完全相同的排序，否则前端 fileList 的 idx 与这里取回的顺序错位 → 删错文件
	ssh.SortEntries(entries)
	if idx >= len(entries) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "idx超出目录范围，请刷新后重试"})
		return
	}

	// 用原始 entry.Name() 字节拼接路径（保留非 UTF-8 编码的文件名）
	target := realParent
	if target[len(target)-1] != '/' {
		target += "/"
	}
	target += entries[idx].Name()

	if err := ssh.RemovePathOnClient(sftpClient, target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// UploadStatus GET /api/bastion/upload-status/:asset_id
// 查询远端已存在的同名文件大小，供前端实现断点续传（返回已上传字节数，文件不存在返回 0）。
func UploadStatus(c *gin.Context) {
	_ = c.Param("asset_id")
	sessionID := c.Query("session_id")
	parent := c.Query("path")
	filename := c.Query("filename")

	if sessionID == "" || filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少session_id或filename参数"})
		return
	}
	sess := ssh.GetManager().GetSession(sessionID)
	if sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH会话不存在或已过期"})
		return
	}

	// 解析父目录（空/ "." 解析为家目录，与 ListFiles 一致）
	realParent := parent
	if realParent == "" || realParent == "." {
		if wd, e := func() (string, error) {
			sc, e := sftp.NewClient(sess.Client)
			if e != nil {
				return "", e
			}
			defer sc.Close()
			return sc.Getwd()
		}(); e == nil && wd != "" {
			realParent = wd
		}
	}
	fullPath := realParent
	if fullPath[len(fullPath)-1] != '/' {
		fullPath += "/"
	}
	fullPath += filename

	size, err := ssh.StatSize(sess.Client, fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败: " + err.Error()})
		return
	}
	if size < 0 {
		size = 0
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"uploaded": size, "path": fullPath}})
}

// UploadChunk POST /api/bastion/upload-chunk/:asset_id
// 分片上传单个分片：body 为二进制分片数据，通过 offset 定位写入，支持断点续传。
// 末片可带 md5 参数触发服务端 MD5 校验（与前端计算的整文件 MD5 比对，确保完整性）。
func UploadChunk(c *gin.Context) {
	_ = c.Param("asset_id")
	sessionID := c.Query("session_id")
	parent := c.Query("path")
	filename := c.Query("filename")
	offsetStr := c.Query("offset")
	md5sum := c.Query("md5") // 仅末片传，触发校验

	if sessionID == "" || filename == "" || offsetStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少session_id、filename或offset参数"})
		return
	}
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "offset参数无效"})
		return
	}
	sess := ssh.GetManager().GetSession(sessionID)
	if sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH会话不存在或已过期"})
		return
	}

	// 解析父目录（空/ "." 解析为家目录）
	realParent := parent
	if realParent == "" || realParent == "." {
		if wd, e := func() (string, error) {
			sc, e := sftp.NewClient(sess.Client)
			if e != nil {
				return "", e
			}
			defer sc.Close()
			return sc.Getwd()
		}(); e == nil && wd != "" {
			realParent = wd
		}
	}
	fullPath := realParent
	if fullPath[len(fullPath)-1] != '/' {
		fullPath += "/"
	}
	fullPath += filename

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取分片失败: " + err.Error()})
		return
	}

	if err := ssh.UploadChunk(sess.Client, fullPath, offset, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "上传分片失败: " + err.Error()})
		return
	}

	// 末片：若传了 md5，则校验整文件 MD5
	if md5sum != "" {
		remoteMD5, e := ssh.ComputeMD5(sess.Client, fullPath)
		if e != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "校验MD5失败: " + e.Error()})
			return
		}
		if remoteMD5 != strings.ToLower(md5sum) {
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "MD5校验不一致，文件可能损坏", "data": gin.H{"expected": md5sum, "actual": remoteMD5}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传完成且MD5校验通过", "data": gin.H{"path": fullPath, "md5": remoteMD5}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "分片已接收", "data": gin.H{"offset": offset + int64(len(body))}})
}

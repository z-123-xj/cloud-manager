package bastion

// ============================================================================
// 公共 SSH 原语：资产授权校验 + 凭证解密 + 连接信息组装 + 一次性远程执行
//
// 日志排查（HTTP handler）与批量执行（后台任务）共用同一套逻辑，
// 避免两处各自实现权限校验与凭证解密。
// ============================================================================

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cloud-manager-backend/internal/config"
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"

	bssh "golang.org/x/crypto/ssh"
)

// logSSHSpec 一台目标机的 SSH 连接信息（不含明文凭证，只有已构造好的 AuthMethod）
type logSSHSpec struct {
	EcsID       uint64
	AssetName   string
	Host        string
	Port        int
	User        string
	AuthMethods []bssh.AuthMethod
}

// sshSpecError 携带 HTTP 状态码，使日志排查接口保持原有的 403/404 行为
type sshSpecError struct {
	status int
	msg    string
}

func (e *sshSpecError) Error() string { return e.msg }

func newSSHSpecError(status int, format string, args ...interface{}) error {
	return &sshSpecError{status: status, msg: fmt.Sprintf(format, args...)}
}

// resolveSSHSpec 组装一台目标机的连接信息。
// 校验顺序：资产存在 → 用户组资产授权 → 可用 IP → 端口 → 凭证（授权/解密）→ 认证方式。
// 不依赖 gin.Context，因此后台批量任务也可以复用。
func resolveSSHSpec(uid int, role string, ecsID int, network string, credID int) (*logSSHSpec, error) {
	var ecs models.EcsInstance
	if err := models.DB.First(&ecs, ecsID).Error; err != nil {
		return nil, newSSHSpecError(http.StatusNotFound, "实例不存在")
	}
	if role != "admin" {
		var groupIDs []uint
		models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
		if len(groupIDs) == 0 {
			return nil, newSSHSpecError(http.StatusForbidden, "无权访问该资产")
		}
		var count int64
		models.DB.Model(&models.BastionGroupAsset{}).
			Where("ecs_id = ? AND group_id IN ?", ecsID, groupIDs).Count(&count)
		if count == 0 {
			return nil, newSSHSpecError(http.StatusForbidden, "无权访问该资产")
		}
	}

	if network == "" {
		network = "public"
	}
	host := ecs.PublicIpAddress
	if network == "eip" && ecs.EipAddress != "" {
		host = ecs.EipAddress
	} else if network == "private" && ecs.PrivateIpAddress != "" {
		host = ecs.PrivateIpAddress
	}
	if host == "" {
		if ecs.EipAddress != "" {
			host = ecs.EipAddress
		} else if ecs.PrivateIpAddress != "" {
			host = ecs.PrivateIpAddress
		}
	}
	if host == "" {
		return nil, newSSHSpecError(http.StatusBadRequest, "该资产没有可用IP，请先在资产管理中设置IP")
	}
	port := ecs.SSHPort
	if port <= 0 || port > 65535 {
		port = config.GetBastionConfig().DefaultSSHPort
		if port <= 0 || port > 65535 {
			port = models.DefaultSSHPort
		}
	}

	if credID == 0 {
		if role == "admin" {
			// 管理员可用全部凭证且不依赖用户组，取凭证下拉的第一条（id DESC）作为默认
			var first models.ServerCredential
			if err := models.DB.Select("id").Order("id DESC").First(&first).Error; err == nil {
				credID = int(first.ID)
			}
		} else {
			// 普通用户：取所在用户组已授权的第一条凭证
			var groupIDs []uint
			models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
			if len(groupIDs) > 0 {
				var gc models.BastionGroupCredential
				models.DB.Where("group_id IN ?", groupIDs).Order("id ASC").First(&gc)
				credID = int(gc.CredentialID)
			}
		}
	}
	if credID == 0 {
		return nil, newSSHSpecError(http.StatusBadRequest, "未找到可用的凭证，请先在用户组管理中授权凭证")
	}
	var cred models.ServerCredential
	if err := models.DB.First(&cred, credID).Error; err != nil {
		return nil, newSSHSpecError(http.StatusNotFound, "凭证不存在")
	}
	password, privateKey, passphrase := "", "", ""
	if cred.Password != "" {
		password, _ = utils.Decrypt(cred.Password)
	}
	if cred.PrivateKey != "" {
		privateKey, _ = utils.Decrypt(cred.PrivateKey)
	}
	if cred.Passphrase != "" {
		passphrase, _ = utils.Decrypt(cred.Passphrase)
	}
	var auths []bssh.AuthMethod
	if cred.AuthType == "private_key" && privateKey != "" {
		var signer bssh.Signer
		var err error
		if passphrase != "" {
			signer, err = bssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
		} else {
			signer, err = bssh.ParsePrivateKey([]byte(privateKey))
		}
		if err != nil {
			return nil, newSSHSpecError(http.StatusBadRequest, "私钥解析失败: %s", err.Error())
		}
		auths = append(auths, bssh.PublicKeys(signer))
	}
	if password != "" {
		auths = append(auths, bssh.Password(password))
	}
	if len(auths) == 0 {
		return nil, newSSHSpecError(http.StatusBadRequest, "凭证无有效的认证方式")
	}
	return &logSSHSpec{
		EcsID:       ecs.ID,
		AssetName:   ecs.InstanceName,
		Host:        host,
		Port:        port,
		User:        cred.Username,
		AuthMethods: auths,
	}, nil
}

func (s *logSSHSpec) dial() (*bssh.Client, error) {
	cfg := &bssh.ClientConfig{
		User:            s.User,
		Auth:            s.AuthMethods,
		HostKeyCallback: bssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	return bssh.Dial("tcp", fmt.Sprintf("%s:%d", s.Host, s.Port), cfg)
}

// shellCommand 解释器白名单：脚本内容经 stdin 传入，不允许任意命令拼接
var shellCommands = map[string]string{
	"sh":      "sh -s",
	"bash":    "bash -s",
	"python3": "python3 -",
	"python":  "python -",
}

func shellCommand(shell string) (string, error) {
	if shell == "" {
		shell = "sh"
	}
	cmd, ok := shellCommands[shell]
	if !ok {
		return "", fmt.Errorf("不支持的解释器: %s", shell)
	}
	return cmd, nil
}

func supportedShells() []string {
	return []string{"sh", "bash", "python3", "python"}
}

// runRemoteCtx 在远端执行一段脚本，支持超时与任务取消。
// 超时/取消时关闭 session 以中断远端进程，不返回部分输出（避免与仍在写缓冲区的 goroutine 竞争）。
func runRemoteCtx(ctx context.Context, client *bssh.Client, script string, command string, timeout time.Duration) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdin = strings.NewReader(script)
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-time.After(timeout):
		session.Close()
		return "", "", fmt.Errorf("远端执行超时（>%s）", timeout)
	case <-ctx.Done():
		session.Close()
		return "", "", ctx.Err()
	}
}

// exitCodeOf 提取远端退出码；无法取得时返回 nil（区别于退出码 0）
func exitCodeOf(err error) *int {
	if err == nil {
		code := 0
		return &code
	}
	var ee *bssh.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitStatus()
		return &code
	}
	return nil
}

// truncateBytes 按 UTF-8 边界安全截断，避免把超长输出写进 TEXT 字段
func truncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + fmt.Sprintf("\n... [输出已截断，原始共 %d 字节]", len(s))
}

// internal/captcha 图形验证码：基于 github.com/mojocn/base64Captcha 生成
// 清晰易认的字母验证码（chromohv.ttf 无衬线字体 + 轻量噪点/干扰线），
// 答案保存在库内置内存存储中（10 分钟 TTL），验证后一次性失效。
// 说明：频率限制记录走 MySQL（见 login/ratelimit.go），此处仅缓存验证码答案。
package captcha

import (
	"image/color"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
)

// 去掉易混淆字符：I O 0 1
const captchaSource = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var (
	// 驱动：高40 宽132，噪点2，横+竖干扰线，4位大写字母/数字
	captchaDriver = base64Captcha.NewDriverString(
		40, 132,
		2,
		base64Captcha.OptionShowHollowLine|base64Captcha.OptionShowSlimeLine,
		4,
		captchaSource,
		&color.RGBA{R: 255, G: 255, B: 255, A: 0}, // 全透明背景：PNG自带白底会盖住前端CSS的半透明效果
		nil,
		[]string{"chromohv.ttf"},
	)
	captchaInstance = base64Captcha.NewCaptcha(captchaDriver, base64Captcha.DefaultMemStore)
)

// Generate 生成验证码，返回 id 与 base64 PNG 图片串
func Generate() (id string, imgBase64 string, err error) {
	id, b64, _, err := captchaInstance.Generate()
	return id, b64, err
}

// Verify 校验验证码（一次性，验证后立即失效）
func Verify(id, code string) bool {
	return base64Captcha.DefaultMemStore.Verify(id, code, true)
}

// GenerateHandler gin 接口：GET /api/captcha
func GenerateHandler(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	id, img, err := Generate()
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "验证码生成失败"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": gin.H{"captcha_id": id, "image": img}})
}

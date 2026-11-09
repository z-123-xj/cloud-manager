package user

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Adduser(c *gin.Context) {
	fmt.Println("添加一个用户")
}

// GetUsers 查询用户列表（对应 GET /user?keyword=&page=1&size=10）
// GET /api/user/                              # 查全部，第1+每页10条
// GET /api/user/?keyword=张三                  # 搜索"张三"
// GET /api/user/?keyword=zhang&page=2&size=20  # 搜索+第2页+每页20条
func GetUsers(c *gin.Context) {
	// 分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	// 搜索关键词（按用户名或邮箱模糊匹配）
	keyword := c.Query("keyword")

	var userList []models.User
	var total int64

	query := models.DB.Model(&models.User{})
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("username LIKE ? OR email LIKE ?", like, like)
	}

	// 查总数
	query.Count(&total)

	// 分页查询，隐藏密码字段
	query.Offset((page - 1) * size).Limit(size).Find(&userList)
	for i := range userList {
		userList[i].Password = ""
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":  userList,
			"total": total,
			"page":  page,
			"size":  size,
		},
	})
}

// GetUser 查询单个用户（对应 GET /user/:id）
func GetUser(c *gin.Context) {
	var user models.User
	if err := models.DB.Where("id = ?", c.Param("id")).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询用户失败"})
		return
	}
	user.Password = ""
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": user,
	})
}

// CreateUser 创建用户（对应 POST /users）
func CreateUser(c *gin.Context) {
	// 1. 绑定请求参数
	var req struct {
		Username string `json:"username" binding:"required,min=2,max=50"`
		Password string `json:"password" binding:"required,min=6"`
		Email    string `json:"email" binding:"required,email"`
		Role     string `json:"role" binding:"required,oneof=admin user"`
		RealName string `json:"real_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// 参数校验失败
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "参数错误：" + err.Error(),
		})
		return
	}

	// 2. 检查用户名是否已被占用
	var count int64
	models.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "用户名已被占用",
		})
		return
	}

	// 3. 检查邮箱是否已被占用
	models.DB.Model(&models.User{}).Where("email = ?", req.Email).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "邮箱已被注册",
		})
		return
	}

	// 4. 加密密码
	hashedPassword, err := utils.Encrypt(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "密码加密失败",
		})
		return
	}

	// 5. 创建用户对象
	user := models.User{
		Username: req.Username,
		Password: hashedPassword, // 加密后的密码
		Email:    req.Email,
		Role:     req.Role,
		RealName: req.RealName,
		AddTime:  time.Now().Format("2006-01-02 15:04:05"),
	}

	// 6. 插入数据库
	if err := models.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "创建用户失败：" + err.Error(),
		})
		return
	}

	// 7. 返回结果（隐藏密码）
	user.Password = ""
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "创建成功",
		"data": user,
	})
}

// UpdateUser 更新用户（对应 PUT /users/:id）
func UpdateUser(c *gin.Context) {
    id := c.Param("id")
    
    // 1. 参数绑定（允许部分更新，使用 omitempty）
    var req struct {
        Username string `json:"username" binding:"omitempty,min=2,max=50"`
        Password string `json:"password" binding:"omitempty,min=6"`
        Email    string `json:"email" binding:"omitempty,email"`
        Role     string `json:"role" binding:"omitempty,oneof=admin user readonly"`
        RealName string `json:"real_name"`
    }
    
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "参数错误：" + err.Error(),
        })
        return
    }
    
    // 2. 检查用户是否存在
    var existingUser models.User
    if err := models.DB.Where("id = ?", id).First(&existingUser).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{
                "code": 404,
                "msg":  "用户不存在",
            })
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{
            "code": 500,
            "msg":  "查询用户失败",
        })
        return
    }
    
    // 3. 构建更新数据（与现有数据对比，只更新真正变化的字段）
    updateData := make(map[string]interface{})
    
    if req.Username != "" && req.Username != existingUser.Username {
        // 检查新用户名是否被其他用户占用
        var count int64
        models.DB.Model(&models.User{}).Where("username = ? AND id != ?", req.Username, id).Count(&count)
        if count > 0 {
            c.JSON(http.StatusBadRequest, gin.H{
                "code": 400,
                "msg":  "用户名已被其他用户占用",
            })
            return
        }
        updateData["username"] = req.Username
    }
    
    if req.Email != "" && req.Email != existingUser.Email {
        // 检查新邮箱是否被其他用户占用
        var count int64
        models.DB.Model(&models.User{}).Where("email = ? AND id != ?", req.Email, id).Count(&count)
        if count > 0 {
            c.JSON(http.StatusBadRequest, gin.H{
                "code": 400,
                "msg":  "邮箱已被其他用户注册",
            })
            return
        }
        updateData["email"] = req.Email
    }
    
    if req.Password != "" {
        // 密码不对比（密文每次不同），传了就更新
        hashedPassword, err := utils.Encrypt(req.Password)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{
                "code": 500,
                "msg":  "密码加密失败",
            })
            return
        }
        updateData["password"] = hashedPassword
    }

    if req.Role != "" && req.Role != existingUser.Role {
        updateData["role"] = req.Role
    }

    // real_name 允许传空字符串来清空
    if req.RealName != "" && req.RealName != existingUser.RealName {
        updateData["real_name"] = req.RealName
    }
    
    // 4. 如果没有字段需要更新
    if len(updateData) == 0 {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "没有需要更新的字段",
        })
        return
    }
    
    // 5. 执行更新（只更新指定字段，不更新 AddTime）
    if err := models.DB.Model(&models.User{}).Where("id = ?", id).Updates(updateData).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "code": 500,
            "msg":  "更新用户失败：" + err.Error(),
        })
        return
    }
    
    // 6. 查询更新后的用户信息
    var updatedUser models.User
    models.DB.Where("id = ?", id).First(&updatedUser)
    updatedUser.Password = "" // 隐藏密码
    
    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "msg":  "更新成功",
        "data": updatedUser,
    })
}

// DeleteUser 删除用户（对应 DELETE /users/:id）
// 不允许删除最后一个管理员账号
func DeleteUser(c *gin.Context) {
    id := c.Param("id")
    
    // 1. 检查用户是否存在
    var user models.User
    if err := models.DB.Where("id = ?", id).First(&user).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{
                "code": 404,
                "msg":  "用户不存在",
            })
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{
            "code": 500,
            "msg":  "查询用户失败",
        })
        return
    }

    // 2. 如果删除的是管理员，检查是否还有其它管理员
    if user.Role == models.RoleAdmin {
        var adminCount int64
        models.DB.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&adminCount)
        if adminCount <= 1 {
            c.JSON(http.StatusBadRequest, gin.H{
                "code": 400,
                "msg":  "不能删除最后一个管理员，系统将无法管理",
            })
            return
        }
    }
    
    // 3. 执行删除
    if err := models.DB.Where("id = ?", id).Delete(&models.User{}).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "code": 500,
            "msg":  "删除用户失败：" + err.Error(),
        })
        return
    }
    
    // 4. 返回成功
    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "msg":  "删除成功",
        "data": gin.H{
            "id": id,
        },
    })
}

// UnlockUser 解锁被锁定的用户  PUT /api/user/:id/unlock（管理员）
func UnlockUser(c *gin.Context) {
    id := c.Param("id")

    var user models.User
    if err := models.DB.Where("id = ?", id).First(&user).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询用户失败"})
        return
    }

    if !user.Locked {
        c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "该账号未锁定"})
        return
    }

    models.DB.Model(&user).Updates(map[string]interface{}{
        "login_attempts": 0,
        "locked":         false,
    })

    c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "账号已解锁"})
}

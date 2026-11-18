package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/model"
)

func GetGroups(c *gin.Context) {
	// 返回真实存在的分组并集（价格配置 / 渠道 / 用户），
	// 避免只取分组价格表导致管理端选不到已有分组。
	groupNames := model.GetAllGroupNames()
	if len(groupNames) == 0 {
		groupNames = []string{"default"}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}
package handler

import (
	"net/http"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListDistributors(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, err := uuid.Parse(companyIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id inválido"})
		return
	}

	var distributors []model.Distributor
	result := postgres.GORM.Where("company_id = ?", companyID).Order("created_at asc").Find(&distributors)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, distributors)
}

func CreateDistributor(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, err := uuid.Parse(companyIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id inválido"})
		return
	}

	var input struct {
		Name  string `json:"name" binding:"required"`
		Phone string `json:"phone" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	distributor := model.Distributor{
		CompanyID: companyID,
		Name:      input.Name,
		Phone:     input.Phone,
	}

	if err := postgres.GORM.Create(&distributor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, distributor)
}

func DeleteDistributor(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, err := uuid.Parse(companyIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id inválido"})
		return
	}

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}

	result := postgres.GORM.Where("company_id = ? AND id = ?", companyID, id).Delete(&model.Distributor{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Distribuidor removido com sucesso"})
}

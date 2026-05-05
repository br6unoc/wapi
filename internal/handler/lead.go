package handler

import (
	"log"
	"net/http"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListLeads(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, err := uuid.Parse(companyIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id inválido"})
		return
	}

	var leads []model.Lead
	result := postgres.GORM.Where("company_id = ?", companyID).Order("created_at desc").Find(&leads)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, leads)
}

func ImportLeads(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, err := uuid.Parse(companyIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id inválido"})
		return
	}

	var input struct {
		ListName string       `json:"list_name"`
		Leads    []model.Lead `json:"leads"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Prepara os leads para inserção em lote
	for i := range input.Leads {
		input.Leads[i].ID = uuid.New()
		input.Leads[i].CompanyID = companyID
		input.Leads[i].ListName = input.ListName
		input.Leads[i].Status = "PENDING"
	}

	if err := postgres.GORM.Create(&input.Leads).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Leads importados com sucesso", "count": len(input.Leads)})
}

func DeleteList(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, _ := uuid.Parse(companyIDStr.(string))
	listName := c.Param("name")

	result := postgres.GORM.Where("company_id = ? AND list_name = ?", companyID, listName).Delete(&model.Lead{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Lista excluída com sucesso", "deleted": result.RowsAffected})
}
func ListQualifiedLeads(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, err := uuid.Parse(companyIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id inválido"})
		return
	}

	log.Printf("[DEBUG-QUALIFIED] Buscando para CompanyID: %s", companyID)
	var leads []model.Lead
	// Busca apenas os qualificados
	result := postgres.GORM.Where("company_id = ? AND is_qualified = ?", companyID, true).Order("qualified_at desc").Find(&leads)
	if result.Error != nil {
		log.Printf("[DEBUG-QUALIFIED-ERROR] %v", result.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	log.Printf("[DEBUG-QUALIFIED] Encontrados %d leads", len(leads))

	c.JSON(http.StatusOK, leads)
}

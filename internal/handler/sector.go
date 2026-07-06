package handler

import (
	"net/http"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListSectors(c *gin.Context) {
	companyID := currentCompanyID(c)
	rows, err := postgres.DB.Query(`
		SELECT s.id, s.name, COUNT(us.user_id) AS users_count
		FROM sectors s
		LEFT JOIN user_sectors us ON us.sector_id = s.id
		WHERE s.company_id = $1
		GROUP BY s.id, s.name
		ORDER BY s.name ASC
	`, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Sector struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		UsersCount int    `json:"users_count"`
	}

	sectors := make([]Sector, 0)
	for rows.Next() {
		var s Sector
		if err := rows.Scan(&s.ID, &s.Name, &s.UsersCount); err != nil {
			continue
		}
		sectors = append(sectors, s)
	}
	c.JSON(http.StatusOK, sectors)
}

func CreateSector(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}

	companyID := currentCompanyID(c)
	id := uuid.New().String()
	_, err := postgres.DB.Exec(`
		INSERT INTO sectors (id, company_id, name) VALUES ($1, $2, $3)
	`, id, companyID, req.Name)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "nome já existe nesta empresa"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "users_count": 0})
}

func UpdateSector(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}

	res, err := postgres.DB.Exec(`
		UPDATE sectors SET name = $1 WHERE id = $2 AND company_id = $3
	`, req.Name, id, companyID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "nome já existe"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "setor não encontrado"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "setor atualizado"})
}

func DeleteSector(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	_, err := postgres.DB.Exec(`DELETE FROM sectors WHERE id = $1 AND company_id = $2`, id, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover setor"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "setor removido"})
}

package handler

import (
	"net/http"

	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

func APIListProducts(c *gin.Context) {
	companyID := currentCompanyID(c)
	rows, err := postgres.DB.Query(
		`SELECT id, name, description, COALESCE(price::text,''), active, created_at
		 FROM products WHERE company_id = $1 ORDER BY created_at ASC`,
		companyID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Product struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Price       string `json:"price"`
		Active      bool   `json:"active"`
		CreatedAt   string `json:"created_at"`
	}
	products := make([]Product, 0)
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Active, &p.CreatedAt); err != nil {
			continue
		}
		products = append(products, p)
	}
	c.JSON(http.StatusOK, products)
}

func APICreateProduct(c *gin.Context) {
	companyID := currentCompanyID(c)
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Description string  `json:"description"`
		Price       *string `json:"price"`
		Active      *bool   `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	var id string
	err := postgres.DB.QueryRow(
		`INSERT INTO products (company_id, name, description, price, active)
		 VALUES ($1, $2, $3, NULLIF($4,'')::decimal, $5) RETURNING id`,
		companyID, req.Name, req.Description, strOrEmpty(req.Price), active,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func APIUpdateProduct(c *gin.Context) {
	companyID := currentCompanyID(c)
	id := c.Param("id")
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Description string  `json:"description"`
		Price       *string `json:"price"`
		Active      *bool   `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	res, err := postgres.DB.Exec(
		`UPDATE products SET name=$1, description=$2, price=NULLIF($3,'')::decimal, active=$4
		 WHERE id=$5 AND company_id=$6`,
		req.Name, req.Description, strOrEmpty(req.Price), active, id, companyID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "produto não encontrado"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func APIDeleteProduct(c *gin.Context) {
	companyID := currentCompanyID(c)
	id := c.Param("id")
	postgres.DB.Exec(`DELETE FROM products WHERE id = $1 AND company_id = $2`, id, companyID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

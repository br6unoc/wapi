package handler

import (
	"net/http"

	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

type TagRow struct {
	ID    string
	Name  string
	Color string
}

func WebTags(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	companyID := currentCompanyID(c)

	rows, _ := postgres.DB.Query(`
		SELECT id, name, color FROM contact_tags
		WHERE company_id = $1 ORDER BY name
	`, companyID)
	defer rows.Close()

	var tags []TagRow
	for rows.Next() {
		var t TagRow
		rows.Scan(&t.ID, &t.Name, &t.Color)
		tags = append(tags, t)
	}

	render(c, http.StatusOK, "tags.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
		"Tags":     tags,
	})
}

// API

func ListTags(c *gin.Context) {
	companyID := currentCompanyID(c)
	rows, err := postgres.DB.Query(`SELECT id, name, color FROM contact_tags WHERE company_id = $1 ORDER BY name`, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type T struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	var out []T
	for rows.Next() {
		var t T
		rows.Scan(&t.ID, &t.Name, &t.Color)
		out = append(out, t)
	}
	if out == nil {
		out = []T{}
	}
	c.JSON(http.StatusOK, out)
}

func CreateTag(c *gin.Context) {
	companyID := currentCompanyID(c)
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome obrigatório"})
		return
	}
	if body.Color == "" {
		body.Color = "#6366f1"
	}
	var id string
	err := postgres.DB.QueryRow(`
		INSERT INTO contact_tags (company_id, name, color) VALUES ($1, $2, $3) RETURNING id
	`, companyID, body.Name, body.Color).Scan(&id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "tag já existe"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "name": body.Name, "color": body.Color})
}

func UpdateTag(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	c.ShouldBindJSON(&body)
	_, err := postgres.DB.Exec(`
		UPDATE contact_tags SET name = $1, color = $2
		WHERE id = $3 AND company_id = $4
	`, body.Name, body.Color, id, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func DeleteTag(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)
	postgres.DB.Exec(`DELETE FROM contact_tags WHERE id = $1 AND company_id = $2`, id, companyID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Vínculos contato ↔ tag

func ListContactTagLinks(c *gin.Context) {
	companyID := currentCompanyID(c)
	rows, err := postgres.DB.Query(`
		SELECT ctl.contact_id, ctl.tag_id
		FROM contact_tag_links ctl
		JOIN contact_tags ct ON ct.id = ctl.tag_id
		WHERE ct.company_id = $1
	`, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type Link struct {
		ContactID string `json:"contact_id"`
		TagID     string `json:"tag_id"`
	}
	var out []Link
	for rows.Next() {
		var l Link
		rows.Scan(&l.ContactID, &l.TagID)
		out = append(out, l)
	}
	if out == nil {
		out = []Link{}
	}
	c.JSON(http.StatusOK, out)
}

func AddContactTag(c *gin.Context) {
	contactID := c.Param("id")
	tagID := c.Param("tagId")
	postgres.DB.Exec(`INSERT INTO contact_tag_links (contact_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, contactID, tagID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func RemoveContactTag(c *gin.Context) {
	contactID := c.Param("id")
	tagID := c.Param("tagId")
	postgres.DB.Exec(`DELETE FROM contact_tag_links WHERE contact_id = $1 AND tag_id = $2`, contactID, tagID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

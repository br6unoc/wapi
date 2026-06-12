package handler

import (
	"context"
	"net/http"

	"botwapp/internal/instance"
	"botwapp/store/postgres"

	waTypes "go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/gin-gonic/gin"
)

type ContactView struct {
	ID             string
	Phone          string
	Name           string
	InstanceName   string
	FirstContactAt string
	LastContactAt  string
	PurchaseCount  int
	IsBlocked      bool
}

func WebContacts(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	companyID := currentCompanyID(c)

	rows, err := postgres.DB.Query(`
		SELECT c.id, c.phone, c.name, i.name,
		       TO_CHAR(c.created_at, 'DD/MM/YYYY HH24:MI'),
		       TO_CHAR(c.last_contact_at, 'DD/MM/YYYY HH24:MI'),
		       c.purchase_count, c.is_blocked
		FROM contacts c
		JOIN instances i ON i.id = c.instance_id
		WHERE i.company_id = $1
		ORDER BY c.last_contact_at DESC NULLS LAST
		LIMIT 500
	`, companyID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Erro ao carregar contatos")
		return
	}
	defer rows.Close()

	var contacts []ContactView
	for rows.Next() {
		var ct ContactView
		var lastAt *string
		rows.Scan(&ct.ID, &ct.Phone, &ct.Name, &ct.InstanceName, &ct.FirstContactAt, &lastAt, &ct.PurchaseCount, &ct.IsBlocked)
		if lastAt != nil {
			ct.LastContactAt = *lastAt
		}
		contacts = append(contacts, ct)
	}

	render(c, http.StatusOK, "contacts.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
		"Contacts": contacts,
	})
}

func apiSetBlock(c *gin.Context, block bool) {
	id := c.Param("id")
	companyID := currentCompanyID(c)
	ct, err := postgres.GetContactBasic(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}
	inst, ok := instance.Global.GetByName(ct.InstanceName)
	if !ok || inst.CompanyID != companyID {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}
	if err := postgres.SetContactBlocked(id, block); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inst.WAClient != nil && inst.WAClient.IsConnected() {
		action := waEvents.BlocklistChangeActionBlock
		if !block {
			action = waEvents.BlocklistChangeActionUnblock
		}
		jid, jidErr := waTypes.ParseJID(ct.Phone + "@s.whatsapp.net")
		if jidErr == nil {
			inst.WAClient.UpdateBlocklist(context.Background(), jid, action)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "blocked": block})
}

func APIContactBlock(c *gin.Context)   { apiSetBlock(c, true) }
func APIContactUnblock(c *gin.Context) { apiSetBlock(c, false) }

func contactBelongsToCompany(contactID, companyID string) bool {
	var count int
	postgres.DB.QueryRow(
		`SELECT COUNT(*) FROM contacts ct JOIN instances i ON i.id = ct.instance_id
		 WHERE ct.id = $1 AND i.company_id = $2`,
		contactID, companyID,
	).Scan(&count)
	return count > 0
}

func APIContactDelete(c *gin.Context) {
	id := c.Param("id")
	if !contactBelongsToCompany(id, currentCompanyID(c)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}
	if err := postgres.DeleteContact(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func APIContactPurchaseIncrement(c *gin.Context) {
	id := c.Param("id")
	if !contactBelongsToCompany(id, currentCompanyID(c)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}
	if err := postgres.IncrementContactPurchase(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func APIContactPurchaseDecrement(c *gin.Context) {
	id := c.Param("id")
	if !contactBelongsToCompany(id, currentCompanyID(c)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}
	if err := postgres.DecrementContactPurchase(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
